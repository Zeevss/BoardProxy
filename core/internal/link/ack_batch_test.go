package link

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
)

// deleteCounter wraps a board.Session and counts Delete calls versus the
// total number of ids acked across them, to verify ackWorker batches bursts
// into fewer wire calls instead of one Delete per id. If notify is set, every
// Delete call sends on it (non-blocking) so a test can observe call timing.
type deleteCounter struct {
	board.Session
	notify chan struct{}

	mu    sync.Mutex
	calls int
	acked int
}

func (d *deleteCounter) Delete(ctx context.Context, ids ...string) error {
	d.mu.Lock()
	d.calls++
	d.acked += len(ids)
	d.mu.Unlock()
	if d.notify != nil {
		select {
		case d.notify <- struct{}{}:
		default:
		}
	}
	return d.Session.Delete(ctx, ids...)
}

func (d *deleteCounter) snapshot() (calls, acked int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls, d.acked
}

func TestAckWorkerBatchesUnderBurst(t *testing.T) {
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
	counter := &deleteCounter{Session: sessB}

	la := New(sessA, codec.Base64Codec{}, Options{RecvWindow: 64, InitialSendWindow: 64})
	lb := New(counter, codec.Base64Codec{}, Options{RecvWindow: 64, InitialSendWindow: 64})
	defer la.Close()
	defer lb.Close()

	const n = 200
	go func() {
		for i := 0; i < n; i++ {
			_ = la.Send(ctx, []byte(fmt.Sprintf("msg-%d", i)))
		}
	}()

	for i := 0; i < n; i++ {
		select {
		case <-lb.Recv():
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for frame %d", i)
		}
	}

	// acked converges to n plus the one startup window-advertise control frame
	// that New() sends on each side (B acks A's), so wait for "at least n"
	// rather than an exact match.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, acked := counter.snapshot(); acked >= n {
			break
		}
		if time.Now().After(deadline) {
			_, acked := counter.snapshot()
			t.Fatalf("not all ids acked within deadline: acked=%d want>=%d", acked, n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	calls, acked := counter.snapshot()
	if calls >= acked {
		t.Fatalf("no batching observed: calls=%d acked=%d, want calls < acked", calls, acked)
	}
	t.Logf("batched %d acks into %d Delete calls", acked, calls)
}

// TestAckWorkerSingleAckNoExtraDelay verifies a lone ack — no burst to batch
// with — is not held up waiting for a batch to fill: ackWorker must drain
// non-blockingly and fall back to a single-id Delete immediately.
func TestAckWorkerSingleAckNoExtraDelay(t *testing.T) {
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
	notify := make(chan struct{}, 8)
	counter := &deleteCounter{Session: sessB, notify: notify}

	la := New(sessA, codec.Base64Codec{}, Options{})
	lb := New(counter, codec.Base64Codec{}, Options{})
	defer la.Close()
	defer lb.Close()

	// Drain the Delete call(s) triggered by the startup keepalive/window
	// advertise exchange (both sides send one immediately in New) before
	// measuring the one ack we actually care about.
	drainNotify(notify, 200*time.Millisecond)

	start := time.Now()
	if err := la.Send(ctx, []byte("solo")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lb.Recv():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame")
	}

	select {
	case <-notify:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("ack not observed promptly")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("ack took %v, expected near-immediate delivery for a lone ack", elapsed)
	}
}

func drainNotify(ch <-chan struct{}, d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case <-ch:
		case <-deadline:
			return
		}
	}
}
