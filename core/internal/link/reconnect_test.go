package link

import (
	"context"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
)

// reconnectableSession оборачивает board.Session, добавляя управляемый в тесте
// канал Reconnects() (у memory он nil).
type reconnectableSession struct {
	board.Session
	reconnects chan []board.Object
}

func (s *reconnectableSession) Reconnects() <-chan []board.Object { return s.reconnects }

// TestReconnectSnapshotTriggersReconcile проверяет проводку: снапшот, пришедший
// на Reconnects() (а не через явный Reconcile), проходит той же reconcile-логикой
// — освобождает слоты за объекты, заacked во время обрыва.
func TestReconnectSnapshotTriggersReconcile(t *testing.T) {
	b := memory.NewBoard()
	ctx := context.Background()
	base := b.NewSession("A")
	if _, err := base.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	sess := &reconnectableSession{Session: base, reconnects: make(chan []board.Object, 1)}

	l := New(sess, codec.Base64Codec{}, Options{InitialSendWindow: 2})
	defer l.Close()

	if err := l.Send(ctx, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := l.Send(ctx, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got := len(l.outstanding.snapshotIDs()); got != 2 {
		t.Fatalf("outstanding = %d, want 2", got)
	}

	// Реконнект: свежий снапшот не содержит наших объектов — оба заacked, пока
	// нас не было. Доставляем его через Reconnects(), как это делает драйвер.
	sess.reconnects <- nil

	if err := eventually(func() bool { return len(l.outstanding.snapshotIDs()) == 0 }); err != nil {
		t.Fatalf("outstanding не очистился после reconnect-снапшота: %v", err)
	}

	// Слоты освобождены — следующая отправка не должна блокироваться.
	done := make(chan error, 1)
	go func() { done <- l.Send(ctx, []byte("three")) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("send после реконнекта: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("send заблокировался: слоты не освобождены reconcile'ом на реконнекте")
	}
}

func TestBoardEventsClosureClosesLink(t *testing.T) {
	b := memory.NewBoard()
	base := b.NewSession("A")
	if _, err := base.Subscribe(context.Background(), "page"); err != nil {
		t.Fatal(err)
	}
	l := New(base, codec.Base64Codec{}, Options{})
	defer l.Close()

	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("terminal board event closure did not close Link.Done")
	}
}
