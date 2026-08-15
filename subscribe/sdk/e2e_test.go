package sdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	recoveryworker "github.com/Zeevss/BoardProxy/subscribe/internal/recovery"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	"github.com/Zeevss/BoardProxy/subscribe/recovery"
)

type liveResolver struct {
	clientPublic string
	snapshot     protocol.Subscription
}

func (resolver liveResolver) ResolveRecoveryKey(_ context.Context, publicKey string) (protocol.Subscription, error) {
	if publicKey != resolver.clientPublic {
		return protocol.Subscription{}, errors.New("unexpected recovery client")
	}
	return resolver.snapshot, nil
}

func TestLiveYandexRecoveryReturnsMultipleKeys(t *testing.T) {
	shareURL := os.Getenv("YANDEX_SHEETS_E2E_URL")
	if shareURL == "" {
		t.Skip("YANDEX_SHEETS_E2E_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	serverPrivate := bytes.Repeat([]byte{4}, 32)
	serverPublic, err := recovery.PublicKey(serverPrivate)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate := bytes.Repeat([]byte{5}, 32)
	clientPublic, err := recovery.PublicKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	expected := protocol.Subscription{
		Version: 1, ID: "live-e2e", Name: "Live E2E", State: "enabled", Revision: "live-1",
		IssuedAt: time.Now().UTC(), UsedBytes: 30,
		Keys: []protocol.Key{
			{ID: "one", Name: "One", NodeID: "node-1", UserID: "e2e", State: "enabled", UsedBytes: 10, Keylink: "bproxy://one"},
			{ID: "two", Name: "Two", NodeID: "node-2", UserID: "e2e", State: "enabled", UsedBytes: 20, Keylink: "bproxy://two"},
		},
	}
	worker := recoveryworker.New(
		shareURL, "live-e2e-key", serverPrivate,
		liveResolver{clientPublic: protocol.EncodeKey(clientPublic), snapshot: expected},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Run(ctx) }()
	readyTicker := time.NewTicker(100 * time.Millisecond)
	defer readyTicker.Stop()
	for !worker.Ready() {
		select {
		case err := <-workerResult:
			t.Fatalf("recovery worker stopped: %v", err)
		case <-readyTicker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	rawURL, err := protocol.BuildURL("https://offline.invalid", "bps_live_e2e", protocol.Capsule{
		Version: 1, YandexURL: shareURL, RecoveryKeyID: "live-e2e-key",
		ClientPrivateKey: protocol.EncodeKey(clientPrivate), RecoveryServerPublic: protocol.EncodeKey(serverPublic),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{HTTP: &http.Client{Transport: failingTransport{}}}
	actual, err := client.Fetch(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if actual.ID != expected.ID || len(actual.Keys) != 2 || actual.Keys[1].ID != "two" {
		t.Fatalf("unexpected recovered subscription: %+v", actual)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("primary HTTPS intentionally unavailable in live E2E")
}
