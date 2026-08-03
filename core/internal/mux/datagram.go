package mux

import (
	"context"
	"errors"
	"sync"

	"bproxy-core/internal/proto"
)

var (
	ErrDatagramClosed   = errors.New("mux: datagram association closed")
	ErrDatagramTooLarge = errors.New("mux: UDP datagram exceeds 65507 bytes")
)

const datagramRecvQueue = 64

// DatagramPacket is one complete UDP message. Target is the destination on
// client->server traffic and the source address on server->client traffic.
type DatagramPacket struct {
	Target  string
	Payload []byte
}

// Datagram is a message-oriented association multiplexed alongside TCP
// streams. It intentionally does not implement net.PacketConn: addresses stay
// strings so DNS names can be resolved only at egress.
type Datagram struct {
	id      uint32
	session *Session
	recv    chan DatagramPacket
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
	once    sync.Once
}

func newDatagram(id uint32, session *Session) *Datagram {
	return &Datagram{
		id:      id,
		session: session,
		recv:    make(chan DatagramPacket, datagramRecvQueue),
		done:    make(chan struct{}),
	}
}

func (d *Datagram) ID() uint32 { return d.id }

// Send queues one complete UDP datagram without merging it with adjacent
// messages. Backpressure is bounded by the mux data queue.
func (d *Datagram) Send(target string, payload []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrDatagramClosed
	}
	encoded, ok := encodeDatagram(target, payload)
	if !ok {
		if len(payload) > maxDatagramPayload {
			return ErrDatagramTooLarge
		}
		return errors.New("mux: invalid datagram target")
	}
	err := d.session.enqueueData(frameOut{
		typ: proto.FrameDatagram, stream: d.id, payload: encoded,
	})
	if err == nil {
		d.session.datagramWritten.Add(uint64(len(payload)))
	}
	return err
}

// Receive waits for the next complete message or association shutdown.
func (d *Datagram) Receive(ctx context.Context) (DatagramPacket, error) {
	select {
	case p := <-d.recv:
		return p, nil
	case <-d.done:
		return DatagramPacket{}, ErrDatagramClosed
	case <-ctx.Done():
		return DatagramPacket{}, ctx.Err()
	}
}

func (d *Datagram) Close() error {
	d.once.Do(func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.closed = true
		// Close follows already queued datagrams on the same per-id data queue.
		_ = d.session.enqueueData(frameOut{typ: proto.FrameDatagramClose, stream: d.id})
		d.session.removeDatagram(d.id)
		close(d.done)
	})
	return nil
}

func (d *Datagram) deliver(p DatagramPacket) {
	select {
	case d.recv <- p:
	case <-d.done:
	case <-d.session.ctx.Done():
	default:
		// UDP is allowed to drop. Blocking the single mux reader behind one slow
		// association would otherwise stall every TCP stream and GOAWAY frame in
		// the session.
	}
}

func (d *Datagram) shutdown() {
	d.once.Do(func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.closed = true
		close(d.done)
	})
}
