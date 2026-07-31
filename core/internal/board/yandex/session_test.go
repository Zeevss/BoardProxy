package yandex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex/socketio"
)

// fakeConn — управляемая замена *socketio.Client для тестов реконнекта.
type fakeConn struct {
	events    chan socketio.Message
	ack       []json.RawMessage
	closeOnce sync.Once
	closed    chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		events: make(chan socketio.Message, 8),
		ack:    []json.RawMessage{json.RawMessage(`{"objects":[]}`)},
		closed: make(chan struct{}),
	}
}

func (c *fakeConn) Emit(_ context.Context, _ string, _ any) ([]json.RawMessage, error) {
	select {
	case <-c.closed:
		return nil, socketio.ErrConnClosed
	default:
		return c.ack, nil
	}
}

func (c *fakeConn) Events() <-chan socketio.Message { return c.events }

func (c *fakeConn) Close() error {
	c.drop()
	return nil
}

// drop имитирует обрыв websocket'а: канал событий закрывается (manage выходит из
// range), а Emit начинает возвращать ErrConnClosed.
func (c *fakeConn) drop() {
	c.closeOnce.Do(func() {
		close(c.closed)
		close(c.events)
	})
}

// newTestSession собирает Session с внедрённым dial, минуя сетевой Join.
func newTestSession(dial dialFunc) *Session {
	s := &Session{
		participant: "me",
		info:        &whiteboardInfo{},
		socketURL:   "wss://test",
		events:      make(chan board.Event, 16),
		reconnects:  make(chan []board.Object, 1),
		closeCh:     make(chan struct{}),
		connWait:    make(chan struct{}),
	}
	s.dial = dial
	return s
}

func TestSessionReconnectsAndResubscribes(t *testing.T) {
	conn1, conn2 := newFakeConn(), newFakeConn()
	dials := make(chan *fakeConn, 1)
	dials <- conn2

	s := newTestSession(func(context.Context) (socketConn, error) {
		return <-dials, nil
	})
	s.setConnected(conn1)
	go s.manage()
	defer s.Close()

	if _, err := s.Subscribe(context.Background(), "page1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Обрыв: manage должен переподключиться на conn2, заново подписаться и отдать
	// снапшот на Reconnects().
	conn1.drop()

	select {
	case <-s.Reconnects():
	case <-time.After(2 * time.Second):
		t.Fatal("после обрыва не пришёл reconnect-снапшот")
	}

	// Операции идут по новому соединению без ошибки наверх.
	if err := s.Put(context.Background(), board.Object{ID: "x", Value: "v"}); err != nil {
		t.Fatalf("put после реконнекта: %v", err)
	}
}

func TestSessionReconnectForeverIgnoresRetryDeadline(t *testing.T) {
	conn1, conn2 := newFakeConn(), newFakeConn()
	attempts := 0
	s := newTestSession(func(context.Context) (socketConn, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporary outage")
		}
		return conn2, nil
	})
	s.reconnectForever = true
	// A normal lane would exhaust this deadline after the first failed dial.
	// The hub observer must keep retrying and reach the second successful dial.
	s.reconnectDeadline = time.Nanosecond
	s.setConnected(conn1)
	go s.manage()
	defer s.Close()

	if _, err := s.Subscribe(context.Background(), "hub"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn1.drop()

	select {
	case <-s.Reconnects():
	case <-time.After(2 * time.Second):
		t.Fatal("retry-forever session did not reconnect after its finite deadline")
	}
	if attempts < 2 {
		t.Fatalf("dial attempts = %d, want at least 2", attempts)
	}
}

func TestSessionEmitBlocksAcrossReconnect(t *testing.T) {
	conn1, conn2 := newFakeConn(), newFakeConn()
	release := make(chan struct{})

	s := newTestSession(func(ctx context.Context) (socketConn, error) {
		// Второй dial (реконнект) держим, пока тест не разрешит — так получаем
		// детерминированное окно, когда соединения нет.
		select {
		case <-release:
			return conn2, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	s.setConnected(conn1)
	go s.manage()
	defer s.Close()

	if _, err := s.Subscribe(context.Background(), "page1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	conn1.drop()

	// Put во время обрыва должен блокироваться, а не падать.
	putErr := make(chan error, 1)
	go func() { putErr <- s.Put(context.Background(), board.Object{ID: "y", Value: "v"}) }()

	select {
	case err := <-putErr:
		t.Fatalf("Put вернулся во время обрыва (err=%v), должен был блокироваться", err)
	case <-time.After(150 * time.Millisecond):
		// Ожидаемо: висит в ожидании переподключения.
	}

	// Разрешаем реконнект — Put обязан завершиться успешно по новому соединению.
	close(release)
	select {
	case err := <-putErr:
		if err != nil {
			t.Fatalf("Put после реконнекта: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Put не завершился после реконнекта")
	}
}

func TestSessionCloseStopsReconnect(t *testing.T) {
	conn1 := newFakeConn()
	dialed := make(chan struct{}, 8)
	s := newTestSession(func(ctx context.Context) (socketConn, error) {
		dialed <- struct{}{}
		<-ctx.Done() // никогда не переподключаемся успешно
		return nil, ctx.Err()
	})
	s.setConnected(conn1)
	go s.manage()

	if _, err := s.Subscribe(context.Background(), "page1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn1.drop()

	// Дождёмся, что реконнект хотя бы начался.
	select {
	case <-dialed:
	case <-time.After(2 * time.Second):
		t.Fatal("реконнект не начался")
	}

	// Close должен разблокировать всё и закрыть канал событий.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case _, ok := <-s.Events():
		if ok {
			// допустимо получить остаточное событие, дренируем до закрытия
			for range s.Events() {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events() не закрылся после Close")
	}
}
