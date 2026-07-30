package main

import (
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"
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
