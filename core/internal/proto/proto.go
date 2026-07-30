// Package proto holds wire-level constants shared across BoardProxy layers:
// the protocol version, mux frame types, and control-channel message kinds.
//
// The concrete on-the-wire encodings are defined by the layers that own them
// (mux/frame.go for stream frames, hub for the rendezvous messages); this
// package only pins the shared vocabulary and version so client and server
// agree. It is intentionally dependency-free.
package proto

// Version is the newest BoardProxy wire protocol version.
const Version = 4

// MinVersion is the oldest version accepted by the current server. V2 remains
// available for legacy one-page clients; v3 is the first ordered bonded wire,
// while v4 adds unordered bond delivery and per-stream offsets.
const MinVersion = 2

// ControlStreamID is the reserved stream id of the system channel that carries
// stream open/close/reset frames at priority over data streams.
const ControlStreamID uint32 = 0

// FrameType identifies the kind of a mux frame. Values are fixed on the wire.
type FrameType uint8

const (
	// FrameData carries stream payload bytes.
	FrameData FrameType = iota
	// FrameSyn opens a new stream (target address travels in its payload).
	FrameSyn
	// FrameFin half-closes a stream: no more data in this direction.
	FrameFin
	// FrameReset aborts a stream immediately.
	FrameReset
	// FrameWindowUpdate is MAX_STREAM_DATA in v3: its payload is an absolute
	// maximum byte offset, so duplicate/reordered updates are idempotent. A v2
	// peer interprets the same frame type as additive credit.
	FrameWindowUpdate
	// FrameGoAway signals an orderly session shutdown so the peer can tear down
	// promptly (the board has no connection-level EOF). Stream id is unused.
	FrameGoAway
	// FrameDatagramOpen creates a UDP association. The stream id identifies it.
	FrameDatagramOpen
	// FrameDatagram carries one complete UDP datagram and its destination/source.
	FrameDatagram
	// FrameDatagramClose releases the UDP association and its egress socket.
	FrameDatagramClose
)

func (t FrameType) String() string {
	switch t {
	case FrameData:
		return "data"
	case FrameSyn:
		return "syn"
	case FrameFin:
		return "fin"
	case FrameReset:
		return "reset"
	case FrameWindowUpdate:
		return "window-update"
	case FrameGoAway:
		return "goaway"
	case FrameDatagramOpen:
		return "datagram-open"
	case FrameDatagram:
		return "datagram"
	case FrameDatagramClose:
		return "datagram-close"
	default:
		return "unknown"
	}
}
