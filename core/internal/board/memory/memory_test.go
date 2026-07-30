package memory

import (
	"context"
	"testing"
	"time"

	"bproxy-core/internal/board"
)

func recv(t *testing.T, ch <-chan board.Event) board.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return board.Event{}
	}
}

func noEvent(t *testing.T, ch <-chan board.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPeerSeesCreateAuthorDoesNot(t *testing.T) {
	ctx := context.Background()
	b := NewBoard()
	a := b.NewSession("A")
	c := b.NewSession("C")
	if _, err := a.Subscribe(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Subscribe(ctx, "p1"); err != nil {
		t.Fatal(err)
	}

	if err := a.Put(ctx, board.Object{ID: "x", Value: "hello"}); err != nil {
		t.Fatal(err)
	}

	ev := recv(t, c.Events())
	if ev.Kind != board.Created || ev.Object.ID != "x" || ev.Object.Value != "hello" {
		t.Fatalf("peer got wrong event: %+v", ev)
	}
	if ev.Object.CreatorHash != "A" {
		t.Fatalf("creator hash = %q, want A", ev.Object.CreatorHash)
	}
	noEvent(t, a.Events()) // author must not see its own creation
}

func TestDeleteReachesAuthorAsAck(t *testing.T) {
	ctx := context.Background()
	b := NewBoard()
	a := b.NewSession("A")
	c := b.NewSession("C")
	mustSubscribe(t, a, "p1")
	mustSubscribe(t, c, "p1")

	if err := a.Put(ctx, board.Object{ID: "x", Value: "data"}); err != nil {
		t.Fatal(err)
	}
	recv(t, c.Events()) // C observes the create

	// C deletes A's object — that deletion is A's ACK.
	if err := c.Delete(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	ev := recv(t, a.Events())
	if ev.Kind != board.Deleted || ev.Object.ID != "x" {
		t.Fatalf("author got wrong ack event: %+v", ev)
	}
}

func TestDeleteBatchEmitsIndividualEvents(t *testing.T) {
	ctx := context.Background()
	b := NewBoard()
	a := b.NewSession("A")
	c := b.NewSession("C")
	mustSubscribe(t, a, "p1")
	mustSubscribe(t, c, "p1")

	for _, id := range []string{"x", "y", "z"} {
		if err := a.Put(ctx, board.Object{ID: id, Value: "data"}); err != nil {
			t.Fatal(err)
		}
		recv(t, c.Events()) // drain C's create events
	}

	if err := c.Delete(ctx, "x", "y", "z"); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, 3)
	for i := 0; i < 3; i++ {
		ev := recv(t, a.Events())
		if ev.Kind != board.Deleted {
			t.Fatalf("event %d: kind = %v, want Deleted", i, ev.Kind)
		}
		got[ev.Object.ID] = true
	}
	for _, id := range []string{"x", "y", "z"} {
		if !got[id] {
			t.Fatalf("missing ack event for %q, got %v", id, got)
		}
	}
	noEvent(t, a.Events())
}

func TestDeleteNoIDsIsNoop(t *testing.T) {
	ctx := context.Background()
	b := NewBoard()
	a := b.NewSession("A")
	c := b.NewSession("C")
	mustSubscribe(t, a, "p1")
	mustSubscribe(t, c, "p1")

	if err := c.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	noEvent(t, a.Events())
}

func TestSubscribeReturnsSnapshot(t *testing.T) {
	ctx := context.Background()
	b := NewBoard()
	a := b.NewSession("A")
	mustSubscribe(t, a, "p1")
	if err := a.Put(ctx, board.Object{ID: "x", Value: "v"}); err != nil {
		t.Fatal(err)
	}

	late := b.NewSession("L")
	snap, err := late.Subscribe(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 1 || snap[0].ID != "x" {
		t.Fatalf("snapshot = %+v, want single object x", snap)
	}
}

func TestPutBeforeSubscribeFails(t *testing.T) {
	b := NewBoard()
	a := b.NewSession("A")
	if err := a.Put(context.Background(), board.Object{ID: "x"}); err != board.ErrNotSubscribed {
		t.Fatalf("err = %v, want ErrNotSubscribed", err)
	}
}

func mustSubscribe(t *testing.T, s *Session, page string) {
	t.Helper()
	if _, err := s.Subscribe(context.Background(), page); err != nil {
		t.Fatal(err)
	}
}
