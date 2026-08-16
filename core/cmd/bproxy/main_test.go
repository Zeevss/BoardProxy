package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

func TestWriteSubscriptionKeys(t *testing.T) {
	var output bytes.Buffer
	snapshot := protocol.Subscription{Keys: []protocol.Key{
		{ID: "de", Name: "Germany", NodeID: "node-1", UserID: "alice", State: "enabled", UsedBytes: 42, Keylink: "bproxy://one"},
		{ID: "nl", Name: "Netherlands", NodeID: "node-2", UserID: "alice", State: "disabled", Keylink: ""},
	}}
	if err := writeSubscriptionKeys(&output, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ID", "Germany", "node-1", "bproxy://one", "Netherlands", "disabled"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output %q does not contain %q", output.String(), expected)
		}
	}
}

func TestSubscriptionCommandResolvesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != protocol.MediaType {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", protocol.MediaType)
		_, _ = writer.Write([]byte(`{
			"version":1,"id":"family","name":"Family","state":"enabled","revision":"r1",
			"issuedAt":"2026-08-15T12:00:00Z","usedBytes":42,
			"keys":[{"id":"de","name":"Germany","nodeId":"node-1","userId":"alice","state":"enabled","usedBytes":42,"keylink":"bproxy://one"}]
		}`))
	}))
	defer server.Close()

	rawURL, err := protocol.BuildURL(server.URL, "bps_test", protocol.Capsule{
		Version: 1, YandexURL: "https://disk.yandex.ru/i/example", RecoveryKeyID: "r1",
		ClientPrivateKey:     protocol.EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: protocol.EncodeKey(bytes.Repeat([]byte{2}, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cmd := subscriptionCmd()
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--url", rawURL})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Germany") || !strings.Contains(output.String(), "bproxy://one") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}
