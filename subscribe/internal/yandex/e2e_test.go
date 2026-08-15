package yandex_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/yandex"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

func TestLiveCommentEventsAndCleanup(t *testing.T) {
	shareURL := os.Getenv("YANDEX_SHEETS_E2E_URL")
	if shareURL == "" {
		t.Skip("YANDEX_SHEETS_E2E_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	watcher, err := yandex.Open(ctx, shareURL)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := yandex.Open(ctx, shareURL)
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan yandex.Event, 16)
	ready := make(chan struct{}, 1)
	watchResult := make(chan error, 1)
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	go func() { watchResult <- watcher.Watch(watchCtx, ready, events) }()
	select {
	case <-ready:
	case err := <-watchResult:
		t.Fatalf("watcher stopped before ready: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	requestID := fmt.Sprintf("codex-e2e-subscribe-%d", time.Now().UnixNano())
	cell := protocol.MailboxCell(requestID, 0)
	rootText := requestID + " root"
	thread, err := writer.CreateThread(ctx, cell, rootText)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupThread(t, shareURL, thread.Root.ID)
	waitForComment(t, ctx, events, watchResult, func(comment yandex.Comment) bool {
		return comment.ParentID == "" && comment.Text == rootText
	})

	replyText := requestID + " reply"
	if _, err := writer.AddReply(ctx, thread.Root.ID, replyText); err != nil {
		t.Fatal(err)
	}
	waitForComment(t, ctx, events, watchResult, func(comment yandex.Comment) bool {
		return comment.ParentID == thread.Root.ID && comment.Text == replyText
	})

	if err := writer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.DeleteComment(ctx, thread.Root.ID); err != nil {
		t.Fatal(err)
	}
	if err := writer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	for _, remaining := range writer.Threads(cell) {
		if remaining.Root.ID == thread.Root.ID {
			t.Fatalf("temporary E2E thread %s was not removed", thread.Root.ID)
		}
	}
}

func waitForComment(
	t *testing.T,
	ctx context.Context,
	events <-chan yandex.Event,
	watchResult <-chan error,
	match func(yandex.Comment) bool,
) {
	t.Helper()
	for {
		select {
		case event := <-events:
			if match(event.Comment) {
				return
			}
		case err := <-watchResult:
			t.Fatalf("watcher stopped: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func cleanupThread(t *testing.T, shareURL, rootID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := yandex.Open(ctx, shareURL)
	if err != nil {
		t.Logf("cleanup open failed for %s: %v", rootID, err)
		return
	}
	if err := client.DeleteComment(ctx, rootID); err != nil && !errors.Is(err, yandex.ErrNotFound) {
		t.Logf("cleanup delete failed for %s: %v", rootID, err)
	}
}
