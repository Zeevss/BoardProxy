package socketio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestApplicationBacklogDoesNotBlockEngineReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Client{
		incoming: make(chan Message),
		events:   make(chan queuedMessage, 2),
		ctx:      ctx,
		cancel:   cancel,
		acks:     newAckRegistry(),
	}
	p := packet{sio: sioEvent, ackID: -1, body: []byte(`["event",{}]`)}
	if err := c.handleMessage(p); err != nil {
		t.Fatal(err)
	}
	if err := c.handleMessage(p); err != nil {
		t.Fatalf("second queued event = %v", err)
	}
	if err := c.handleMessage(p); !errors.Is(err, ErrEventBacklog) {
		t.Fatalf("third event error = %v, want ErrEventBacklog", err)
	}
}

func TestParseHeartbeatTimeout(t *testing.T) {
	oldGrace := heartbeatGrace
	heartbeatGrace = 5 * time.Second
	defer func() { heartbeatGrace = oldGrace }()

	got, err := parseHeartbeatTimeout([]byte(`{"pingInterval":25000,"pingTimeout":20000}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := 50 * time.Second; got != want {
		t.Fatalf("timeout = %v, want %v", got, want)
	}
}

// A TCP socket can stay ESTABLISHED after suspend or an interface switch.
// Missing Engine.IO pings must still close Events so Session.manage starts its
// reconnect path without waiting for the kernel's much longer TCP timeout.
func TestMissingHeartbeatClosesConnection(t *testing.T) {
	oldGrace := heartbeatGrace
	heartbeatGrace = 10 * time.Millisecond
	defer func() { heartbeatGrace = oldGrace }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"boards.yandex.ru"},
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText,
			[]byte(`0{"sid":"test","pingInterval":10,"pingTimeout":10}`))
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("40"))
		// Deliberately send no Engine.IO ping and leave TCP open.
		_, _, _ = conn.Read(r.Context())
	}))
	defer srv.Close()

	client, err := Dial(context.Background(), strings.Replace(srv.URL, "http://", "ws://", 1), "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatal("unexpected socket event")
		}
	case <-time.After(time.Second):
		t.Fatal("stale websocket was not closed after heartbeat timeout")
	}
	if err := client.Err(); err == nil || !strings.Contains(err.Error(), "heartbeat timeout") {
		t.Fatalf("terminal error = %v, want heartbeat timeout", err)
	}
}
