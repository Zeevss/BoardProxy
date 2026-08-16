package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

func TestParseLinkReturnsBoardMetadata(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	info, err := NewApp().ParseLink("bproxy://" + token + "@board-a,board-b#desktop")
	if err != nil {
		t.Fatal(err)
	}
	if info.Label != "desktop" {
		t.Fatalf("label = %q", info.Label)
	}
	if len(info.Boards) != 2 || info.Boards[0] != "board-a" || info.Boards[1] != "board-b" {
		t.Fatalf("boards = %v", info.Boards)
	}
}

func TestEventLogHandlerIncludesStructuredAttributes(t *testing.T) {
	handler := slog.New(&eventLogHandler{}).
		With("bundle", "abc").
		WithGroup("lane").
		Handler()
	record := slog.NewRecord(time.Now(), slog.LevelDebug, "link transport stats", 0)
	record.Add("cwnd", 12, "rtt_ms", 84)

	message := handler.(*eventLogHandler).format(record)
	for _, want := range []string{
		`bundle="abc"`,
		"lane.cwnd=12",
		"lane.rtt_ms=84",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("formatted log %q does not contain %q", message, want)
		}
	}
}

func TestParseLinkRejectsForeignScheme(t *testing.T) {
	if _, err := NewApp().ParseLink("https://example.com"); err == nil {
		t.Fatal("expected invalid keylink error")
	}
}

func TestParseLinkResolvesSubscriptionWithMultipleKeys(t *testing.T) {
	app := NewApp()
	app.subscriptions = subscriptionFetcherFunc(func(context.Context, string) (protocol.Subscription, error) {
		return protocol.Subscription{
			Version: 1, ID: "family", Name: "Family", State: "enabled", Revision: "r1",
			Keys: []protocol.Key{
				{ID: "one", Name: "Germany", NodeID: "node-1", State: "enabled", Keylink: testKeylink("board-a", "de")},
				{ID: "two", Name: "Netherlands", NodeID: "node-2", State: "enabled", Keylink: testKeylink("board-b", "nl")},
			},
		}, nil
	})

	info, err := app.ParseLink("https://subscribe.example.com/s/token#bp1=capsule")
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "subscription" || info.Label != "Family" || len(info.Keys) != 2 {
		t.Fatalf("unexpected subscription info: %+v", info)
	}
	if len(info.Boards) != 2 || info.Boards[0] != "board-a" || info.Boards[1] != "board-b" {
		t.Fatalf("boards = %v", info.Boards)
	}
}

func TestSelectSubscriptionKeySkipsDisabled(t *testing.T) {
	snapshot := protocol.Subscription{Name: "Family", Keys: []protocol.Key{
		{ID: "disabled", State: "disabled", Keylink: testKeylink("board-a", "off")},
		{ID: "enabled", Name: "Netherlands", NodeID: "node-2", State: "enabled", Keylink: testKeylink("board-b", "on")},
	}}
	key, err := selectSubscriptionKey(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if key.ID != "enabled" {
		t.Fatalf("selected key = %+v", key)
	}
}

type subscriptionFetcherFunc func(context.Context, string) (protocol.Subscription, error)

func (fn subscriptionFetcherFunc) Fetch(ctx context.Context, raw string) (protocol.Subscription, error) {
	return fn(ctx, raw)
}

func testKeylink(board, label string) string {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	return "bproxy://" + token + "@" + board + "#" + label
}
