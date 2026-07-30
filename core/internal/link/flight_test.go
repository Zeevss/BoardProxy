package link

import (
	"context"
	"testing"
	"time"
)

func TestFlightLimitBlocksAndReleases(t *testing.T) {
	lim := newLimiter()    // limit starts at 4
	f := newFlight(lim, 2) // rwnd 2 → effective limit min(4,2)=2
	ctx := context.Background()

	if err := f.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.acquire(ctx); err != nil {
		t.Fatal(err)
	}

	// Third acquire must block until a slot is released.
	blocked := make(chan error, 1)
	go func() { blocked <- f.acquire(ctx) }()
	select {
	case <-blocked:
		t.Fatal("third acquire should have blocked at the limit")
	case <-time.After(100 * time.Millisecond):
	}

	f.release()
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("acquire after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not unblock after release")
	}
}

func TestFlightRwndRaiseUnblocks(t *testing.T) {
	lim := newLimiter()
	f := newFlight(lim, 1) // limit 1
	ctx := context.Background()

	if err := f.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() { blocked <- f.acquire(ctx) }()
	select {
	case <-blocked:
		t.Fatal("acquire should block at limit 1")
	case <-time.After(100 * time.Millisecond):
	}

	f.setRwnd(4) // raise the advertised window
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("acquire after raise: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("raising rwnd did not unblock a waiter")
	}
}

func TestFlightAcquireCancels(t *testing.T) {
	lim := newLimiter()
	f := newFlight(lim, 1)
	ctx, cancel := context.WithCancel(context.Background())

	if err := f.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() { blocked <- f.acquire(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-blocked:
		if err == nil {
			t.Fatal("acquire should return ctx error after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not observe context cancellation")
	}
}
