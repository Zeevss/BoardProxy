package sdk

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

func TestFetchHTTPReturnsMultipleKeys(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) *http.Response {
		if request.Header.Get("Accept") != protocol.MediaType {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		return response(http.StatusOK, `{
            "version":1,"id":"family","name":"Family","state":"enabled","revision":"r1",
            "issuedAt":"2026-08-15T12:00:00Z","usedBytes":30,
            "keys":[
              {"id":"phone","name":"Phone","nodeId":"node-1","userId":"alice","state":"enabled","usedBytes":10,"keylink":"bproxy://one"},
              {"id":"laptop","name":"Laptop","nodeId":"node-2","userId":"alice","state":"enabled","usedBytes":20,"keylink":"bproxy://two"}
            ]}`)
	})}}

	snapshot, err := client.Fetch(context.Background(), testSubscriptionURL(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Keys) != 2 || snapshot.Keys[0].ID != "phone" || snapshot.Keys[1].ID != "laptop" {
		t.Fatalf("unexpected keys: %+v", snapshot.Keys)
	}
}

func TestFetchDoesNotBypassTerminalHTTPStatusWithCache(t *testing.T) {
	rawURL := testSubscriptionURL(t)
	cache := NewMemoryCache()
	cache.Store(rawURL, protocol.Subscription{Version: 1, ID: "cached", State: "enabled", Revision: "old"})
	client := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
			return response(http.StatusGone, "revoked")
		})},
		Cache: cache,
	}

	if _, err := client.Fetch(context.Background(), rawURL); err == nil {
		t.Fatal("expected terminal HTTP 410 error")
	}
}

func testSubscriptionURL(t *testing.T) string {
	t.Helper()
	value, err := protocol.BuildURL("https://subscribe.example.com", "bps_token", protocol.Capsule{
		Version: 1, YandexURL: "https://disk.yandex.ru/edit/example", RecoveryKeyID: "r1",
		ClientPrivateKey:     protocol.EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: protocol.EncodeKey(bytes.Repeat([]byte{2}, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type roundTripFunc func(*http.Request) *http.Response

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request), nil
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Request:    &http.Request{},
	}
}
