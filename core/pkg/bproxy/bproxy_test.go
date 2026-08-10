package bproxy

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bproxy-core/internal/mux"
	"bproxy-core/internal/proxy"
)

func TestRunEmptyKeylinkGoesConnectingThenError(t *testing.T) {
	c := New(Config{}) // без keylink — DialClient обязан отказать
	var statuses []Status
	c.OnStatus(func(s Status, _ error) { statuses = append(statuses, s) })

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("Run без keylink должен вернуть ошибку")
	}
	// Ожидаем переход Connecting → Error, без Connected.
	if len(statuses) != 2 || statuses[0] != StatusConnecting || statuses[1] != StatusError {
		t.Fatalf("статусы = %v, хочу [connecting error]", statuses)
	}
	if c.Status() != StatusError {
		t.Fatalf("Status() = %q, хочу error", c.Status())
	}
}

func TestRunRetriesInitialFailureWhenEnabled(t *testing.T) {
	c := New(Config{Keylink: "test", RetryInitial: true})
	conn := newReconnectTestConn()
	sess := mux.New(conn, mux.Options{Client: true})
	var dials atomic.Int32
	c.dial = func(context.Context, Config, *slog.Logger) (*mux.Session, error) {
		if dials.Add(1) == 1 {
			return nil, errors.New("temporary board outage")
		}
		return sess, nil
	}
	c.serve = func(ctx context.Context, _ string, _ proxy.Dialer, _ *slog.Logger, _ proxy.Options) error {
		<-ctx.Done()
		return ctx.Err()
	}
	connected := make(chan struct{})
	c.OnStatus(func(status Status, _ error) {
		if status == StatusConnected {
			select {
			case <-connected:
			default:
				close(connected)
			}
		}
	})
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not recover from initial connection failure")
	}
	c.Stop()
	if err := <-done; err != nil {
		t.Fatalf("Run after recovery and Stop: %v", err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("dial attempts = %d, want 2", got)
	}
}

func TestRunDoesNotRetryPermanentInitialConfigurationError(t *testing.T) {
	c := New(Config{RetryInitial: true})
	started := time.Now()
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("invalid keylink must remain fatal with initial retry enabled")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("permanent error was retried for %v", elapsed)
	}
}

func TestStopBeforeRunReturnsNil(t *testing.T) {
	c := New(Config{Keylink: "bproxy://whatever"})
	c.Stop()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run после Stop = %v, хочу nil", err)
	}
}

func TestConcurrentRunRejected(t *testing.T) {
	c := New(Config{Keylink: "test"})
	started := make(chan struct{})
	var once sync.Once
	c.dial = func(ctx context.Context, _ Config, _ *slog.Logger) (*mux.Session, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Run(context.Background()) }()
	<-started
	if err := c.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("concurrent Run = %v, want ErrAlreadyRunning", err)
	}
	c.Stop()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Run after Stop = %v", err)
	}
}

func TestListenOrDefault(t *testing.T) {
	if got := (&Config{}).listenOrDefault(); got != "127.0.0.1:1080" {
		t.Fatalf("дефолтный listen = %q", got)
	}
	if got := (&Config{Listen: "0.0.0.0:9050"}).listenOrDefault(); got != "0.0.0.0:9050" {
		t.Fatalf("явный listen = %q", got)
	}
}

func TestFillFromStatsMapsFields(t *testing.T) {
	var m Metrics
	fillFromStats(&m, mux.SessionStats{
		Streams:        2,
		Written:        1000,
		Received:       2000,
		TransportAcked: 900,
		BacklogFrames:  3,
		BacklogBytes:   4096,
		BlockedWriters: 1,
		RTT:            50 * time.Millisecond,
		PerStream: []mux.StreamStats{
			{ID: 1, Target: "a:80", Written: 400, Received: 800},
			{ID: 3, Target: "b:443", Written: 600, Received: 1200},
		},
	})
	if m.Streams != 2 || m.TotalTx != 1000 || m.TotalRx != 2000 ||
		m.TransportAcked != 900 || m.BacklogFrames != 3 || m.BacklogBytes != 4096 ||
		m.BlockedWriters != 1 || m.RTT != 50*time.Millisecond {
		t.Fatalf("агрегат смаппился неверно: %+v", m)
	}
	if len(m.Details) != 2 || m.Details[0].Target != "a:80" || m.Details[1].Rx != 1200 {
		t.Fatalf("per-stream смаппился неверно: %+v", m.Details)
	}
}

type reconnectTestConn struct {
	recv chan []byte
	once sync.Once
}

func newReconnectTestConn() *reconnectTestConn {
	return &reconnectTestConn{recv: make(chan []byte)}
}

func (c *reconnectTestConn) Send(context.Context, []byte) error { return nil }
func (c *reconnectTestConn) Recv() <-chan []byte                { return c.recv }
func (c *reconnectTestConn) TargetBatchSize() int               { return 1024 }
func (c *reconnectTestConn) RTT() time.Duration                 { return 0 }
func (c *reconnectTestConn) Close() error {
	c.once.Do(func() { close(c.recv) })
	return nil
}

// TestRunReconnectsAfterEstablishedSessionCloses проверяет полный клиентский
// lifecycle без сети: закрытие первой mux-сессии имитирует GOAWAY, после чего
// Run сообщает reconnecting, делает второй dial и снова становится connected.
func TestRunReconnectsAfterEstablishedSessionCloses(t *testing.T) {
	c := New(Config{Keylink: "test"})
	firstConn := newReconnectTestConn()
	secondConn := newReconnectTestConn()
	first := mux.New(firstConn, mux.Options{Client: true})
	second := mux.New(secondConn, mux.Options{Client: true})

	var dialMu sync.Mutex
	dials := 0
	c.dial = func(context.Context, Config, *slog.Logger) (*mux.Session, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		dials++
		switch dials {
		case 1:
			return first, nil
		case 2:
			return second, nil
		default:
			return nil, errors.New("unexpected extra dial")
		}
	}
	secondServing := make(chan struct{})
	var serveCalls atomic.Int32
	c.serve = func(ctx context.Context, _ string, d proxy.Dialer, _ *slog.Logger, _ proxy.Options) error {
		serveCalls.Add(1)
		if _, ok := d.(*sessionDialer); !ok {
			return errors.New("proxy did not receive persistent session dialer")
		}
		<-ctx.Done()
		return ctx.Err()
	}

	var statusMu sync.Mutex
	var statuses []Status
	c.OnStatus(func(s Status, _ error) {
		statusMu.Lock()
		statuses = append(statuses, s)
		connected := 0
		for _, status := range statuses {
			if status == StatusConnected {
				connected++
			}
		}
		statusMu.Unlock()
		if s == StatusConnected && connected == 1 {
			firstConn.Close() // peer/link disappeared; listener must remain alive
		}
		if s == StatusConnected && connected == 2 {
			close(secondServing)
		}
	})
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()

	select {
	case <-secondServing:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not reconnect to the second session")
	}
	c.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	statusMu.Lock()
	defer statusMu.Unlock()
	if !containsStatus(statuses, StatusReconnecting) {
		t.Fatalf("statuses %v do not contain reconnecting", statuses)
	}
	connected := 0
	for _, s := range statuses {
		if s == StatusConnected {
			connected++
		}
	}
	if connected != 2 {
		t.Fatalf("connected transitions = %d, want 2 (statuses=%v)", connected, statuses)
	}
	if got := serveCalls.Load(); got != 1 {
		t.Fatalf("proxy Serve calls = %d, want 1 across reconnect", got)
	}
	if len(statuses) < 2 || statuses[len(statuses)-2] != StatusStopping || statuses[len(statuses)-1] != StatusDisconnected {
		t.Fatalf("final statuses = %v, want ... stopping disconnected", statuses)
	}
}

func TestReconnectClosesCurrentSessionAndPublishesStatus(t *testing.T) {
	c := New(Config{Keylink: "test"})
	conn := newReconnectTestConn()
	sess := mux.New(conn, mux.Options{Client: true})

	c.mu.Lock()
	c.running = true
	c.sess = sess
	c.mu.Unlock()

	status := make(chan Status, 1)
	c.OnStatus(func(value Status, _ error) {
		select {
		case status <- value:
		default:
		}
	})

	if !c.Reconnect() {
		t.Fatal("Reconnect rejected an active session")
	}
	if c.Reconnect() {
		t.Fatal("second Reconnect was accepted while reconnecting")
	}

	select {
	case got := <-status:
		if got != StatusReconnecting {
			t.Fatalf("status = %s, want %s", got, StatusReconnecting)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnecting status was not published")
	}

	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("current mux session was not closed")
	}
}

func containsStatus(statuses []Status, want Status) bool {
	for _, s := range statuses {
		if s == want {
			return true
		}
	}
	return false
}
