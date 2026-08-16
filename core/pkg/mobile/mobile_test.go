package mobile

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

type testListener struct {
	mu       sync.Mutex
	statuses []string
	messages []string
}

func TestResolveSubscriptionReturnsSnapshotJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != protocol.MediaType {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		_, _ = writer.Write([]byte(`{
			"version":1,"id":"family","name":"Family","state":"enabled","revision":"r1",
			"issuedAt":"2026-08-16T08:00:00Z","usedBytes":42,
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
	raw, err := ResolveSubscription(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"name":"Family"`) || !strings.Contains(raw, `"keylink":"bproxy://one"`) {
		t.Fatalf("unexpected snapshot: %s", raw)
	}
}

func (l *testListener) OnStatus(status, message string) {
	l.mu.Lock()
	l.statuses = append(l.statuses, status)
	l.messages = append(l.messages, message)
	l.mu.Unlock()
}
func (*testListener) OnLog(string, string) {}
func (*testListener) OnMetrics(string)     {}

func TestNewClientRejectsUnknownConfigField(t *testing.T) {
	if _, err := CreateClient(`{"keylink":"x","typo":true}`, nil, nil); err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
}

func TestUDPIsSupportedByMobileConfig(t *testing.T) {
	_, err := CreateClient(`{"keylink":"placeholder","enable_udp":true}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !SupportsUDP() {
		t.Fatal("SupportsUDP returned false with datagram transport enabled")
	}
}

func TestMetricsJSONHasStableLowercaseSchema(t *testing.T) {
	c, err := CreateClient(`{"keylink":"placeholder"}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(c.MetricsJSON()), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "rtt_ms", "streams", "datagrams", "total_tx", "total_rx", "details"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("MetricsJSON missing key %q: %v", key, got)
		}
	}
}
