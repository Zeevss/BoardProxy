package bond

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSend struct {
	packet  []byte
	receipt chan struct{}
}

type fakeLane struct {
	sent   chan fakeSend
	recv   chan []byte
	done   chan struct{}
	once   sync.Once
	target int
	rtt    time.Duration
}

func newFakeLane() *fakeLane {
	return &fakeLane{
		sent:   make(chan fakeSend, 16),
		recv:   make(chan []byte, 16),
		done:   make(chan struct{}),
		target: 1 << 20,
		rtt:    20 * time.Millisecond,
	}
}

func (l *fakeLane) SendTracked(ctx context.Context, packet []byte) (<-chan struct{}, error) {
	receipt := make(chan struct{})
	copyPacket := append([]byte(nil), packet...)
	select {
	case l.sent <- fakeSend{packet: copyPacket, receipt: receipt}:
		return receipt, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.done:
		return nil, ErrClosed
	}
}

func (l *fakeLane) Recv() <-chan []byte   { return l.recv }
func (l *fakeLane) Done() <-chan struct{} { return l.done }
func (l *fakeLane) TargetBatchSize() int  { return l.target }
func (l *fakeLane) RTT() time.Duration    { return l.rtt }
func (l *fakeLane) Close() error {
	l.once.Do(func() {
		close(l.done)
		close(l.recv)
	})
	return nil
}

func (l *fakeLane) inject(t *testing.T, packet []byte) {
	t.Helper()
	select {
	case l.recv <- append([]byte(nil), packet...):
	case <-l.done:
		t.Fatal("inject into closed lane")
	}
}

func receiveSend(t *testing.T, lane *fakeLane) fakeSend {
	t.Helper()
	select {
	case sent := <-lane.sent:
		return sent
	case <-time.After(2 * time.Second):
		t.Fatal("lane did not receive a packet")
		return fakeSend{}
	}
}

func receivePayload(t *testing.T, c *Conn) string {
	t.Helper()
	select {
	case payload, ok := <-c.Recv():
		if !ok {
			t.Fatalf("bond receive closed: %v", c.Err())
		}
		return string(payload)
	case <-time.After(2 * time.Second):
		t.Fatal("bond did not deliver payload")
		return ""
	}
}

func TestConnStripesAndDeliversAcrossLanesWithoutGlobalHOL(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	lane1 := newFakeLane()
	lane2 := newFakeLane()
	if err := c.AddLane(1, lane1); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLane(2, lane2); err != nil {
		t.Fatal(err)
	}

	if err := c.Send(context.Background(), []byte("zero")); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), []byte("one")); err != nil {
		t.Fatal(err)
	}
	first := receiveSend(t, lane1)
	second := receiveSend(t, lane2)
	firstSeq, _, ok := decodePacket(first.packet)
	if !ok || firstSeq != 0 {
		t.Fatalf("first packet seq=%d ok=%v", firstSeq, ok)
	}
	secondSeq, _, ok := decodePacket(second.packet)
	if !ok || secondSeq != 1 {
		t.Fatalf("second packet seq=%d ok=%v", secondSeq, ok)
	}

	// Lane 2 arrives first and must be delivered immediately. Per-stream mux
	// offsets, not the bond PacketID, restore TCP byte order.
	lane2.inject(t, second.packet)
	if got := receivePayload(t, c); got != "one" {
		t.Fatalf("first arrival payload = %q", got)
	}
	lane1.inject(t, first.packet)
	if got := receivePayload(t, c); got != "zero" {
		t.Fatalf("second arrival payload = %q", got)
	}

	// Replay of an already delivered PacketID is ignored.
	lane1.inject(t, first.packet)
	select {
	case got := <-c.Recv():
		t.Fatalf("duplicate payload delivered: %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	close(first.receipt)
	close(second.receipt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.ConfirmedBytes(); got != uint64(len("zero")+len("one")) {
		t.Fatalf("confirmed bytes = %d", got)
	}
}

func TestOrderedCompatibilityModePreservesV3PacketOrder(t *testing.T) {
	c := New(Options{Ordered: true})
	defer c.Close()
	lane := newFakeLane()
	if err := c.AddLane(1, lane); err != nil {
		t.Fatal(err)
	}
	lane.inject(t, encodePacket(1, []byte("one")))
	select {
	case got := <-c.Recv():
		t.Fatalf("ordered mode delivered across a gap: %q", got)
	case <-time.After(30 * time.Millisecond):
	}
	lane.inject(t, encodePacket(0, []byte("zero")))
	if got := receivePayload(t, c); got != "zero" {
		t.Fatalf("first ordered payload=%q", got)
	}
	if got := receivePayload(t, c); got != "one" {
		t.Fatalf("second ordered payload=%q", got)
	}
}

func TestConnRetransmitsUnconfirmedPacketAfterLaneFailure(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	lane1 := newFakeLane()
	lane2 := newFakeLane()
	if err := c.AddLane(1, lane1); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLane(2, lane2); err != nil {
		t.Fatal(err)
	}

	if err := c.Send(context.Background(), []byte("replay-me")); err != nil {
		t.Fatal(err)
	}
	original := receiveSend(t, lane1)
	_ = lane1.Close() // no receipt: packet must move to lane 2
	replayed := receiveSend(t, lane2)
	if string(replayed.packet) != string(original.packet) {
		t.Fatal("retransmit changed PacketID or payload")
	}
	close(replayed.receipt)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.ConfirmedBytes(); got != uint64(len("replay-me")) {
		t.Fatalf("confirmed bytes = %d", got)
	}
	eventuallyBond(t, func() bool { return c.LaneCount() == 1 })
}

func TestDrainLaneStopsNewSchedulingAndWaitsForReceipt(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	lane1 := newFakeLane()
	lane2 := newFakeLane()
	if err := c.AddLane(1, lane1); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLane(2, lane2); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), []byte("first")); err != nil {
		t.Fatal(err)
	}
	first := receiveSend(t, lane1)

	drained := make(chan error, 1)
	go func() { drained <- c.DrainLane(context.Background(), 1) }()
	eventuallyBond(t, func() bool {
		c.laneMu.Lock()
		defer c.laneMu.Unlock()
		return c.draining[1]
	})

	if err := c.Send(context.Background(), []byte("second")); err != nil {
		t.Fatal(err)
	}
	second := receiveSend(t, lane2)
	select {
	case err := <-drained:
		t.Fatalf("drain completed before receipt: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(first.receipt)
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not complete")
	}
	close(second.receipt)
	if got := c.LaneCount(); got != 1 {
		t.Fatalf("lane count after drain = %d", got)
	}
}

func TestSchedulerPrefersEarlierEstimatedDelivery(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	slow := newFakeLane()
	slow.rtt = 100 * time.Millisecond
	fast := newFakeLane()
	fast.rtt = 10 * time.Millisecond
	if err := c.AddLane(1, slow); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLane(2, fast); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), []byte("fast")); err != nil {
		t.Fatal(err)
	}
	sent := receiveSend(t, fast)
	close(sent.receipt)
}

func TestConnReplayLimitBackpressuresSender(t *testing.T) {
	c := New(Options{MaxUnackedBytes: packetHeaderLen + 4})
	defer c.Close()
	lane := newFakeLane()
	if err := c.AddLane(1, lane); err != nil {
		t.Fatal(err)
	}

	if err := c.Send(context.Background(), []byte("full")); err != nil {
		t.Fatal(err)
	}
	first := receiveSend(t, lane)
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- c.Send(context.Background(), []byte("next"))
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second Send escaped replay backpressure: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(first.receipt)
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Send did not resume after receipt")
	}
	second := receiveSend(t, lane)
	close(second.receipt)
}

func TestCancelledSendDoesNotConsumePacketID(t *testing.T) {
	c := New(Options{})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Send(ctx, []byte("cancelled")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled Send error = %v", err)
	}

	lane := newFakeLane()
	if err := c.AddLane(1, lane); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), []byte("first")); err != nil {
		t.Fatal(err)
	}
	sent := receiveSend(t, lane)
	seq, payload, ok := decodePacket(sent.packet)
	if !ok || seq != 0 || string(payload) != "first" {
		t.Fatalf("packet after cancellation: seq=%d payload=%q ok=%v", seq, payload, ok)
	}
	close(sent.receipt)
}

func TestConnFailsOnReorderOverflow(t *testing.T) {
	c := New(Options{MaxReorderBytes: 4})
	lane := newFakeLane()
	if err := c.AddLane(1, lane); err != nil {
		t.Fatal(err)
	}
	lane.inject(t, encodePacket(1, []byte("12345")))
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("bond did not close after reorder overflow")
	}
	if !errors.Is(c.Err(), ErrReorderOverflow) {
		t.Fatalf("error = %v, want ErrReorderOverflow", c.Err())
	}
}

func TestConnRejectsDuplicateLane(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	if err := c.AddLane(1, newFakeLane()); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLane(1, newFakeLane()); !errors.Is(err, ErrDuplicateLane) {
		t.Fatalf("duplicate AddLane error = %v", err)
	}
}

func TestConnClosesWhenLastLaneIsLost(t *testing.T) {
	c := New(Options{})
	lane := newFakeLane()
	if err := c.AddLane(1, lane); err != nil {
		t.Fatal(err)
	}
	if err := lane.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("bond stayed alive without physical lanes")
	}
	if !errors.Is(c.Err(), ErrNoLanes) {
		t.Fatalf("bond error = %v, want ErrNoLanes", c.Err())
	}
}

func eventuallyBond(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
