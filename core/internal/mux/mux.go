// Package mux is the L3 transport-mux layer: it multiplexes many bidirectional
// byte streams (one per proxied TCP connection) over a reliable frame pipe
// (link.Link or bond.Conn), with a priority system channel for lifecycle
// frames. V4 does not require the pipe to preserve cross-packet order: DATA
// offsets and FIN final offsets reassemble each stream independently.
//
// The mux depends only on the Conn interface (Send/Recv/Close), so it runs over
// the real board-backed link or any in-memory loopback in tests.
package mux

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/proto"
	"bproxy-core/internal/transportstats"
)

// ErrClosed is returned once the session is closed.
var ErrClosed = errors.New("mux: session closed")

// ErrPeerGoAway is the terminal session reason when the peer explicitly
// announced a graceful shutdown. Callers use it to distinguish an orderly
// server restart from an unexplained link failure.
var ErrPeerGoAway = errors.New("mux: peer graceful shutdown")

var ErrProtocolViolation = errors.New("mux: protocol violation")

const (
	defaultMaxPayload      = 2048
	defaultStreamWindow    = 1 << 20
	defaultMaxStreamWindow = 32 << 20
	controlQueueCap        = 64
	// dataQueueCap bounds the total number of queued data frames across all
	// streams (not per stream — a stream's own sendMax already bounds how
	// much it can have in flight). Sized well above what one coalesced batch
	// consumes at the bootstrap coalesce target, given frames are typically
	// ≤32KiB (io.Copy's buffer size).
	dataQueueCap = 256
	acceptQueue  = 64
	orphanLimit  = 4 << 20
	// maxStreamReorderRanges bounds map overhead even for a malicious peer
	// sending many one-byte sparse ranges inside an otherwise valid window.
	maxStreamReorderRanges = 4096

	// bootstrapCoalesceTarget seeds the writer's coalesce target before the
	// first call to conn.TargetBatchSize() reports a real adaptive value —
	// matches the byte budget the manual empirical measurement found (see
	// README), so day-0 behaviour doesn't regress while the adaptive sizer
	// (internal/link/sizer.go) starts learning the actual sweet spot for the
	// current network/backend.
	bootstrapCoalesceTarget = 3 * 1024 * 1024
)

// Conn is the reliable frame pipe the mux runs over (satisfied by *link.Link).
type Conn interface {
	Send(ctx context.Context, frame []byte) error
	Recv() <-chan []byte
	Close() error
	// TargetBatchSize returns the current adaptive target size (bytes) for a
	// coalesced data batch — see internal/link/sizer.go. The writer refreshes
	// its coalesce target from this on every loop iteration.
	TargetBatchSize() int
	// RTT returns the current reactive round-trip estimate to the peer, for
	// metrics. Zero before any sample.
	RTT() time.Duration
}

// connFlusher is an optional capability implemented by link.Link. Ordinary
// mock/in-memory Conn implementations need not provide it. It makes the final
// GOAWAY observable before Close cancels asynchronous board writes.
type connFlusher interface {
	Flush(context.Context) error
}

// StreamStats is a snapshot of one stream's traffic counters.
type StreamStats struct {
	ID        uint32
	Target    string
	Written   uint64 // bytes written by the local app
	Received  uint64 // bytes received from the peer
	StartedAt time.Time
}

// SessionStats is a snapshot of a session's aggregate traffic and per-stream
// detail, for metrics.
type SessionStats struct {
	Streams        int           // open streams
	Datagrams      int           // open UDP associations
	Written        uint64        // total bytes written (open + already-closed streams)
	Received       uint64        // total bytes received
	TransportAcked uint64        // link payload bytes confirmed by the peer
	BacklogFrames  int           // data frames currently queued for the writer
	BacklogBytes   int           // encoded bytes currently queued for the writer
	BlockedWriters int           // writers waiting because the data queue is full
	RTT            time.Duration // reactive RTT to the peer
	Lanes          []transportstats.Lane
	PerStream      []StreamStats // open streams only
}

// Options configures a Session.
type Options struct {
	// Version selects mux wire semantics. Zero means the current protocol.
	// Version 2 is retained only for legacy single-page clients.
	Version int
	// Client marks the session as the stream initiator, which selects the
	// odd/even stream-id space to avoid collisions.
	Client bool
	// MaxPayload is the largest DATA payload per frame; larger writes fragment.
	MaxPayload int
	// StreamWindow is the per-stream receive window in bytes advertised to the
	// peer (how much unread data it may have in flight per stream).
	StreamWindow int
	// MaxStreamWindow caps automatic receive-window growth.
	MaxStreamWindow int
	// CoalesceTarget, if set (>0), is a hard ceiling on the adaptive coalesce
	// target reported by conn.TargetBatchSize() — an optional manual override
	// for operators who want to cap batch size. 0 means fully adaptive, no
	// ceiling.
	CoalesceTarget int
	// StreamIdleTimeout resets a stream that has carried no traffic (Write or
	// an inbound deliver) for this long, even while other streams on the same
	// session stay active. 0 disables the sweep.
	StreamIdleTimeout time.Duration
}

// Session multiplexes streams over one Conn.
type Session struct {
	conn            Conn
	version         int
	client          bool
	maxPayload      int
	initialWindow   int
	maxStreamWindow int
	windowThreshold int

	mu             sync.Mutex
	streams        map[uint32]*Stream
	datagrams      map[uint32]*Datagram
	nextID         uint32
	accept         chan *Stream
	acceptDatagram chan *Datagram
	orphans        map[uint32][]frameOut
	orphanBytes    int

	// Байты стримов, которые уже закрылись, — чтобы суммарные счётчики не
	// уменьшались, когда стрим уходит из streams (см. removeStream, Stats).
	closedWritten    atomic.Uint64
	closedReceived   atomic.Uint64
	datagramWritten  atomic.Uint64
	datagramReceived atomic.Uint64

	streamIdleTimeout time.Duration

	// Очередь на отправку — см. пакетный докстринг в control.go. coalesceTarget
	// — atomic.Int64, не под qMu: writer() обновляет его из conn.TargetBatchSize()
	// вне qMu на каждой итерации цикла (запрашивать адаптивное значение у link,
	// держа qMu, значило бы вкладывать чужой мьютекс внутрь своего), а
	// drainDataLocked() читает его под qMu — атомик безопасен для чтения под
	// чужим локом без гонки. coalesceCeiling — необязательный ручной потолок
	// (Options.CoalesceTarget), неизменяемый после New(), синхронизация не нужна.
	qMu             sync.Mutex
	qCond           *sync.Cond
	control         []frameOut
	dataByStream    map[uint32][]frameOut
	dataOrder       []uint32
	dataLen         int
	dataBytes       int
	blockedWriters  atomic.Int64
	coalesceTarget  atomic.Int64
	coalesceCeiling int
	qClosed         bool

	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	goAwayOnce sync.Once
	closeErr   error
	wg         sync.WaitGroup
}

// setCoalesceTarget stores n as the effective coalesce target, clamped to the
// optional operator-configured ceiling (coalesceCeiling, 0 = no ceiling) and
// floored at 1 (drainDataLocked's "size < target" loop must always admit at
// least one frame).
func (s *Session) setCoalesceTarget(n int) {
	if s.coalesceCeiling > 0 && n > s.coalesceCeiling {
		n = s.coalesceCeiling
	}
	if n < 1 {
		n = 1
	}
	s.coalesceTarget.Store(int64(n))
}

// New starts a mux session over conn.
func New(conn Conn, opts Options) *Session {
	version := opts.Version
	if version == 0 {
		version = proto.Version
	}
	maxPayload := opts.MaxPayload
	if maxPayload <= 0 {
		maxPayload = defaultMaxPayload
	}
	window := opts.StreamWindow
	if window <= 0 {
		window = defaultStreamWindow
	}
	maxWindow := opts.MaxStreamWindow
	if maxWindow <= 0 {
		maxWindow = defaultMaxStreamWindow
	}
	if maxWindow < window {
		maxWindow = window
	}
	threshold := window / 2
	if threshold < 1 {
		threshold = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		conn:              conn,
		version:           version,
		client:            opts.Client,
		maxPayload:        maxPayload,
		initialWindow:     window,
		maxStreamWindow:   maxWindow,
		windowThreshold:   threshold,
		streams:           make(map[uint32]*Stream),
		datagrams:         make(map[uint32]*Datagram),
		accept:            make(chan *Stream, acceptQueue),
		acceptDatagram:    make(chan *Datagram, acceptQueue),
		orphans:           make(map[uint32][]frameOut),
		dataByStream:      make(map[uint32][]frameOut),
		coalesceCeiling:   opts.CoalesceTarget,
		streamIdleTimeout: opts.StreamIdleTimeout,
		ctx:               ctx,
		cancel:            cancel,
	}
	s.setCoalesceTarget(bootstrapCoalesceTarget)
	s.qCond = sync.NewCond(&s.qMu)
	context.AfterFunc(ctx, func() {
		s.qMu.Lock()
		s.qClosed = true
		s.qCond.Broadcast()
		s.qMu.Unlock()
	})
	// Client streams are odd, server streams even; id 0 is reserved.
	if opts.Client {
		s.nextID = 1
	} else {
		s.nextID = 2
	}
	s.wg.Add(2)
	go s.writer()
	go s.reader()
	if s.streamIdleTimeout > 0 {
		s.wg.Add(1)
		go s.idleSweep()
	}
	return s
}

// OpenStream opens a new stream to target and announces it with a SYN.
func (s *Session) OpenStream(target string) (*Stream, error) {
	s.mu.Lock()
	if s.streams == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	id := s.nextID
	s.nextID += 2
	// sendMax starts at our own initial window (the peer is assumed symmetric
	// until SYN says otherwise); SYN advertises our absolute initial limit.
	st := newStream(id, s, false, s.initialWindow)
	st.target = target
	s.streams[id] = st
	s.mu.Unlock()

	syn := frameOut{typ: proto.FrameSyn, stream: id, payload: encodeSyn(uint32(s.initialWindow), target)}
	if err := s.enqueueControl(syn); err != nil {
		s.removeStream(id)
		return nil, err
	}
	return st, nil
}

// OpenDatagram creates a message-oriented UDP association over this session.
func (s *Session) OpenDatagram() (*Datagram, error) {
	s.mu.Lock()
	if s.datagrams == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	id := s.nextID
	s.nextID += 2
	d := newDatagram(id, s)
	s.datagrams[id] = d
	s.mu.Unlock()
	if err := s.enqueueControl(frameOut{typ: proto.FrameDatagramOpen, stream: id}); err != nil {
		s.removeDatagram(id)
		return nil, err
	}
	return d, nil
}

// AcceptStream returns the next stream opened by the peer.
func (s *Session) AcceptStream(ctx context.Context) (*Stream, error) {
	select {
	case st := <-s.accept:
		return st, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrClosed
	}
}

// AcceptDatagram returns the next UDP association opened by the peer.
func (s *Session) AcceptDatagram(ctx context.Context) (*Datagram, error) {
	select {
	case d := <-s.acceptDatagram:
		return d, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrClosed
	}
}

// Done is closed when the session closes (peer/link gone or Close called). The
// hub uses it to release a client's page on disconnect.
func (s *Session) Done() <-chan struct{} { return s.ctx.Done() }

// Err returns the terminal reason after Done is closed, or nil while the
// session is still running. closeWithError stores the reason before canceling
// the context, so receiving from Done synchronizes this read.
func (s *Session) Err() error {
	select {
	case <-s.Done():
		return s.closeErr
	default:
		return nil
	}
}

// Close shuts the session and its streams down, first sending a GOAWAY so the
// peer (which has no connection-level EOF over the board) tears down promptly.
func (s *Session) Close() error {
	s.goAwayOnce.Do(s.sendGoAway)
	s.closeWithError(ErrClosed)
	s.wg.Wait()
	return s.closeErr
}

// sendGoAway makes a best-effort GOAWAY send before teardown.
func (s *Session) sendGoAway() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.conn.Send(ctx, encodeBatch([]frameOut{{typ: proto.FrameGoAway}})); err != nil {
		return
	}
	if f, ok := s.conn.(connFlusher); ok {
		_ = f.Flush(ctx)
	}
}

func (s *Session) closeWithError(err error) {
	s.closeOnce.Do(func() {
		s.closeErr = err
		s.cancel()
		_ = s.conn.Close()
		s.mu.Lock()
		streams := s.streams
		s.streams = nil
		datagrams := s.datagrams
		s.datagrams = nil
		s.mu.Unlock()
		for _, st := range streams {
			st.shutdown()
		}
		for _, d := range datagrams {
			d.shutdown()
		}
	})
}

func (s *Session) reader() {
	defer s.wg.Done()
	defer s.closeWithError(ErrClosed)
	recv := s.conn.Recv()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-recv:
			if !ok {
				return
			}
			for _, f := range decodeBatch(raw) {
				s.dispatch(f)
			}
		}
	}
}

func (s *Session) dispatch(f frameOut) {
	switch f.typ {
	case proto.FrameSyn:
		s.onSyn(f)
	case proto.FrameData:
		if st := s.stream(f.stream); st != nil {
			s.dispatchStreamFrame(st, f)
		} else if s.bufferOrphan(f) {
			return
		} else {
			// Data for an unknown stream: tell the peer to abort it.
			_ = s.enqueueControl(frameOut{typ: proto.FrameReset, stream: f.stream})
		}
	case proto.FrameFin:
		if st := s.stream(f.stream); st != nil {
			s.dispatchStreamFrame(st, f)
		} else {
			s.bufferOrphan(f)
		}
	case proto.FrameReset:
		if st := s.stream(f.stream); st != nil {
			st.onReset()
		} else {
			s.bufferOrphan(f)
		}
	case proto.FrameWindowUpdate:
		if st := s.stream(f.stream); st != nil {
			if s.version >= 4 {
				if maximum, ok := decodeMaxStreamData(f.payload); ok {
					st.setSendMax(maximum)
				}
			} else if credit, ok := decodeWindowUpdate(f.payload); ok {
				st.addSendCredit(credit)
			}
		}
	case proto.FrameGoAway:
		s.closeWithError(ErrPeerGoAway)
	case proto.FrameDatagramOpen:
		s.onDatagramOpen(f)
	case proto.FrameDatagram:
		if s.datagram(f.stream) == nil && s.bufferOrphan(f) {
			return
		}
		s.onDatagram(f)
	case proto.FrameDatagramClose:
		if d := s.datagram(f.stream); d != nil {
			s.removeDatagram(f.stream)
			d.shutdown()
		} else {
			s.bufferOrphan(f)
		}
	}
}

func (s *Session) dispatchStreamFrame(st *Stream, f frameOut) {
	switch f.typ {
	case proto.FrameData:
		if s.version >= 4 {
			offset, payload, ok := decodeStreamData(f.payload)
			if !ok || !st.deliverAt(offset, payload) {
				_ = st.Reset()
			}
		} else {
			st.deliver(f.payload)
		}
	case proto.FrameFin:
		if s.version >= 4 {
			finalOffset, ok := decodeFinalOffset(f.payload)
			if !ok || !st.onFinAt(finalOffset) {
				_ = st.Reset()
			}
		} else {
			st.onFin()
		}
	}
}

func (s *Session) peerOwnedID(id uint32) bool {
	if id == 0 {
		return false
	}
	if s.client {
		return id%2 == 0
	}
	return id%2 == 1
}

func (s *Session) bufferOrphan(f frameOut) bool {
	if !s.peerOwnedID(f.stream) {
		return false
	}
	size := encodedLen(f)
	s.mu.Lock()
	if s.streams == nil || s.orphanBytes+size > orphanLimit {
		s.mu.Unlock()
		s.closeWithError(ErrProtocolViolation)
		return false
	}
	s.orphans[f.stream] = append(s.orphans[f.stream], f)
	s.orphanBytes += size
	s.mu.Unlock()
	return true
}

func (s *Session) takeOrphansLocked(id uint32) []frameOut {
	frames := s.orphans[id]
	delete(s.orphans, id)
	for _, f := range frames {
		s.orphanBytes -= encodedLen(f)
	}
	return frames
}

func (s *Session) onSyn(f frameOut) {
	s.mu.Lock()
	if s.streams == nil {
		s.mu.Unlock()
		return
	}
	if _, exists := s.streams[f.stream]; exists || s.datagrams[f.stream] != nil {
		s.mu.Unlock()
		return // duplicate SYN, ignore
	}
	window, target, ok := decodeSyn(f.payload)
	if !ok {
		s.mu.Unlock()
		return
	}
	// The peer advertised how much it will buffer from us → our send window.
	st := newStream(f.stream, s, true, int(window))
	st.target = target
	s.streams[f.stream] = st
	pending := s.takeOrphansLocked(f.stream)
	s.mu.Unlock()

	for _, queued := range pending {
		if s.stream(f.stream) != st {
			break
		}
		switch queued.typ {
		case proto.FrameData, proto.FrameFin:
			s.dispatchStreamFrame(st, queued)
		case proto.FrameReset:
			st.onReset()
		}
	}
	if s.stream(f.stream) != st {
		return
	}

	select {
	case s.accept <- st:
	case <-s.ctx.Done():
	}
}

func (s *Session) onDatagramOpen(f frameOut) {
	s.mu.Lock()
	if s.datagrams == nil || s.streams[f.stream] != nil || s.datagrams[f.stream] != nil {
		s.mu.Unlock()
		return
	}
	d := newDatagram(f.stream, s)
	s.datagrams[f.stream] = d
	pending := s.takeOrphansLocked(f.stream)
	s.mu.Unlock()
	for _, queued := range pending {
		if s.datagram(f.stream) != d {
			break
		}
		switch queued.typ {
		case proto.FrameDatagram:
			s.onDatagram(queued)
		case proto.FrameDatagramClose:
			s.removeDatagram(f.stream)
			d.shutdown()
		}
	}
	if s.datagram(f.stream) != d {
		return
	}
	select {
	case s.acceptDatagram <- d:
	case <-s.ctx.Done():
	}
}

func (s *Session) onDatagram(f frameOut) {
	target, payload, ok := decodeDatagram(f.payload)
	if !ok {
		return
	}
	d := s.datagram(f.stream)
	if d == nil {
		_ = s.enqueueControl(frameOut{typ: proto.FrameDatagramClose, stream: f.stream})
		return
	}
	s.datagramReceived.Add(uint64(len(payload)))
	d.deliver(DatagramPacket{Target: target, Payload: append([]byte(nil), payload...)})
}

// StreamCount returns the number of open streams.
func (s *Session) StreamCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

func (s *Session) stream(id uint32) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

func (s *Session) removeStream(id uint32) {
	s.mu.Lock()
	if s.streams != nil {
		if st := s.streams[id]; st != nil {
			s.closedWritten.Add(st.written.Load())
			s.closedReceived.Add(st.received.Load())
		}
		delete(s.streams, id)
	}
	s.mu.Unlock()
}

func (s *Session) datagram(id uint32) *Datagram {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.datagrams[id]
}

func (s *Session) removeDatagram(id uint32) {
	s.mu.Lock()
	if s.datagrams != nil {
		delete(s.datagrams, id)
	}
	s.mu.Unlock()
}

// Stats returns a snapshot of the session's traffic and open streams, for
// metrics (see pkg/bproxy). Totals include bytes from already-closed streams so
// they never regress when a stream ends.
func (s *Session) Stats() SessionStats {
	s.mu.Lock()
	per := make([]StreamStats, 0, len(s.streams))
	datagrams := len(s.datagrams)
	var written, received uint64
	for _, st := range s.streams {
		ss := st.Stats()
		per = append(per, ss)
		written += ss.Written
		received += ss.Received
	}
	s.mu.Unlock()

	s.qMu.Lock()
	backlogFrames := s.dataLen
	backlogBytes := s.dataBytes
	s.qMu.Unlock()

	var transportAcked uint64
	if counter, ok := s.conn.(interface{ ConfirmedBytes() uint64 }); ok {
		transportAcked = counter.ConfirmedBytes()
	}
	var lanes []transportstats.Lane
	if provider, ok := s.conn.(transportstats.Provider); ok {
		lanes = provider.TransportStats()
	}
	return SessionStats{
		Streams:        len(per),
		Datagrams:      datagrams,
		Written:        written + s.closedWritten.Load() + s.datagramWritten.Load(),
		Received:       received + s.closedReceived.Load() + s.datagramReceived.Load(),
		TransportAcked: transportAcked,
		BacklogFrames:  backlogFrames,
		BacklogBytes:   backlogBytes,
		BlockedWriters: int(s.blockedWriters.Load()),
		RTT:            s.conn.RTT(),
		Lanes:          lanes,
		PerStream:      per,
	}
}
