package agent

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	corev1 "bproxy-core/api/control/v1"
	"bproxy-node-agent/internal/localstore"
)

// Ядро на простое не шлёт ни одного события: ни запущенных досок, ни сессий.
// Раньше снимок брался внутри цикла приёма, поэтому хаб переставал получать
// его вовсе и показывал «ядро не отвечает» у здоровой ноды.
func TestSnapshotsContinueWhileCoreIsSilent(t *testing.T) {
	previous := runtimeSnapshotGap
	runtimeSnapshotGap = 20 * time.Millisecond
	t.Cleanup(func() { runtimeSnapshotGap = previous })

	store := openStore(t)
	core := &countingCore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- consumeCoreRuntimeEvents(ctx, core, silentStream{ctx: ctx}, store, slog.New(slog.DiscardHandler))
	}()

	// Первый снимок уходит сразу, дальше — по тикеру.
	waitFor(t, func() bool { return core.calls.Load() >= 3 }, "снимки не пошли при молчащем ядре")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("сбор не завершился по отмене контекста")
	}
}

// Ошибка приёма должна возвращаться наружу: внешний цикл переподключается.
func TestStreamFailureStopsCollection(t *testing.T) {
	store := openStore(t)
	core := &countingCore{}

	err := consumeCoreRuntimeEvents(
		context.Background(), core, failingStream{}, store, slog.New(slog.DiscardHandler),
	)
	if err != io.EOF {
		t.Fatalf("consumeCoreRuntimeEvents() = %v, want io.EOF", err)
	}
}

// Недоступное ядро не должно ронять сбор: сокет мог ещё не подняться.
func TestUnavailableCoreKeepsCollectionAlive(t *testing.T) {
	previous := runtimeSnapshotGap
	runtimeSnapshotGap = 20 * time.Millisecond
	t.Cleanup(func() { runtimeSnapshotGap = previous })

	store := openStore(t)
	core := &countingCore{fail: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- consumeCoreRuntimeEvents(ctx, core, silentStream{ctx: ctx}, store, slog.New(slog.DiscardHandler))
	}()

	waitFor(t, func() bool { return core.calls.Load() >= 3 }, "сбор остановился на недоступном ядре")

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("consumeCoreRuntimeEvents() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("сбор не завершился по отмене контекста")
	}
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(message)
}

func openStore(t *testing.T) *localstore.Store {
	t.Helper()
	store, err := localstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

type countingCore struct {
	calls atomic.Int64
	fail  bool
}

func (c *countingCore) RuntimeSnapshot(context.Context) (*corev1.RuntimeSnapshot, error) {
	c.calls.Add(1)
	if c.fail {
		return nil, io.ErrUnexpectedEOF
	}
	return &corev1.RuntimeSnapshot{}, nil
}

// silentStream — ядро, которому нечего сказать: Recv висит до отмены.
type silentStream struct{ ctx context.Context }

func (s silentStream) Recv() (*corev1.CoreRuntimeEvent, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s silentStream) Close() error { return nil }

type failingStream struct{}

func (failingStream) Recv() (*corev1.CoreRuntimeEvent, error) { return nil, io.EOF }
func (failingStream) Close() error                            { return nil }
