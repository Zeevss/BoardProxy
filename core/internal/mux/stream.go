package mux

import (
	"bytes"
	"errors"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/proto"
)

// ErrStreamReset is returned by Read/Write after the peer reset the stream.
var ErrStreamReset = errors.New("mux: stream reset")

// ErrWriteClosed is returned by Write after the stream's write side is closed.
var ErrWriteClosed = errors.New("mux: write closed")

// Stream is one multiplexed, bidirectional byte stream — a net.Conn-like pipe.
//
// Per-stream flow control (the receive window each side advertises) bounds how
// much unread data the peer may have in flight, so a slow reader on one stream
// cannot force the shared link to buffer without limit or block other streams:
//   - sendMax is the absolute offset the peer has granted us. Write blocks once
//     sendOffset reaches it, until a MAX_STREAM_DATA arrives.
//   - the receive side buffers delivered bytes (bounded by the window we granted)
//     and, as the application reads, advertises the freed space back with
//     absolute MAX_STREAM_DATA frames.
//
// V4 DATA frames carry absolute byte offsets and FIN carries final_offset, so
// frames may arrive out of order across bond lanes.
type Stream struct {
	id      uint32
	sess    *Session
	target  string
	inbound bool

	// recv side
	recvMu        sync.Mutex
	recvCond      *sync.Cond
	recvBuf       [][]byte
	recvLen       int
	readErr       error // io.EOF after FIN, ErrStreamReset after RESET, ErrClosed on shutdown
	pendingCredit int   // v2 only: bytes read but not yet advertised back
	recvNext      uint64
	recvConsumed  uint64
	recvLimit     uint64
	recvWindow    uint64
	tuneConsumed  uint64
	tuneAt        time.Time
	recvPending   map[uint64][]byte
	pendingBytes  int
	finOffset     *uint64

	// send side
	writeMu     sync.Mutex
	sendMu      sync.Mutex
	sendCond    *sync.Cond
	sendOffset  uint64
	sendMax     uint64
	writeClosed bool

	stateMu sync.Mutex
	finRecv bool
	finSent bool

	// lastActivity — UnixNano времени последнего реального трафика (Write или
	// deliver), для idleSweep. Read не считается активностью — это локальное
	// потребление, а не сетевой трафик.
	lastActivity atomic.Int64

	// Счётчики трафика для метрик: written — байт, отправленных приложением
	// (Write), received — байт, полученных от пира (deliver). Направление Tx/Rx
	// вызывающий трактует сам (клиент: written=upload, received=download).
	written   atomic.Uint64
	received  atomic.Uint64
	startedAt time.Time
}

func newStream(id uint32, sess *Session, inbound bool, sendWindow int) *Stream {
	s := &Stream{
		id: id, sess: sess, inbound: inbound,
		sendMax: uint64(sendWindow), recvLimit: uint64(sess.initialWindow),
		recvWindow: uint64(sess.initialWindow), tuneAt: time.Now(),
		recvPending: make(map[uint64][]byte), startedAt: time.Now(),
	}
	s.recvCond = sync.NewCond(&s.recvMu)
	s.sendCond = sync.NewCond(&s.sendMu)
	s.lastActivity.Store(time.Now().UnixNano())
	return s
}

// Stats возвращает снимок счётчиков стрима для метрик.
func (s *Stream) Stats() StreamStats {
	return StreamStats{
		ID:        s.id,
		Target:    s.target,
		Written:   s.written.Load(),
		Received:  s.received.Load(),
		StartedAt: s.startedAt,
	}
}

// Target is the destination address requested when the stream was opened.
func (s *Stream) Target() string { return s.target }

// ID returns the stream id.
func (s *Stream) ID() uint32 { return s.id }

// Read returns bytes from the peer in order, blocking until data is available.
// It returns io.EOF after the peer's FIN and ErrStreamReset after a RESET. As
// bytes are consumed it advertises the freed window space back to the peer.
func (s *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.recvMu.Lock()
	for len(s.recvBuf) == 0 && s.readErr == nil {
		s.recvCond.Wait()
	}
	if len(s.recvBuf) == 0 {
		err := s.readErr
		s.recvMu.Unlock()
		return 0, err
	}
	chunk := s.recvBuf[0]
	n := copy(p, chunk)
	if n < len(chunk) {
		s.recvBuf[0] = chunk[n:]
	} else {
		s.recvBuf = s.recvBuf[1:]
	}
	s.recvLen -= n
	var (
		credit     int
		newMaximum uint64
	)
	if s.sess.version >= 4 {
		s.recvConsumed += uint64(n)
		s.tuneReceiveWindowLocked(time.Now())
		desired := s.recvConsumed + s.recvWindow
		if desired < s.recvConsumed {
			desired = math.MaxUint64
		}
		threshold := max(uint64(1), s.recvWindow/4)
		if desired > s.recvLimit &&
			desired-s.recvLimit >= threshold {
			s.recvLimit = desired
			newMaximum = desired
		}
	} else {
		s.pendingCredit += n
		if s.pendingCredit >= s.sess.windowThreshold {
			credit = s.pendingCredit
			s.pendingCredit = 0
		}
	}
	s.recvMu.Unlock()

	if newMaximum > 0 {
		_ = s.sess.enqueueControl(frameOut{
			typ: proto.FrameWindowUpdate, stream: s.id,
			payload: encodeMaxStreamData(newMaximum),
		})
	} else if credit > 0 {
		// Priority channel: a stalled sender waiting on credit resumes promptly.
		_ = s.sess.enqueueControl(frameOut{
			typ:     proto.FrameWindowUpdate,
			stream:  s.id,
			payload: encodeWindowUpdate(uint32(credit)),
		})
	}
	return n, nil
}

// tuneReceiveWindowLocked doubles a busy stream's window when half of the
// current allowance is consumed within a few RTTs. Quiet streams retain the
// small initial allocation; growth is bounded by MaxStreamWindow.
func (s *Stream) tuneReceiveWindowLocked(now time.Time) {
	maxWindow := uint64(s.sess.maxStreamWindow)
	if s.recvWindow >= maxWindow {
		return
	}
	if s.recvConsumed-s.tuneConsumed < max(uint64(1), s.recvWindow/2) {
		return
	}
	elapsed := now.Sub(s.tuneAt)
	rtt := s.sess.conn.RTT()
	if rtt <= 0 {
		rtt = 100 * time.Millisecond
	}
	growWithin := 4 * rtt
	if growWithin < 250*time.Millisecond {
		growWithin = 250 * time.Millisecond
	}
	if elapsed <= growWithin {
		s.recvWindow = min(maxWindow, s.recvWindow*2)
	}
	s.tuneConsumed = s.recvConsumed
	s.tuneAt = now
}

// Write fragments p into DATA frames, respecting both the max frame payload and
// the available send window (blocking for flow control while the window is 0).
func (s *Stream) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.lastActivity.Store(time.Now().UnixNano())
	total := 0
	for len(p) > 0 {
		s.sendMu.Lock()
		for s.sendOffset >= s.sendMax && !s.writeClosed && s.sess.ctx.Err() == nil {
			s.sendCond.Wait()
		}
		if s.writeClosed {
			s.sendMu.Unlock()
			return total, ErrWriteClosed
		}
		if s.sess.ctx.Err() != nil {
			s.sendMu.Unlock()
			return total, ErrClosed
		}
		avail := s.sendMax - s.sendOffset
		offset := s.sendOffset
		s.sendMu.Unlock()

		n := min(len(p), s.sess.maxPayload)
		if uint64(n) > avail {
			n = int(avail)
		}
		chunk := make([]byte, n)
		copy(chunk, p[:n])
		wirePayload := chunk
		if s.sess.version >= 4 {
			wirePayload = encodeStreamData(offset, chunk)
		}
		if err := s.sess.enqueueData(frameOut{
			typ: proto.FrameData, stream: s.id, payload: wirePayload,
		}); err != nil {
			return total, err
		}
		s.sendMu.Lock()
		s.sendOffset += uint64(n)
		s.sendMu.Unlock()

		s.written.Add(uint64(n))
		p = p[n:]
		total += n
	}
	return total, nil
}

// addSendCredit grows the send window on a peer WINDOW_UPDATE.
func (s *Stream) addSendCredit(delta uint32) {
	s.sendMu.Lock()
	if math.MaxUint64-s.sendMax < uint64(delta) {
		s.sendMax = math.MaxUint64
	} else {
		s.sendMax += uint64(delta)
	}
	s.sendCond.Broadcast()
	s.sendMu.Unlock()
}

func (s *Stream) setSendMax(maximum uint64) {
	s.sendMu.Lock()
	if maximum > s.sendMax {
		s.sendMax = maximum
		s.sendCond.Broadcast()
	}
	s.sendMu.Unlock()
}

// CloseWrite half-closes the write side, sending a FIN so the peer sees EOF.
func (s *Stream) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.sendMu.Lock()
	if s.writeClosed {
		s.sendMu.Unlock()
		return nil
	}
	s.writeClosed = true
	finalOffset := s.sendOffset
	s.sendCond.Broadcast()
	s.sendMu.Unlock()

	s.stateMu.Lock()
	s.finSent = true
	done := s.finRecv
	s.stateMu.Unlock()

	// FIN is an orderly end-of-stream marker: it rides the data queue so it
	// follows this stream's data (unlike RESET, an out-of-band abort).
	var payload []byte
	if s.sess.version >= 4 {
		payload = encodeFinalOffset(finalOffset)
	}
	err := s.sess.enqueueData(frameOut{typ: proto.FrameFin, stream: s.id, payload: payload})
	if done {
		s.sess.removeStream(s.id)
	}
	return err
}

// Close closes the write side (FIN). The read side ends when the peer's FIN
// arrives; the stream is removed once both directions are closed.
func (s *Stream) Close() error { return s.CloseWrite() }

// Reset aborts the stream in both directions, sending a RESET.
func (s *Stream) Reset() error {
	s.setReadErr(ErrStreamReset)
	s.closeWriteSide()
	err := s.sess.enqueueControl(frameOut{typ: proto.FrameReset, stream: s.id})
	s.sess.removeStream(s.id)
	return err
}

// deliver appends an inbound DATA payload to the receive buffer. It never blocks
// the session reader — flow control bounds how much the peer can have in flight.
func (s *Stream) deliver(payload []byte) {
	s.lastActivity.Store(time.Now().UnixNano())
	s.recvMu.Lock()
	if s.readErr != nil {
		s.recvMu.Unlock()
		return // stream closed; drop
	}
	s.recvBuf = append(s.recvBuf, payload)
	s.recvLen += len(payload)
	s.received.Add(uint64(len(payload)))
	s.recvCond.Signal()
	s.recvMu.Unlock()
}

// deliverAt accepts one v4 DATA range. Exact duplicates are ignored; disjoint
// ranges are buffered until recvNext reaches them. Conflicting overlaps or data
// beyond the advertised receive limit/final offset reject the stream.
func (s *Stream) deliverAt(offset uint64, payload []byte) bool {
	if len(payload) == 0 || offset > math.MaxUint64-uint64(len(payload)) {
		return false
	}
	end := offset + uint64(len(payload))
	s.lastActivity.Store(time.Now().UnixNano())
	s.recvMu.Lock()
	if end <= s.recvNext {
		s.recvMu.Unlock()
		return true
	}
	if s.readErr != nil {
		s.recvMu.Unlock()
		return true
	}
	if end > s.recvLimit || (s.finOffset != nil && end > *s.finOffset) {
		s.recvMu.Unlock()
		return false
	}
	if offset < s.recvNext {
		payload = payload[s.recvNext-offset:]
		offset = s.recvNext
		end = offset + uint64(len(payload))
	}
	for pendingOffset, pending := range s.recvPending {
		pendingEnd := pendingOffset + uint64(len(pending))
		if offset == pendingOffset && end == pendingEnd && bytes.Equal(payload, pending) {
			s.recvMu.Unlock()
			return true
		}
		if offset < pendingEnd && pendingOffset < end {
			s.recvMu.Unlock()
			return false
		}
	}
	data := append([]byte(nil), payload...)
	if uint64(s.recvLen+s.pendingBytes+len(data)) > s.recvWindow {
		s.recvMu.Unlock()
		return false
	}
	if offset != s.recvNext && len(s.recvPending) >= maxStreamReorderRanges {
		s.recvMu.Unlock()
		return false
	}
	s.received.Add(uint64(len(data)))
	finReady := false
	if offset == s.recvNext {
		s.appendReadyLocked(data)
		for {
			pending, ok := s.recvPending[s.recvNext]
			if !ok {
				break
			}
			delete(s.recvPending, s.recvNext)
			s.pendingBytes -= len(pending)
			s.appendReadyLocked(pending)
		}
		finReady = s.finishReceiveLocked()
	} else {
		s.recvPending[offset] = data
		s.pendingBytes += len(data)
	}
	s.recvMu.Unlock()
	if finReady {
		s.markFinReceived()
	}
	return true
}

func (s *Stream) appendReadyLocked(payload []byte) {
	s.recvBuf = append(s.recvBuf, payload)
	s.recvLen += len(payload)
	s.recvNext += uint64(len(payload))
	s.recvCond.Signal()
}

func (s *Stream) finishReceiveLocked() bool {
	if s.finOffset == nil || s.recvNext != *s.finOffset || s.readErr != nil {
		return false
	}
	s.readErr = io.EOF
	s.recvCond.Broadcast()
	return true
}

func (s *Stream) onFinAt(finalOffset uint64) bool {
	s.recvMu.Lock()
	if s.finOffset != nil {
		ok := *s.finOffset == finalOffset
		s.recvMu.Unlock()
		return ok
	}
	if finalOffset < s.recvNext || finalOffset > s.recvLimit {
		s.recvMu.Unlock()
		return false
	}
	for offset, pending := range s.recvPending {
		if offset+uint64(len(pending)) > finalOffset {
			s.recvMu.Unlock()
			return false
		}
	}
	value := finalOffset
	s.finOffset = &value
	finReady := s.finishReceiveLocked()
	s.recvMu.Unlock()
	if finReady {
		s.markFinReceived()
	}
	return true
}

// onFin marks the peer's write side closed: the reader sees EOF after draining.
func (s *Stream) onFin() {
	s.setReadErr(io.EOF)
	s.markFinReceived()
}

func (s *Stream) markFinReceived() {
	s.stateMu.Lock()
	s.finRecv = true
	done := s.finSent
	s.stateMu.Unlock()
	if done {
		s.sess.removeStream(s.id)
	}
}

// onReset aborts the stream due to a peer RESET.
func (s *Stream) onReset() {
	s.setReadErr(ErrStreamReset)
	s.closeWriteSide()
	s.sess.removeStream(s.id)
}

// shutdown terminates the stream when the whole session closes.
func (s *Stream) shutdown() {
	s.setReadErr(ErrClosed)
	s.closeWriteSide()
}

// setReadErr records the terminal read error (first wins) and wakes readers.
func (s *Stream) setReadErr(err error) {
	s.recvMu.Lock()
	if s.readErr == nil {
		s.readErr = err
	}
	s.recvCond.Broadcast()
	s.recvMu.Unlock()
}

// closeWriteSide marks the write side closed and wakes blocked writers.
func (s *Stream) closeWriteSide() {
	s.sendMu.Lock()
	s.writeClosed = true
	s.sendCond.Broadcast()
	s.sendMu.Unlock()
}
