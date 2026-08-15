package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

type staticResolver struct{ value protocol.Subscription }

func (resolver staticResolver) ResolveToken(context.Context, string) (protocol.Subscription, error) {
	return resolver.value, nil
}

func TestSubscriptionJSONContainsMultipleKeys(t *testing.T) {
	handler := New(staticResolver{testSnapshot()}, nil, func() bool { return true }).Routes()
	request := httptest.NewRequest(http.MethodGet, "/s/bps_token", nil)
	request.Header.Set("Accept", protocol.MediaType)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"id":"phone"`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"id":"laptop"`)) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestQRRequiresMatchingSubscriptionURL(t *testing.T) {
	handler := New(staticResolver{testSnapshot()}, nil, nil).Routes()
	capsule := protocol.Capsule{
		Version: 1, YandexURL: "https://disk.yandex.ru/i/example", RecoveryKeyID: "r1",
		ClientPrivateKey:     protocol.EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: protocol.EncodeKey(bytes.Repeat([]byte{2}, 32)),
	}
	rawURL, err := protocol.BuildURL("https://subscribe.example.com", "bps_token", capsule)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/s/bps_token/qr", bytes.NewBufferString(rawURL))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" ||
		!bytes.HasPrefix(recorder.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("unexpected QR response %d, type %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func testSnapshot() protocol.Subscription {
	return protocol.Subscription{
		Version: 1, ID: "family", Name: "Family", State: "enabled", Revision: "r1",
		Keys: []protocol.Key{
			{ID: "phone", Name: "Phone", State: "enabled", Keylink: "bproxy://one"},
			{ID: "laptop", Name: "Laptop", State: "enabled", Keylink: "bproxy://two"},
		},
	}
}
