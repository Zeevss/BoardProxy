package link

import (
	"context"
	"fmt"
	"testing"
	"time"

	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/boardtest"
	"bproxy-core/internal/codec"
)

// TestLiveEcho runs the full L0+L1+L2 stack over a real board: two guests on the
// same slide each wrap their session in a Link and exchange sequence-numbered
// frames in both directions, exercising create=deliver / delete=ack end to end.
//
// Skipped unless BPROXY_LIVE=1. BPROXY_BOARD overrides the board hash.
func TestLiveEcho(t *testing.T) {
	hash := boardtest.LiveHash(t)
	opts := yandex.Options{APIBase: boardtest.APIBase, Hash: hash, GuestName: "bproxy-link"}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	sessA, err := yandex.Join(ctx, opts)
	if err != nil {
		t.Fatalf("A join: %v", err)
	}
	sessB, err := yandex.Join(ctx, opts)
	if err != nil {
		t.Fatalf("B join: %v", err)
	}
	slide := sessA.CurrentSlide()
	if _, err := sessA.Subscribe(ctx, slide); err != nil {
		t.Fatalf("A subscribe: %v", err)
	}
	if _, err := sessB.Subscribe(ctx, slide); err != nil {
		t.Fatalf("B subscribe: %v", err)
	}

	la := New(sessA, codec.Base64Codec{}, Options{RecvWindow: 8, InitialSendWindow: 3})
	lb := New(sessB, codec.Base64Codec{}, Options{RecvWindow: 8, InitialSendWindow: 3})
	defer la.Close()
	defer lb.Close()

	const n = 8
	// A -> B
	go func() {
		for i := 0; i < n; i++ {
			_ = la.Send(ctx, []byte(fmt.Sprintf("a2b-%d", i)))
		}
	}()
	for i := 0; i < n; i++ {
		select {
		case got := <-lb.Recv():
			if want := fmt.Sprintf("a2b-%d", i); string(got) != want {
				t.Fatalf("A->B frame %d: got %q want %q", i, got, want)
			}
		case <-ctx.Done():
			t.Fatalf("A->B timed out at frame %d", i)
		}
	}
	t.Logf("A->B delivered %d frames in order over the live board", n)

	// B -> A
	go func() {
		for i := 0; i < n; i++ {
			_ = lb.Send(ctx, []byte(fmt.Sprintf("b2a-%d", i)))
		}
	}()
	for i := 0; i < n; i++ {
		select {
		case got := <-la.Recv():
			if want := fmt.Sprintf("b2a-%d", i); string(got) != want {
				t.Fatalf("B->A frame %d: got %q want %q", i, got, want)
			}
		case <-ctx.Done():
			t.Fatalf("B->A timed out at frame %d", i)
		}
	}
	t.Logf("B->A delivered %d frames in order — full duplex link over the board confirmed", n)
}
