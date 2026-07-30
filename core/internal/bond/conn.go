package bond

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/transportstats"
)

const (
	packetMarker           = byte(0xb3)
	packetHeaderLen        = 9
	defaultMaxUnackedBytes = 64 << 20
	defaultMaxReorderBytes = 64 << 20
	defaultTargetBatchSize = 3 << 20
)

var (
	ErrClosed          = errors.New("bond: closed")
	ErrDuplicateLane   = errors.New("bond: duplicate lane")
	ErrMalformedPacket = errors.New("bond: malformed packet")
	ErrReorderOverflow = errors.New("bond: reorder buffer overflow")
)

// Lane is one reliable ordered page transport. link.Link satisfies it.
type Lane interface {
	SendTracked(context.Context, []byte) (<-chan struct{}, error)
	Recv() <-chan []byte
	Done() <-chan struct{}
	Close() error
	TargetBatchSize() int
	RTT() time.Duration
}

type Options struct {
	MaxUnackedBytes int
	// MaxReorderBytes is retained as the stage-2 option name. In unordered v3
	// it bounds sparse PacketID dedup metadata above a missing contiguous id;
	// payload bytes themselves are delivered immediately and are not buffered.
	MaxReorderBytes int
	// Ordered preserves stage-2/v3 global PacketID delivery order for already
	// deployed clients. New v4 sessions leave it false and reassemble in mux.
	Ordered bool
}

type replayRecord struct {
	packet      []byte
	payloadSize int
}

// Conn presents several physical lanes as one reliable packet pipe. PacketID
// provides replay deduplication but does not impose delivery order: mux v3
// reassembles each TCP stream by its own byte offsets, avoiding cross-stream
// and cross-lane head-of-line blocking.
type Conn struct {
	ctx    context.Context
	cancel context.CancelFunc

	laneMu       sync.Mutex
	lanes        map[LaneID]Lane
	laneOrder    []LaneID
	laneInflight map[LaneID]int
	draining     map[LaneID]bool
	laneChanged  chan struct{}
	closing      bool

	replayMu     sync.Mutex
	sendSerial   sync.Mutex
	sendNext     uint64
	replay       map[uint64]replayRecord
	replayBytes  int
	maxUnacked   int
	spaceChanged chan struct{}
	confirmed    atomic.Uint64

	recvMu       sync.Mutex
	ordered      bool
	recvNext     uint64
	seen         map[uint64]int
	seenBytes    int
	pending      map[uint64][]byte
	pendingBytes int
	maxReorder   int
	recv         chan []byte

	errMu sync.Mutex
	err   error

	wg   sync.WaitGroup
	done chan struct{}
}

func New(options Options) *Conn {
	maxUnacked := options.MaxUnackedBytes
	if maxUnacked <= 0 {
		maxUnacked = defaultMaxUnackedBytes
	}
	maxReorder := options.MaxReorderBytes
	if maxReorder <= 0 {
		maxReorder = defaultMaxReorderBytes
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		ctx:          ctx,
		cancel:       cancel,
		lanes:        make(map[LaneID]Lane),
		laneInflight: make(map[LaneID]int),
		draining:     make(map[LaneID]bool),
		laneChanged:  make(chan struct{}),
		replay:       make(map[uint64]replayRecord),
		maxUnacked:   maxUnacked,
		spaceChanged: make(chan struct{}),
		seen:         make(map[uint64]int),
		pending:      make(map[uint64][]byte),
		ordered:      options.Ordered,
		maxReorder:   maxReorder,
		recv:         make(chan []byte, 64),
		done:         make(chan struct{}),
	}
	go c.shutdown()
	return c
}

func (c *Conn) AddLane(id LaneID, lane Lane) error {
	if id == 0 || lane == nil {
		return fmt.Errorf("bond: invalid lane %d", id)
	}
	c.laneMu.Lock()
	if c.closing {
		c.laneMu.Unlock()
		return ErrClosed
	}
	if c.lanes[id] != nil {
		c.laneMu.Unlock()
		return ErrDuplicateLane
	}
	c.lanes[id] = lane
	c.laneOrder = append(c.laneOrder, id)
	c.signalLaneChangedLocked()
	// Add while laneMu is held: shutdown sets closing under the same mutex
	// before calling Wait, so Add cannot race a zero-count Wait.
	c.wg.Add(2)
	c.laneMu.Unlock()

	go c.readLane(id, lane)
	go c.watchLane(id, lane)
	return nil
}

func (c *Conn) RemoveLane(id LaneID) {
	c.laneMu.Lock()
	lane := c.lanes[id]
	c.laneMu.Unlock()
	if lane != nil {
		_ = lane.Close()
		c.detachLane(id, lane)
	}
}

// DrainLane stops assigning new packets to a lane, waits for its accepted
// sends to be acknowledged (or for ctx to expire), and then closes it.
func (c *Conn) DrainLane(ctx context.Context, id LaneID) error {
	c.laneMu.Lock()
	if c.lanes[id] == nil {
		c.laneMu.Unlock()
		return nil
	}
	if len(c.lanes) <= 1 {
		c.laneMu.Unlock()
		return errors.New("bond: cannot drain last lane")
	}
	c.draining[id] = true
	c.signalLaneChangedLocked()
	for c.lanes[id] != nil && c.laneInflight[id] > 0 {
		changed := c.laneChanged
		c.laneMu.Unlock()
		select {
		case <-ctx.Done():
			c.RemoveLane(id)
			return ctx.Err()
		case <-c.ctx.Done():
			return ErrClosed
		case <-changed:
		}
		c.laneMu.Lock()
	}
	lane := c.lanes[id]
	c.laneMu.Unlock()
	if lane != nil {
		if closer, ok := lane.(interface {
			CloseGracefully(context.Context) error
		}); ok {
			_ = closer.CloseGracefully(ctx)
		} else {
			_ = lane.Close()
		}
		c.detachLane(id, lane)
	}
	return nil
}

func (c *Conn) LaneCount() int {
	c.laneMu.Lock()
	defer c.laneMu.Unlock()
	return len(c.lanes)
}

func (c *Conn) LaneIDs() []LaneID {
	c.laneMu.Lock()
	defer c.laneMu.Unlock()
	ids := make([]LaneID, 0, len(c.lanes))
	for _, id := range c.laneOrder {
		if c.lanes[id] != nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (c *Conn) TransportStats() []transportstats.Lane {
	c.laneMu.Lock()
	defer c.laneMu.Unlock()
	stats := make([]transportstats.Lane, 0, len(c.laneOrder))
	for _, id := range c.laneOrder {
		lane := c.lanes[id]
		if lane == nil {
			continue
		}
		stat := transportstats.Lane{
			ID:            uint32(id),
			Inflight:      c.laneInflight[id],
			TargetPayload: lane.TargetBatchSize(),
			RTT:           lane.RTT(),
			Draining:      c.draining[id],
		}
		if provider, ok := lane.(transportstats.Provider); ok {
			if laneStats := provider.TransportStats(); len(laneStats) > 0 {
				stat = laneStats[0]
				stat.ID = uint32(id)
				stat.Draining = c.draining[id]
			}
		}
		stats = append(stats, stat)
	}
	return stats
}

func (c *Conn) Send(ctx context.Context, payload []byte) error {
	// PacketIDs must not develop holes if ctx is cancelled while waiting for a
	// lane. Serialize assignment and publish a sequence only after a lane has
	// accepted the tracked send.
	c.sendSerial.Lock()
	defer c.sendSerial.Unlock()

	packetSize := packetHeaderLen + len(payload)
	if packetSize > c.maxUnacked {
		return fmt.Errorf("bond: packet size %d exceeds replay limit %d", packetSize, c.maxUnacked)
	}

	for {
		c.replayMu.Lock()
		if c.replayBytes+packetSize <= c.maxUnacked {
			c.replayMu.Unlock()
			break
		}
		changed := c.spaceChanged
		c.replayMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctx.Done():
			return ErrClosed
		case <-changed:
		}
	}

	seq := c.sendNext
	packet := encodePacket(seq, payload)
	var avoid LaneID
	for {
		id, lane, err := c.chooseLane(ctx, avoid)
		if err != nil {
			return err
		}
		receipt, err := lane.SendTracked(ctx, packet)
		if err != nil {
			c.detachLane(id, lane)
			_ = lane.Close()
			avoid = id
			continue
		}
		c.trackLaneSend(id, lane)

		c.replayMu.Lock()
		c.sendNext++
		c.replay[seq] = replayRecord{
			packet:      packet,
			payloadSize: len(payload),
		}
		c.replayBytes += len(packet)
		c.replayMu.Unlock()
		go c.awaitReceipt(seq, id, lane, receipt)
		return nil
	}
}

func (c *Conn) dispatch(ctx context.Context, seq uint64, avoid LaneID) error {
	for {
		record, ok := c.replayRecord(seq)
		if !ok {
			return nil // another attempt already confirmed it
		}
		id, lane, err := c.chooseLane(ctx, avoid)
		if err != nil {
			return err
		}
		receipt, err := lane.SendTracked(ctx, record.packet)
		if err != nil {
			c.detachLane(id, lane)
			_ = lane.Close()
			avoid = id
			continue
		}
		c.trackLaneSend(id, lane)
		go c.awaitReceipt(seq, id, lane, receipt)
		return nil
	}
}

func (c *Conn) awaitReceipt(seq uint64, laneID LaneID, lane Lane, receipt <-chan struct{}) {
	defer c.completeLaneSend(laneID, lane)
	select {
	case <-receipt:
		c.confirm(seq)
	case <-lane.Done():
		// Retry in its own goroutine so lane teardown never blocks waiting for a
		// replacement lane. Duplicate delivery is safe because PacketID is stable.
		go func() {
			_ = c.dispatch(c.ctx, seq, laneID)
		}()
	case <-c.ctx.Done():
	}
}

func (c *Conn) chooseLane(ctx context.Context, avoid LaneID) (LaneID, Lane, error) {
	for {
		c.laneMu.Lock()
		var (
			bestID    LaneID
			bestLane  Lane
			bestScore time.Duration
		)
		for _, id := range c.laneOrder {
			if id == avoid || c.draining[id] {
				continue
			}
			lane := c.lanes[id]
			if lane == nil {
				continue
			}
			rtt := lane.RTT()
			if rtt <= 0 {
				rtt = 100 * time.Millisecond
			}
			score := rtt * time.Duration(c.laneInflight[id]+1)
			if bestLane == nil || score < bestScore ||
				(score == bestScore && id < bestID) {
				bestID, bestLane, bestScore = id, lane, score
			}
		}
		if bestLane != nil {
			c.laneMu.Unlock()
			return bestID, bestLane, nil
		}
		changed := c.laneChanged
		c.laneMu.Unlock()
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-c.ctx.Done():
			return 0, nil, ErrClosed
		case <-changed:
		}
	}
}

func (c *Conn) trackLaneSend(id LaneID, lane Lane) {
	c.laneMu.Lock()
	if c.lanes[id] == lane {
		c.laneInflight[id]++
		c.signalLaneChangedLocked()
	}
	c.laneMu.Unlock()
}

func (c *Conn) completeLaneSend(id LaneID, lane Lane) {
	c.laneMu.Lock()
	if c.lanes[id] == lane && c.laneInflight[id] > 0 {
		c.laneInflight[id]--
		c.signalLaneChangedLocked()
	}
	c.laneMu.Unlock()
}

func (c *Conn) readLane(id LaneID, lane Lane) {
	defer c.wg.Done()
	defer c.detachLane(id, lane)
	for {
		select {
		case <-c.ctx.Done():
			return
		case raw, ok := <-lane.Recv():
			if !ok {
				return
			}
			seq, payload, ok := decodePacket(raw)
			if !ok {
				c.fail(ErrMalformedPacket)
				return
			}
			if !c.accept(seq, payload) {
				return
			}
		}
	}
}

func (c *Conn) watchLane(id LaneID, lane Lane) {
	defer c.wg.Done()
	select {
	case <-lane.Done():
		c.detachLane(id, lane)
	case <-c.ctx.Done():
	}
}

// accept deduplicates PacketIDs and delivers new payloads immediately. recvNext
// is the contiguous seen prefix; ids above a gap stay in a bounded sparse set
// until the replayed missing packet arrives.
func (c *Conn) accept(seq uint64, payload []byte) bool {
	if c.ordered {
		return c.acceptOrdered(seq, payload)
	}
	c.recvMu.Lock()
	defer c.recvMu.Unlock()
	if seq < c.recvNext {
		return true
	}
	if _, duplicate := c.seen[seq]; duplicate {
		return true
	}
	const trackedSize = 16 // approximate sparse-map budget per PacketID
	if seq > c.recvNext && c.seenBytes+trackedSize > c.maxReorder {
		c.fail(ErrReorderOverflow)
		return false
	}
	if seq == c.recvNext {
		c.recvNext++
		for {
			size, ok := c.seen[c.recvNext]
			if !ok {
				break
			}
			delete(c.seen, c.recvNext)
			c.seenBytes -= size
			c.recvNext++
		}
	} else {
		c.seen[seq] = trackedSize
		c.seenBytes += trackedSize
	}
	deliver := append([]byte(nil), payload...)
	select {
	case c.recv <- deliver:
		return true
	case <-c.ctx.Done():
		return false
	}
}

func (c *Conn) acceptOrdered(seq uint64, payload []byte) bool {
	c.recvMu.Lock()
	defer c.recvMu.Unlock()
	if seq < c.recvNext {
		return true
	}
	if _, duplicate := c.pending[seq]; duplicate {
		return true
	}
	if seq > c.recvNext && c.pendingBytes+len(payload) > c.maxReorder {
		c.fail(ErrReorderOverflow)
		return false
	}
	c.pending[seq] = append([]byte(nil), payload...)
	c.pendingBytes += len(payload)
	for {
		ready, ok := c.pending[c.recvNext]
		if !ok {
			return true
		}
		delete(c.pending, c.recvNext)
		c.pendingBytes -= len(ready)
		c.recvNext++
		select {
		case c.recv <- ready:
		case <-c.ctx.Done():
			return false
		}
	}
}

func (c *Conn) Recv() <-chan []byte { return c.recv }

func (c *Conn) Flush(ctx context.Context) error {
	for {
		c.replayMu.Lock()
		if len(c.replay) == 0 {
			c.replayMu.Unlock()
			return nil
		}
		changed := c.spaceChanged
		c.replayMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctx.Done():
			return ErrClosed
		case <-changed:
		}
	}
}

func (c *Conn) Close() error {
	c.cancel()
	<-c.done
	return c.Err()
}

func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

func (c *Conn) ConfirmedBytes() uint64 { return c.confirmed.Load() }

func (c *Conn) TargetBatchSize() int {
	c.laneMu.Lock()
	defer c.laneMu.Unlock()
	target := 0
	for _, lane := range c.lanes {
		n := lane.TargetBatchSize()
		if n > 0 && (target == 0 || n < target) {
			target = n
		}
	}
	if target == 0 {
		return defaultTargetBatchSize
	}
	return target
}

func (c *Conn) RTT() time.Duration {
	c.laneMu.Lock()
	defer c.laneMu.Unlock()
	var rtt time.Duration
	for _, lane := range c.lanes {
		if sample := lane.RTT(); sample > rtt {
			rtt = sample
		}
	}
	return rtt
}

func (c *Conn) confirm(seq uint64) {
	c.replayMu.Lock()
	record, ok := c.replay[seq]
	if ok {
		delete(c.replay, seq)
		c.replayBytes -= len(record.packet)
		c.confirmed.Add(uint64(record.payloadSize))
		c.signalSpaceChangedLocked()
	}
	c.replayMu.Unlock()
}

func (c *Conn) replayRecord(seq uint64) (replayRecord, bool) {
	c.replayMu.Lock()
	defer c.replayMu.Unlock()
	record, ok := c.replay[seq]
	return record, ok
}

func (c *Conn) detachLane(id LaneID, lane Lane) {
	c.laneMu.Lock()
	if c.lanes[id] != lane {
		c.laneMu.Unlock()
		return
	}
	delete(c.lanes, id)
	delete(c.laneInflight, id)
	delete(c.draining, id)
	for i, existing := range c.laneOrder {
		if existing == id {
			c.laneOrder = append(c.laneOrder[:i], c.laneOrder[i+1:]...)
			break
		}
	}
	c.signalLaneChangedLocked()
	c.laneMu.Unlock()
}

func (c *Conn) fail(err error) {
	c.errMu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.errMu.Unlock()
	c.cancel()
}

func (c *Conn) shutdown() {
	<-c.ctx.Done()
	c.laneMu.Lock()
	c.closing = true
	lanes := make([]Lane, 0, len(c.lanes))
	for _, lane := range c.lanes {
		lanes = append(lanes, lane)
	}
	c.signalLaneChangedLocked()
	c.laneMu.Unlock()

	c.replayMu.Lock()
	c.signalSpaceChangedLocked()
	c.replayMu.Unlock()

	for _, lane := range lanes {
		_ = lane.Close()
	}
	c.wg.Wait()
	close(c.recv)
	close(c.done)
}

func (c *Conn) signalLaneChangedLocked() {
	close(c.laneChanged)
	c.laneChanged = make(chan struct{})
}

func (c *Conn) signalSpaceChangedLocked() {
	close(c.spaceChanged)
	c.spaceChanged = make(chan struct{})
}

func encodePacket(seq uint64, payload []byte) []byte {
	b := make([]byte, packetHeaderLen+len(payload))
	b[0] = packetMarker
	binary.BigEndian.PutUint64(b[1:packetHeaderLen], seq)
	copy(b[packetHeaderLen:], payload)
	return b
}

func decodePacket(b []byte) (uint64, []byte, bool) {
	if len(b) < packetHeaderLen || b[0] != packetMarker {
		return 0, nil, false
	}
	return binary.BigEndian.Uint64(b[1:packetHeaderLen]), b[packetHeaderLen:], true
}
