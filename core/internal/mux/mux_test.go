package mux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/link"
	"bproxy-core/internal/proto"
)

// newTestSession builds a bare Session with just the queue state pickBatch and
// enqueue* need, bypassing New()'s conn/writer/reader setup — for scheduler
// tests that drive the queue directly.
func newTestSession(t *testing.T, coalesceTarget int) *Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Session{ctx: ctx, dataByStream: make(map[uint32][]frameOut)}
	s.coalesceTarget.Store(int64(coalesceTarget))
	s.qCond = sync.NewCond(&s.qMu)
	return s
}

// TestPickBatchPrefersControl checks the scheduler drains the priority system
// channel (SYN/RESET) ahead of queued data frames, one control frame per
// batch, and coalesces same-stream data frames into a single batch.
func TestPickBatchPrefersControl(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)

	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("d0")})
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("d1")})
	_ = s.enqueueControl(frameOut{typ: proto.FrameSyn, stream: 1})
	_ = s.enqueueControl(frameOut{typ: proto.FrameReset, stream: 3})

	batch, ok := s.pickBatch()
	if !ok || len(batch) != 1 || batch[0].typ != proto.FrameSyn {
		t.Fatalf("first batch = %+v ok=%v, want a lone SYN", batch, ok)
	}
	batch, ok = s.pickBatch()
	if !ok || len(batch) != 1 || batch[0].typ != proto.FrameReset {
		t.Fatalf("second batch = %+v ok=%v, want a lone RESET", batch, ok)
	}
	batch, ok = s.pickBatch()
	if !ok || len(batch) != 2 || batch[0].typ != proto.FrameData || batch[1].typ != proto.FrameData {
		t.Fatalf("third batch = %+v ok=%v, want both data frames coalesced", batch, ok)
	}
}

// TestPickBatchRoundRobinsAcrossStreams checks data frames from different
// streams interleave one-per-stream-per-lap rather than draining one stream's
// backlog before touching another's.
func TestPickBatchRoundRobinsAcrossStreams(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)

	// Stream 1 queues 3 frames first, stream 2 queues 2 — a plain FIFO would
	// drain all of stream 1 before touching stream 2.
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("1a")})
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("1b")})
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("1c")})
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 2, payload: []byte("2a")})
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 2, payload: []byte("2b")})

	batch, ok := s.pickBatch()
	if !ok || len(batch) != 5 {
		t.Fatalf("batch = %+v ok=%v, want all 5 queued frames in one batch", batch, ok)
	}
	wantStreams := []uint32{1, 2, 1, 2, 1}
	for i, f := range batch {
		if f.stream != wantStreams[i] {
			t.Fatalf("frame %d stream = %d, want %d (order %v)", i, f.stream, wantStreams[i], streamsOf(batch))
		}
	}
}

func streamsOf(batch []frameOut) []uint32 {
	out := make([]uint32, len(batch))
	for i, f := range batch {
		out[i] = f.stream
	}
	return out
}

// TestPickBatchDoesNotWaitForMoreData checks a single already-queued frame is
// returned immediately, without waiting to see if more arrives.
func TestPickBatchDoesNotWaitForMoreData(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)
	_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("solo")})

	done := make(chan []frameOut, 1)
	start := time.Now()
	go func() {
		batch, _ := s.pickBatch()
		done <- batch
	}()
	select {
	case batch := <-done:
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("pickBatch took %v, expected an immediate return", elapsed)
		}
		if len(batch) != 1 {
			t.Fatalf("batch = %+v, want exactly the one queued frame", batch)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pickBatch blocked instead of returning the one queued frame")
	}
}

// TestCoalesceTargetCapsBatch checks pickBatch stops once the batch reaches
// coalesceTarget bytes, leaving the remainder queued for the next call.
func TestCoalesceTargetCapsBatch(t *testing.T) {
	// Each frame encodes to batchLenLen+headerLen+4-byte payload = 13 bytes.
	// drainDataLocked checks the running total *before* adding each frame, so
	// a target of 20 admits a 2nd frame (13 < 20) but not a 3rd (26 >= 20).
	s := newTestSession(t, 20)
	for i := 0; i < 5; i++ {
		_ = s.enqueueData(frameOut{typ: proto.FrameData, stream: 1, payload: []byte("data")})
	}

	first, ok := s.pickBatch()
	if !ok || len(first) != 2 {
		t.Fatalf("first batch = %+v ok=%v, want exactly 2 frames under the target", first, ok)
	}
	second, ok := s.pickBatch()
	if !ok || len(second) != 2 {
		t.Fatalf("second batch = %+v ok=%v, want the next 2 frames", second, ok)
	}
	third, ok := s.pickBatch()
	if !ok || len(third) != 1 {
		t.Fatalf("third batch = %+v ok=%v, want the last remaining frame", third, ok)
	}
}

type telemetryConn struct {
	stubConn
	confirmed atomic.Uint64
}

func (c *telemetryConn) ConfirmedBytes() uint64 { return c.confirmed.Load() }

func TestSessionStatsReportsTransportPressure(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)
	conn := &telemetryConn{}
	conn.confirmed.Store(1234)
	s.conn = conn

	first := frameOut{typ: proto.FrameData, stream: 1, payload: []byte("first")}
	second := frameOut{typ: proto.FrameData, stream: 3, payload: []byte("second")}
	if err := s.enqueueData(first); err != nil {
		t.Fatal(err)
	}
	if err := s.enqueueData(second); err != nil {
		t.Fatal(err)
	}

	stats := s.Stats()
	wantBytes := encodedLen(first) + encodedLen(second)
	if stats.TransportAcked != 1234 || stats.BacklogFrames != 2 || stats.BacklogBytes != wantBytes {
		t.Fatalf("queued stats = %+v, want acked=1234 frames=2 bytes=%d", stats, wantBytes)
	}

	if _, ok := s.pickBatch(); !ok {
		t.Fatal("pickBatch unexpectedly closed")
	}
	stats = s.Stats()
	if stats.BacklogFrames != 0 || stats.BacklogBytes != 0 {
		t.Fatalf("drained backlog was not cleared: %+v", stats)
	}
}

func TestSessionStatsReportsBlockedWriter(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)
	s.conn = &stubConn{}
	frame := frameOut{typ: proto.FrameData, stream: 1, payload: []byte("data")}
	for range dataQueueCap {
		if err := s.enqueueData(frame); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- s.enqueueData(frame)
	}()
	if err := eventuallyMux(func() bool {
		return s.Stats().BlockedWriters == 1
	}); err != nil {
		t.Fatalf("writer pressure was not reported: %v", err)
	}

	if _, ok := s.pickBatch(); !ok {
		t.Fatal("pickBatch unexpectedly closed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked writer did not resume after queue drain")
	}
}

// linkedSessions wires a client and server mux over two links sharing an
// in-memory board page. MaxPayload is small to force fragmentation.
func linkedSessions(t *testing.T) (client, server *Session) {
	return linkedSessionsWindow(t, 0)
}

// linkedSessionsWindow is linkedSessions with an explicit per-stream window
// (0 = default), for exercising flow control.
func linkedSessionsWindow(t *testing.T, streamWindow int) (client, server *Session) {
	return linkedSessionsWindowVersion(t, streamWindow, proto.Version)
}

func linkedSessionsWindowVersion(t *testing.T, streamWindow, version int) (client, server *Session) {
	t.Helper()
	ctx := context.Background()
	b := memory.NewBoard()
	sa := b.NewSession("client")
	sb := b.NewSession("server")
	if _, err := sa.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	la := link.New(sa, codec.Base64Codec{}, link.Options{RecvWindow: 8})
	lb := link.New(sb, codec.Base64Codec{}, link.Options{RecvWindow: 8})
	client = New(la, Options{Version: version, Client: true, MaxPayload: 16, StreamWindow: streamWindow})
	server = New(lb, Options{Version: version, Client: false, MaxPayload: 16, StreamWindow: streamWindow})
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client, server
}

func TestLegacyV2AndV3MuxEchoStillWork(t *testing.T) {
	for _, version := range []int{2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			client, server := linkedSessionsWindowVersion(t, 48, version)
			echoServer(t, server)
			st, err := client.OpenStream(fmt.Sprintf("legacy-v%d:80", version))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Repeat(fmt.Sprintf("v%d", version), 80)
			if _, err := io.WriteString(st, want); err != nil {
				t.Fatal(err)
			}
			if err := st.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			if got := readAllWithTimeout(t, st, 5*time.Second); got != want {
				t.Fatalf("legacy echo=%q want=%q", got, want)
			}
		})
	}
}

func TestDatagramPreservesMessageBoundariesAndAddresses(t *testing.T) {
	client, server := linkedSessions(t)
	clientDatagram, err := client.OpenDatagram()
	if err != nil {
		t.Fatal(err)
	}
	serverDatagram, err := server.AcceptDatagram(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []DatagramPacket{
		{Target: "dns.example:53", Payload: []byte("first")},
		{Target: "[2001:db8::1]:443", Payload: []byte("second-message")},
	}
	for _, packet := range want {
		if err := clientDatagram.Send(packet.Target, packet.Payload); err != nil {
			t.Fatal(err)
		}
	}
	for _, expected := range want {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got, err := serverDatagram.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if got.Target != expected.Target || string(got.Payload) != string(expected.Payload) {
			t.Fatalf("packet = %+v, want %+v", got, expected)
		}
	}

	if err := serverDatagram.Send("127.0.0.1:5353", []byte("reply")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := clientDatagram.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != "127.0.0.1:5353" || string(got.Payload) != "reply" {
		t.Fatalf("reply = %+v", got)
	}
}

// echoServer accepts streams and echoes each stream's bytes back, verifying the
// SYN target along the way.
func echoServer(t *testing.T, server *Session) {
	go func() {
		for {
			st, err := server.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go func(st *Stream) {
				if st.Target() == "" {
					t.Errorf("accepted stream has empty target")
				}
				data, _ := io.ReadAll(st)
				_, _ = st.Write(data)
				_ = st.CloseWrite()
			}(st)
		}
	}()
}

func TestMuxEchoFragmented(t *testing.T) {
	client, server := linkedSessions(t)
	echoServer(t, server)

	// Longer than MaxPayload (16) to exercise fragmentation and reassembly.
	msg := strings.Repeat("The quick brown fox. ", 8)
	st, err := client.OpenStream("example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(st, msg); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got := readAllWithTimeout(t, st, 5*time.Second)
	if got != msg {
		t.Fatalf("echo mismatch:\n got %q\nwant %q", got, msg)
	}
}

func TestMuxConcurrentStreams(t *testing.T) {
	client, server := linkedSessions(t)
	echoServer(t, server)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := fmt.Sprintf("stream-%d payload %s", i, strings.Repeat("x", i))
			st, err := client.OpenStream(fmt.Sprintf("host-%d:80", i))
			if err != nil {
				errs <- err
				return
			}
			_, _ = io.WriteString(st, msg)
			_ = st.CloseWrite()
			got := readAllWithTimeout(t, st, 5*time.Second)
			if got != msg {
				errs <- fmt.Errorf("stream %d: got %q want %q", i, got, msg)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestMuxResetSurfacesToPeer(t *testing.T) {
	client, server := linkedSessions(t)

	accepted := make(chan *Stream, 1)
	go func() {
		st, err := server.AcceptStream(context.Background())
		if err == nil {
			accepted <- st
		}
	}()

	st, err := client.OpenStream("host:80")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}

	select {
	case srvSt := <-accepted:
		_, err := srvSt.Read(make([]byte, 4))
		if err != ErrStreamReset {
			t.Fatalf("server read err = %v, want ErrStreamReset", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted the stream")
	}
}

// TestStreamWindowBackpressure checks a fast writer blocks once it has sent a
// window's worth of unread data, and resumes when the reader drains it.
func TestStreamWindowBackpressure(t *testing.T) {
	const window = 48
	client, server := linkedSessionsWindow(t, window)

	accepted := make(chan *Stream, 1)
	go func() {
		st, err := server.AcceptStream(context.Background())
		if err == nil {
			accepted <- st
		}
	}()

	st, err := client.OpenStream("sink:80")
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("z", 200) // far more than the window
	writeDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(st, payload)
		writeDone <- err
		_ = st.CloseWrite()
	}()

	// The server is not reading yet, so the writer must block after ~one window.
	select {
	case <-writeDone:
		t.Fatal("write completed without the reader consuming — no backpressure")
	case <-time.After(300 * time.Millisecond):
	}

	// Drain on the server: this frees window space (WINDOW_UPDATE) and unblocks.
	srvSt := <-accepted
	got := readAllWithTimeout(t, srvSt, 5*time.Second)
	if got != payload {
		t.Fatalf("server got %d bytes, want %d", len(got), len(payload))
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not unblock after reader drained the window")
	}
}

// TestNoHeadOfLineBlocking checks a stalled reader on one stream does not block
// a second stream from completing.
func TestNoHeadOfLineBlocking(t *testing.T) {
	const window = 48
	client, server := linkedSessionsWindow(t, window)

	// Server: echo the "echo" stream, but never read the "sink" stream.
	go func() {
		for {
			st, err := server.AcceptStream(context.Background())
			if err != nil {
				return
			}
			if st.Target() == "echo" {
				go func(st *Stream) {
					data, _ := io.ReadAll(st)
					_, _ = st.Write(data)
					_ = st.CloseWrite()
				}(st)
			}
			// "sink" stream is intentionally left unread.
		}
	}()

	// Stalled stream: fill well beyond its window; its Write will block — fine.
	sink, err := client.OpenStream("sink")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.WriteString(sink, strings.Repeat("x", 500)) }()

	// Independent stream must still round-trip promptly.
	echo, err := client.OpenStream("echo")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello over an independent stream"
	if _, err := io.WriteString(echo, msg); err != nil {
		t.Fatal(err)
	}
	if err := echo.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got := readAllWithTimeout(t, echo, 3*time.Second)
	if got != msg {
		t.Fatalf("echo stream blocked by stalled sink: got %q want %q", got, msg)
	}
}

func TestStreamReordersDataAndWaitsForEarlyFin(t *testing.T) {
	sess := &Session{
		version:         proto.Version,
		initialWindow:   64,
		windowThreshold: 32,
		streams:         make(map[uint32]*Stream),
	}
	st := newStream(1, sess, false, 64)
	sess.streams[1] = st

	if !st.onFinAt(6) {
		t.Fatal("valid early FIN rejected")
	}
	if !st.deliverAt(3, []byte("def")) {
		t.Fatal("out-of-order tail rejected")
	}
	if !st.deliverAt(3, []byte("def")) {
		t.Fatal("exact duplicate rejected")
	}
	st.recvMu.Lock()
	earlyEOF := st.readErr
	st.recvMu.Unlock()
	if earlyEOF != nil {
		t.Fatalf("early FIN exposed before missing bytes arrived: %v", earlyEOF)
	}
	if !st.deliverAt(0, []byte("abc")) {
		t.Fatal("leading range rejected")
	}
	data, err := io.ReadAll(st)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("reassembled data=%q err=%v", data, err)
	}
	if got := st.Stats().Received; got != 6 {
		t.Fatalf("duplicate inflated received bytes to %d", got)
	}
}

func TestMuxBuffersDataAndFinThatOvertakeSyn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Session{
		version:         proto.Version,
		initialWindow:   64,
		windowThreshold: 32,
		client:          false,
		streams:         make(map[uint32]*Stream),
		datagrams:       make(map[uint32]*Datagram),
		orphans:         make(map[uint32][]frameOut),
		accept:          make(chan *Stream, 1),
		ctx:             ctx,
	}
	s.dispatch(frameOut{
		typ: proto.FrameData, stream: 1,
		payload: encodeStreamData(0, []byte("before-syn")),
	})
	s.dispatch(frameOut{
		typ: proto.FrameFin, stream: 1,
		payload: encodeFinalOffset(uint64(len("before-syn"))),
	})
	if len(s.orphans[1]) != 2 {
		t.Fatalf("orphan frames = %d, want 2", len(s.orphans[1]))
	}
	s.dispatch(frameOut{
		typ: proto.FrameSyn, stream: 1,
		payload: encodeSyn(64, "example:80"),
	})
	st := <-s.accept
	data, err := io.ReadAll(st)
	if err != nil || string(data) != "before-syn" {
		t.Fatalf("pre-SYN data=%q err=%v", data, err)
	}
	if len(s.orphans) != 0 || s.orphanBytes != 0 {
		t.Fatalf("orphan buffer not released: frames=%d bytes=%d", len(s.orphans), s.orphanBytes)
	}
}

func TestMuxBuffersDatagramThatOvertakesOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Session{
		version:        proto.Version,
		client:         false,
		streams:        make(map[uint32]*Stream),
		datagrams:      make(map[uint32]*Datagram),
		orphans:        make(map[uint32][]frameOut),
		acceptDatagram: make(chan *Datagram, 1),
		ctx:            ctx,
	}
	payload, ok := encodeDatagram("dns.example:53", []byte("query"))
	if !ok {
		t.Fatal("encode datagram")
	}
	s.dispatch(frameOut{typ: proto.FrameDatagram, stream: 1, payload: payload})
	s.dispatch(frameOut{typ: proto.FrameDatagramOpen, stream: 1})
	d := <-s.acceptDatagram
	packet, err := d.Receive(context.Background())
	if err != nil || packet.Target != "dns.example:53" || string(packet.Payload) != "query" {
		t.Fatalf("pre-open datagram=%+v err=%v", packet, err)
	}
}

func TestMissingRangeOnOneStreamDoesNotBlockAnother(t *testing.T) {
	sess := &Session{
		version:         proto.Version,
		initialWindow:   64,
		windowThreshold: 32,
		streams:         make(map[uint32]*Stream),
	}
	blocked := newStream(1, sess, false, 64)
	fast := newStream(3, sess, false, 64)
	sess.streams[1] = blocked
	sess.streams[3] = fast

	if !blocked.deliverAt(4, []byte("tail")) {
		t.Fatal("blocked stream tail rejected")
	}
	if !fast.deliverAt(0, []byte("fast")) || !fast.onFinAt(4) {
		t.Fatal("independent stream frames rejected")
	}
	data, err := io.ReadAll(fast)
	if err != nil || string(data) != "fast" {
		t.Fatalf("independent stream data=%q err=%v", data, err)
	}
	if blocked.recvNext != 0 {
		t.Fatalf("blocked stream unexpectedly advanced to %d", blocked.recvNext)
	}
}

func TestMaxStreamDataIsAbsoluteAndMonotonic(t *testing.T) {
	sess := &Session{version: proto.Version, initialWindow: 64}
	st := newStream(1, sess, false, 10)
	st.setSendMax(20)
	st.setSendMax(15)
	st.sendMu.Lock()
	got := st.sendMax
	st.sendMu.Unlock()
	if got != 20 {
		t.Fatalf("send max regressed/inflated to %d", got)
	}
}

func TestStreamRejectsConflictingRangesAndDataPastFin(t *testing.T) {
	sess := &Session{
		version: proto.Version, initialWindow: 32, windowThreshold: 16,
		streams: make(map[uint32]*Stream),
	}
	st := newStream(1, sess, false, 32)
	if !st.deliverAt(4, []byte("efgh")) {
		t.Fatal("valid sparse range rejected")
	}
	if st.deliverAt(6, []byte("conflict")) {
		t.Fatal("overlapping range accepted")
	}
	if !st.onFinAt(8) {
		t.Fatal("valid final offset rejected")
	}
	if st.deliverAt(8, []byte("past-fin")) {
		t.Fatal("data past final offset accepted")
	}
	if st.deliverAt(31, []byte("too-large")) {
		t.Fatal("data past receive limit accepted")
	}
}

// TestIdleStreamsFindsExpiredButNotActive unit-tests idleStreams' detection
// logic directly (no real ticker involved): a stream whose lastActivity is
// older than streamIdleTimeout is flagged, a fresh one is not.
func TestIdleStreamsFindsExpiredButNotActive(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)
	s.streamIdleTimeout = 50 * time.Millisecond

	stale := newStream(1, s, false, 100)
	stale.lastActivity.Store(time.Now().Add(-time.Hour).UnixNano())
	fresh := newStream(2, s, false, 100)
	s.streams = map[uint32]*Stream{1: stale, 2: fresh}

	idle := s.idleStreams()
	if len(idle) != 1 || idle[0].id != 1 {
		t.Fatalf("idleStreams() = %v, want just stream 1", idle)
	}
}

// TestIdleStreamReset is an end-to-end regression test for the sweep
// goroutine wired up by New(): a stream that never carries traffic gets
// reset, while a sibling stream on the same session that keeps exchanging
// data survives.
func TestIdleStreamReset(t *testing.T) {
	old := streamIdleSweepInterval
	streamIdleSweepInterval = 20 * time.Millisecond
	t.Cleanup(func() { streamIdleSweepInterval = old })

	ctx := context.Background()
	b := memory.NewBoard()
	sa := b.NewSession("client")
	sb := b.NewSession("server")
	if _, err := sa.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Subscribe(ctx, "page"); err != nil {
		t.Fatal(err)
	}
	la := link.New(sa, codec.Base64Codec{}, link.Options{RecvWindow: 8})
	lb := link.New(sb, codec.Base64Codec{}, link.Options{RecvWindow: 8})
	client := New(la, Options{Client: true, StreamIdleTimeout: 60 * time.Millisecond})
	server := New(lb, Options{StreamIdleTimeout: 60 * time.Millisecond})
	t.Cleanup(func() { client.Close(); server.Close() })

	echoServer(t, server)

	idle, err := client.OpenStream("idle:80")
	if err != nil {
		t.Fatal(err)
	}
	active, err := client.OpenStream("active:80")
	if err != nil {
		t.Fatal(err)
	}

	// Keep "active" busy well past the idle timeout so it must survive.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = io.WriteString(active, "x")
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	buf := make([]byte, 1)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			close(stop)
			t.Fatal("idle stream was not reset within the deadline")
		}
		_, err := idle.Read(buf)
		if err == ErrStreamReset {
			break
		}
		if err != nil {
			close(stop)
			t.Fatalf("idle stream Read err = %v, want ErrStreamReset (eventually)", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)

	if _, err := active.Write([]byte("y")); err != nil {
		t.Fatalf("active stream should have survived the sweep, Write err = %v", err)
	}
}

// stubConn is a minimal Conn whose TargetBatchSize() is externally
// adjustable, for testing that writer() actually re-reads it every loop
// iteration rather than caching a value fixed once at New().
type stubConn struct {
	target atomic.Int64
	recv   chan []byte
}

func TestAdaptiveReceiveWindowGrowsOnlyForBusyStream(t *testing.T) {
	const (
		initial = 256 << 10
		limit   = 2 << 20
	)
	sess := &Session{
		conn:            newStubConn(1024),
		initialWindow:   initial,
		maxStreamWindow: limit,
	}
	stream := newStream(1, sess, false, initial)

	stream.recvMu.Lock()
	stream.recvConsumed = initial / 2
	stream.tuneAt = time.Now().Add(-100 * time.Millisecond)
	stream.tuneReceiveWindowLocked(time.Now())
	got := stream.recvWindow
	stream.recvMu.Unlock()
	if got != 2*initial {
		t.Fatalf("busy stream window = %d, want %d", got, 2*initial)
	}

	stream.recvMu.Lock()
	stream.recvConsumed += stream.recvWindow / 2
	stream.tuneAt = time.Now().Add(-time.Second)
	stream.tuneReceiveWindowLocked(time.Now())
	got = stream.recvWindow
	stream.recvMu.Unlock()
	if got != 2*initial {
		t.Fatalf("slow stream window = %d, want unchanged %d", got, 2*initial)
	}
}

func TestAdaptiveReceiveWindowStopsAtConfiguredMaximum(t *testing.T) {
	const (
		initial = 1 << 20
		limit   = 2 << 20
	)
	sess := &Session{
		conn:            newStubConn(1024),
		initialWindow:   initial,
		maxStreamWindow: limit,
	}
	stream := newStream(1, sess, false, initial)

	for range 4 {
		stream.recvMu.Lock()
		stream.recvConsumed += stream.recvWindow / 2
		stream.tuneAt = time.Now().Add(-100 * time.Millisecond)
		stream.tuneReceiveWindowLocked(time.Now())
		stream.recvMu.Unlock()
	}
	if stream.recvWindow != limit {
		t.Fatalf("window = %d, want configured maximum %d", stream.recvWindow, limit)
	}
}

func newStubConn(initial int) *stubConn {
	c := &stubConn{recv: make(chan []byte)}
	c.target.Store(int64(initial))
	return c
}

func (c *stubConn) Send(context.Context, []byte) error { return nil }
func (c *stubConn) Recv() <-chan []byte                { return c.recv }
func (c *stubConn) Close() error                       { return nil }
func (c *stubConn) TargetBatchSize() int               { return int(c.target.Load()) }
func (c *stubConn) RTT() time.Duration                 { return 0 }

type flushRecordingConn struct {
	mu    sync.Mutex
	order []string
	recv  chan []byte
}

func (c *flushRecordingConn) record(v string) {
	c.mu.Lock()
	c.order = append(c.order, v)
	c.mu.Unlock()
}

func (c *flushRecordingConn) Send(context.Context, []byte) error {
	c.record("send")
	return nil
}
func (c *flushRecordingConn) Flush(context.Context) error { c.record("flush"); return nil }
func (c *flushRecordingConn) Close() error                { c.record("close"); return nil }
func (c *flushRecordingConn) Recv() <-chan []byte         { return c.recv }
func (c *flushRecordingConn) TargetBatchSize() int        { return 1024 }
func (c *flushRecordingConn) RTT() time.Duration          { return 0 }

// TestCloseFlushesGoAwayBeforeConnClose guards the real-board race where
// link.Send accepted GOAWAY asynchronously and immediate Close canceled its
// Put before it reached the peer.
func TestCloseFlushesGoAwayBeforeConnClose(t *testing.T) {
	c := &flushRecordingConn{recv: make(chan []byte)}
	s := New(c, Options{Client: true})
	_ = s.Close()

	c.mu.Lock()
	defer c.mu.Unlock()
	if got := strings.Join(c.order, ","); got != "send,flush,close" {
		t.Fatalf("close order = %q, want send,flush,close", got)
	}
}

func TestPeerGoAwayPreservesTerminalReason(t *testing.T) {
	c := &flushRecordingConn{recv: make(chan []byte)}
	s := New(c, Options{Client: true})

	s.dispatch(frameOut{typ: proto.FrameGoAway})
	<-s.Done()
	if !errors.Is(s.Err(), ErrPeerGoAway) {
		t.Fatalf("session error = %v, want ErrPeerGoAway", s.Err())
	}
	_ = s.Close()
}

func TestPeerCannotOpenStreamInLocalIDSpace(t *testing.T) {
	c := newStubConn(1024)
	s := New(c, Options{}) // server owns even ids; client peer owns odd ids
	s.dispatch(frameOut{typ: proto.FrameSyn, stream: 2, payload: encodeSyn(1024, "example:80")})
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("wrong-parity SYN did not close the session")
	}
	if !errors.Is(s.Err(), ErrProtocolViolation) {
		t.Fatalf("error = %v, want ErrProtocolViolation", s.Err())
	}
}

func TestDatagramDeliveryDropsInsteadOfBlockingMux(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Session{ctx: ctx}
	d := newDatagram(1, s)
	for i := 0; i < datagramRecvQueue; i++ {
		d.recv <- DatagramPacket{Payload: []byte{byte(i)}}
	}
	done := make(chan struct{})
	go func() {
		d.deliver(DatagramPacket{Payload: []byte("drop")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full datagram queue blocked mux delivery")
	}
}

func eventuallyMux(cond func() bool) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("condition not met within deadline")
}

// TestWriterTracksConnTargetBatchSize checks writer() refreshes its coalesce
// target from conn.TargetBatchSize() on every loop iteration, so each batch
// picks up the sizer's latest value instead of one fixed at New().
func TestWriterTracksConnTargetBatchSize(t *testing.T) {
	stub := newStubConn(1000)
	s := New(stub, Options{Client: true})
	defer s.Close()

	if err := eventuallyMux(func() bool { return s.coalesceTarget.Load() == 1000 }); err != nil {
		t.Fatalf("initial target not picked up: %v", err)
	}

	stub.target.Store(2000)
	// pickBatch is blocked waiting on an empty queue; enqueue a control frame
	// to unblock it and drive the writer loop back around to setCoalesceTarget.
	if _, err := s.OpenStream("t:80"); err != nil {
		t.Fatal(err)
	}
	if err := eventuallyMux(func() bool { return s.coalesceTarget.Load() == 2000 }); err != nil {
		t.Fatalf("updated target not picked up: %v", err)
	}
}

// TestSetCoalesceTargetClampsToCeiling checks the optional operator-configured
// ceiling (Options.CoalesceTarget) caps whatever the adaptive sizer reports.
func TestSetCoalesceTargetClampsToCeiling(t *testing.T) {
	s := newTestSession(t, bootstrapCoalesceTarget)
	s.coalesceCeiling = 500

	s.setCoalesceTarget(10_000_000)
	if got := s.coalesceTarget.Load(); got != 500 {
		t.Fatalf("coalesceTarget = %d, want clamped to ceiling 500", got)
	}

	s.setCoalesceTarget(100)
	if got := s.coalesceTarget.Load(); got != 100 {
		t.Fatalf("coalesceTarget = %d, want 100 (below ceiling, unclamped)", got)
	}
}

func readAllWithTimeout(t *testing.T, r io.Reader, d time.Duration) string {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(r)
		ch <- result{data, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read: %v", res.err)
		}
		return string(res.data)
	case <-time.After(d):
		t.Fatal("read timed out")
		return ""
	}
}
