package yandex

import (
	"context"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/boardtest"
)

// TestLiveCreate is the M1 acceptance check: the Go port of
// probe/create_text_object.mjs. It joins the board, subscribes to the current
// slide, creates a text object, and verifies via a second guest that the object
// is really on the page (independent of the ack channel).
//
// It is skipped unless BPROXY_LIVE=1. Set BPROXY_BOARD to override the board
// hash (defaults to the authorized test board from SPEC.md).
func TestLiveCreate(t *testing.T) {
	hash := boardtest.LiveHash(t)
	opts := Options{APIBase: boardtest.APIBase, Hash: hash, GuestName: "bproxy-m1"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writer, err := Join(ctx, opts)
	if err != nil {
		t.Fatalf("writer join: %v", err)
	}
	defer writer.Close()
	slide := writer.info.currentSlide
	t.Logf("joined: participant=%s slide=%s socket=%s", writer.participant, slide, writer.info.socketServers[0].URL())

	if _, err := writer.Subscribe(ctx, slide); err != nil {
		t.Fatalf("writer subscribe: %v", err)
	}

	obj := board.Object{ID: newObjectID(), Value: "bproxy M1 " + time.Now().Format("15:04:05")}
	if err := writer.Put(ctx, obj); err != nil {
		t.Fatalf("put: %v", err)
	}
	t.Logf("created object id=%s value=%q", obj.ID, obj.Value)

	// Independent verification: a second guest subscribes and must see the object
	// in its snapshot.
	reader, err := Join(ctx, opts)
	if err != nil {
		t.Fatalf("reader join: %v", err)
	}
	defer reader.Close()
	time.Sleep(500 * time.Millisecond)
	snap, err := reader.Subscribe(ctx, slide)
	if err != nil {
		t.Fatalf("reader subscribe: %v", err)
	}
	found := false
	for _, o := range snap {
		if o.ID == obj.ID {
			found = true
			if o.Value != obj.Value {
				t.Errorf("value mismatch: got %q want %q", o.Value, obj.Value)
			}
			break
		}
	}
	if !found {
		t.Fatalf("created object %s not found in reader snapshot of %d objects", obj.ID, len(snap))
	}
	t.Logf("verified object present in second guest's snapshot (%d objects on page)", len(snap))
	t.Logf("check visually: https://boards.yandex.ru/whiteboard/?hash=%s", hash)
}

// TestLiveDropAck verifies the ACK path: guest A creates an object, guest B
// observes it, B drops it (as a receiver acknowledging), and A observes the
// drop as a Deleted event carrying the object id. This is exactly the
// create=deliver / delete=ack loop the link layer relies on.
func TestLiveDropAck(t *testing.T) {
	hash := boardtest.LiveHash(t)
	opts := Options{APIBase: boardtest.APIBase, Hash: hash, GuestName: "bproxy-m3"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a, err := Join(ctx, opts)
	if err != nil {
		t.Fatalf("A join: %v", err)
	}
	defer a.Close()
	b, err := Join(ctx, opts)
	if err != nil {
		t.Fatalf("B join: %v", err)
	}
	defer b.Close()

	slide := a.info.currentSlide
	if _, err := a.Subscribe(ctx, slide); err != nil {
		t.Fatalf("A subscribe: %v", err)
	}
	if _, err := b.Subscribe(ctx, slide); err != nil {
		t.Fatalf("B subscribe: %v", err)
	}

	obj := board.Object{ID: newObjectID(), Value: "bproxy drop-ack " + time.Now().Format("15:04:05")}
	if err := a.Put(ctx, obj); err != nil {
		t.Fatalf("A put: %v", err)
	}

	// B observes the create.
	if err := waitFor(ctx, b.Events(), board.Created, obj.ID); err != nil {
		t.Fatalf("B did not observe create: %v", err)
	}
	t.Logf("B observed create of %s", obj.ID)

	// B drops the object (the ACK).
	if err := b.Delete(ctx, obj.ID); err != nil {
		t.Fatalf("B drop: %v", err)
	}

	// A observes the drop as its ACK.
	if err := waitFor(ctx, a.Events(), board.Deleted, obj.ID); err != nil {
		t.Fatalf("A did not observe drop (ACK): %v", err)
	}
	t.Logf("A observed drop (ACK) of %s — create=deliver/delete=ack loop confirmed", obj.ID)
}

func TestLiveReconnectResubscribesAfterSocketLoss(t *testing.T) {
	hash := boardtest.LiveHash(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	session, err := Join(ctx, Options{
		APIBase: boardtest.APIBase, Hash: hash, GuestName: "bproxy-reconnect",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Subscribe(ctx, session.CurrentSlide()); err != nil {
		t.Fatal(err)
	}

	oldSocket, _, _ := session.connState()
	if oldSocket == nil {
		t.Fatal("session has no active socket")
	}
	_ = oldSocket.Close()
	select {
	case <-session.Reconnects():
	case <-ctx.Done():
		t.Fatal("live websocket did not redial and resubscribe")
	}

	obj := board.Object{ID: newObjectID(), Value: "bproxy reconnect " + time.Now().Format("15:04:05")}
	if err := session.Put(ctx, obj); err != nil {
		t.Fatalf("Put after live reconnect: %v", err)
	}
}

func waitFor(ctx context.Context, ch <-chan board.Event, kind board.EventKind, id string) error {
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return context.Canceled
			}
			if ev.Kind == kind && ev.Object.ID == id {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
