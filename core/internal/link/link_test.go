package link

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
)

type failingPutSession struct {
	board.Session
}

func (s failingPutSession) Put(context.Context, board.Object) error {
	return errors.New("injected put failure")
}

func TestReasmOrdersAndDedups(t *testing.T) {
	r := newReasm()

	if ready, dup := r.accept(0, []byte("a")); dup || len(ready) != 1 || string(ready[0]) != "a" {
		t.Fatalf("seq0: ready=%v dup=%v", ready, dup)
	}
	// Out of order: 2 then 1 should release nothing then [1,2].
	if ready, dup := r.accept(2, []byte("c")); dup || len(ready) != 0 {
		t.Fatalf("seq2: ready=%v dup=%v", ready, dup)
	}
	ready, dup := r.accept(1, []byte("b"))
	if dup || len(ready) != 2 || string(ready[0]) != "b" || string(ready[1]) != "c" {
		t.Fatalf("seq1: ready=%v dup=%v", ready, dup)
	}
	// Duplicates of already-delivered seqs.
	if _, dup := r.accept(0, []byte("a")); !dup {
		t.Fatal("seq0 replay should be dup")
	}
	if _, dup := r.accept(2, []byte("c")); !dup {
		t.Fatal("seq2 replay should be dup")
	}
}

func TestLinkEchoWithFlowControl(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()

	sessA := b.NewSession("A")
	sessB := b.NewSession("B")
	if _, err := sessA.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessB.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}

	la := New(sessA, codec.Base64Codec{}, Options{RecvWindow: 4, InitialSendWindow: 2})
	lb := New(sessB, codec.Base64Codec{}, Options{RecvWindow: 4, InitialSendWindow: 2})
	defer la.Close()
	defer lb.Close()

	const n = 50
	var sentBytes int
	sendErr := make(chan error, 1)
	go func() {
		for i := 0; i < n; i++ {
			payload := []byte(fmt.Sprintf("msg-%d", i))
			if err := la.Send(ctx, payload); err != nil {
				sendErr <- err
				return
			}
		}
		sendErr <- nil
	}()

	for i := 0; i < n; i++ {
		sentBytes += len(fmt.Sprintf("msg-%d", i))
		select {
		case got := <-lb.Recv():
			want := fmt.Sprintf("msg-%d", i)
			if string(got) != want {
				t.Fatalf("frame %d: got %q want %q", i, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for frame %d", i)
		}
	}
	if err := <-sendErr; err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := eventually(func() bool {
		return la.ConfirmedBytes() == uint64(sentBytes)
	}); err != nil {
		t.Fatalf("confirmed bytes = %d, want %d: %v", la.ConfirmedBytes(), sentBytes, err)
	}
}

func TestSendTrackedClosesReceiptOnlyAfterPeerAck(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()
	sessA := b.NewSession("A")
	sessB := b.NewSession("B")
	if _, err := sessA.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessB.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	la := New(sessA, codec.Base64Codec{}, Options{})
	lb := New(sessB, codec.Base64Codec{}, Options{})
	defer la.Close()
	defer lb.Close()

	receipt, err := la.SendTracked(ctx, []byte("tracked"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-receipt:
		t.Fatal("receipt closed before peer received the payload")
	default:
	}
	select {
	case got := <-lb.Recv():
		if string(got) != "tracked" {
			t.Fatalf("payload = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not receive tracked payload")
	}
	select {
	case <-receipt:
	case <-time.After(2 * time.Second):
		t.Fatal("receipt did not close after peer ACK")
	}
}

func TestSendTrackedDoesNotConfirmLocalPutFailure(t *testing.T) {
	b := memory.NewBoard()
	base := b.NewSession("A")
	if _, err := base.Subscribe(context.Background(), "page"); err != nil {
		t.Fatal(err)
	}
	l := New(failingPutSession{Session: base}, codec.Base64Codec{}, Options{})
	defer l.Close()

	receipt, err := l.SendTracked(context.Background(), []byte("not-delivered"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Put failure did not close the physical lane")
	}
	select {
	case <-receipt:
		t.Fatal("local Put failure was incorrectly reported as peer delivery")
	default:
	}
}

func TestReconcileReleasesAckedWhileAway(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()
	sess := b.NewSession("A")
	if _, err := sess.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	// No peer to advertise a window, so the initial send window bounds inflight.
	l := New(sess, codec.Base64Codec{}, Options{InitialSendWindow: 2})
	defer l.Close()

	// Fill the window with 2 unacked objects (no peer to ack them).
	if err := l.Send(ctx, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := l.Send(ctx, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got := len(l.outstanding.snapshotIDs()); got != 2 {
		t.Fatalf("outstanding = %d, want 2", got)
	}

	// A reconnect snapshot shows neither object → both were acked while away.
	if err := l.Reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := eventually(func() bool { return len(l.outstanding.snapshotIDs()) == 0 }); err != nil {
		t.Fatalf("outstanding not cleared after reconcile: %v", err)
	}
	if got := l.ConfirmedBytes(); got != uint64(len("one")+len("two")) {
		t.Fatalf("confirmed bytes after reconcile = %d, want %d", got, len("one")+len("two"))
	}

	// Slots were released, so a further send must not block.
	done := make(chan error, 1)
	go func() { done <- l.Send(ctx, []byte("three")) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("send after reconcile: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("send blocked: slots not released by reconcile")
	}
}

// TestLastActivityIgnoresOwnAndInvalidEvents guards the live-only lease bug:
// Yandex may echo a participant's own broadcasts, unlike board/memory. Such an
// echo (or an unrelated object) must not keep an absent peer's page alive.
func TestLastActivityIgnoresOwnAndInvalidEvents(t *testing.T) {
	b := memory.NewBoard()
	sess := b.NewSession("self")
	if _, err := sess.Subscribe(context.Background(), "page"); err != nil {
		t.Fatal(err)
	}
	l := New(sess, codec.Base64Codec{}, Options{})
	defer l.Close()

	old := time.Now().Add(-time.Hour)
	l.lastRecv.Store(old.UnixNano())
	valid, err := codec.Base64Codec{}.Encode(encodeControl(encodeWindowAdvertise(4)))
	if err != nil {
		t.Fatal(err)
	}
	l.handle(board.Event{Kind: board.Created, Object: board.Object{
		ID: "own", CreatorHash: "self", Value: valid,
	}})
	l.handle(board.Event{Kind: board.Created, Object: board.Object{
		ID: "foreign-invalid", CreatorHash: "other", Value: "not-our-frame",
	}})
	l.handle(board.Event{Kind: board.Deleted, Object: board.Object{ID: "unknown"}})
	if got := l.LastActivity(); !got.Equal(old) {
		t.Fatalf("non-peer events changed LastActivity: got %v want %v", got, old)
	}

	l.handle(board.Event{Kind: board.Created, Object: board.Object{
		ID: "peer", CreatorHash: "other", Value: valid,
	}})
	if got := l.LastActivity(); !got.After(old) {
		t.Fatalf("valid peer event did not update LastActivity: %v", got)
	}
}

func TestLinkRemovesForeignGarbageFromReservedPage(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()
	owner, foreign := b.NewSession("owner"), b.NewSession("foreign")
	if _, err := owner.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	l := New(owner, codec.Base64Codec{}, Options{})
	defer l.Close()
	defer foreign.Close()
	if err := foreign.Put(ctx, board.Object{ID: "garbage", Value: "not-protocol"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-foreign.Events():
			if ev.Kind == board.Deleted && ev.Object.ID == "garbage" {
				return
			}
		case <-deadline:
			t.Fatal("foreign garbage was not removed from reserved lane page")
		}
	}
}

// TestTargetBatchSizeRespondsToTraffic is an end-to-end check that Send/ack
// traffic actually reaches the sizer wired into Link — i.e. TargetBatchSize()
// moves away from its initial value under sustained traffic, proving samples
// flow Send()->ack->sizer.onAck()->TargetBatchSize() correctly. It
// deliberately does not assert a direction (grow vs shrink): an in-memory
// board's real RTT is microseconds, small enough that ordinary goroutine
// scheduling jitter dominates the short/long cost comparison, so growth isn't
// reliably reproducible here — that precise behavior (grows on flat cost,
// shrinks on sustained worse cost) is already covered deterministically by
// sizer_test.go with explicit RTT/size inputs. Payloads must be at least
// sizerMinSampleSize — the same small "msg-%d" payloads other tests in this
// file use would be filtered out by that gate and TargetBatchSize() would
// never move at all.
func TestTargetBatchSizeRespondsToTraffic(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()

	sessA := b.NewSession("A")
	sessB := b.NewSession("B")
	if _, err := sessA.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessB.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}

	la := New(sessA, codec.Base64Codec{}, Options{RecvWindow: 16, InitialSendWindow: 8})
	lb := New(sessB, codec.Base64Codec{}, Options{RecvWindow: 16, InitialSendWindow: 8})
	defer la.Close()
	defer lb.Close()

	start := la.TargetBatchSize()

	payload := make([]byte, 8<<10) // ≥ sizerMinSampleSize
	const n = 60
	go func() {
		for i := 0; i < n; i++ {
			if err := la.Send(ctx, payload); err != nil {
				return
			}
		}
	}()
	// Drain B's side so the ack-by-delete loop (A's sizer input) keeps flowing
	// instead of stalling on a full recvCh. Recv() closes on lb.Close(), which
	// is deferred above, so this goroutine exits with the test.
	go func() {
		for {
			if _, ok := <-lb.Recv(); !ok {
				return
			}
		}
	}()

	if err := eventually(func() bool { return la.TargetBatchSize() != start }); err != nil {
		t.Fatalf("target batch size never moved from its initial value under traffic: still %d: %v", la.TargetBatchSize(), err)
	}
}

func eventually(cond func() bool) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("condition not met within deadline")
}
