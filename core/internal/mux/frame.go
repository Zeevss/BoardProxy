package mux

import (
	"encoding/binary"
	"math"

	"bproxy-core/internal/proto"
)

const maxDatagramPayload = 65507

// A mux frame within a batch is a fixed header followed by the payload:
//
//	byte  0     : frame type (proto.FrameType)
//	bytes 1..4  : stream id (uint32, big endian)
//	bytes 5..   : payload
//
// V4 payload meaning by type: SYN = receive window + target; DATA = byte offset
// + stream bytes; FIN = final byte offset; MAX_STREAM_DATA = absolute limit.
const headerLen = 5

// batchLenLen is the size of the length prefix in front of each frame inside
// a batch envelope (see encodeBatch).
const batchLenLen = 4

// frameOut is a frame queued for the writer, tagged with its priority class.
type frameOut struct {
	typ     proto.FrameType
	stream  uint32
	payload []byte
}

func encodeFrame(f frameOut) []byte {
	buf := make([]byte, headerLen+len(f.payload))
	buf[0] = byte(f.typ)
	binary.BigEndian.PutUint32(buf[1:headerLen], f.stream)
	copy(buf[headerLen:], f.payload)
	return buf
}

func decodeFrame(b []byte) (frameOut, bool) {
	if len(b) < headerLen {
		return frameOut{}, false
	}
	return frameOut{
		typ:     proto.FrameType(b[0]),
		stream:  binary.BigEndian.Uint32(b[1:headerLen]),
		payload: b[headerLen:],
	}, true
}

// A SYN payload carries the opener's initial receive window (the credit the
// opener grants the peer) followed by the target address:
//
//	[window:4][target...]
func encodeSyn(window uint32, target string) []byte {
	b := make([]byte, 4+len(target))
	binary.BigEndian.PutUint32(b[:4], window)
	copy(b[4:], target)
	return b
}

func decodeSyn(payload []byte) (window uint32, target string, ok bool) {
	if len(payload) < 4 {
		return 0, "", false
	}
	return binary.BigEndian.Uint32(payload[:4]), string(payload[4:]), true
}

// A v4 DATA payload carries the absolute byte offset of its first byte.
func encodeStreamData(offset uint64, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint64(b[:8], offset)
	copy(b[8:], payload)
	return b
}

func decodeStreamData(payload []byte) (offset uint64, data []byte, ok bool) {
	if len(payload) <= 8 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint64(payload[:8]), payload[8:], true
}

func encodeFinalOffset(offset uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, offset)
	return b
}

func decodeFinalOffset(payload []byte) (uint64, bool) {
	if len(payload) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(payload), true
}

func encodeMaxStreamData(maximum uint64) []byte {
	return encodeFinalOffset(maximum)
}

func decodeMaxStreamData(payload []byte) (uint64, bool) {
	return decodeFinalOffset(payload)
}

// A WINDOW_UPDATE payload is a uint32 credit (bytes) added to the peer's send
// window for the stream.
func encodeWindowUpdate(credit uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, credit)
	return b
}

func decodeWindowUpdate(payload []byte) (uint32, bool) {
	if len(payload) < 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(payload[:4]), true
}

// A DATAGRAM payload preserves the UDP message boundary and carries the
// destination (client->server) or source (server->client):
//
//	[address length:2][address bytes][UDP payload]
func encodeDatagram(target string, payload []byte) ([]byte, bool) {
	if target == "" || len(target) > math.MaxUint16 || len(payload) > maxDatagramPayload {
		return nil, false
	}
	b := make([]byte, 2+len(target)+len(payload))
	binary.BigEndian.PutUint16(b[:2], uint16(len(target)))
	copy(b[2:], target)
	copy(b[2+len(target):], payload)
	return b, true
}

func decodeDatagram(payload []byte) (target string, data []byte, ok bool) {
	if len(payload) < 2 {
		return "", nil, false
	}
	n := int(binary.BigEndian.Uint16(payload[:2]))
	if n == 0 || len(payload) < 2+n || len(payload)-(2+n) > maxDatagramPayload {
		return "", nil, false
	}
	return string(payload[2 : 2+n]), payload[2+n:], true
}

// encodedLen is how many bytes f occupies inside a batch envelope: its length
// prefix plus its own header and payload.
func encodedLen(f frameOut) int {
	return batchLenLen + headerLen + len(f.payload)
}

// encodeBatch packs frames into a single link payload as a sequence of
// [len:4][encodeFrame(f)] entries. Every conn.Send call uses this envelope —
// even a single frame — so the receiver never needs to distinguish a batch
// from a lone frame.
func encodeBatch(frames []frameOut) []byte {
	total := 0
	for _, f := range frames {
		total += encodedLen(f)
	}
	buf := make([]byte, 0, total)
	for _, f := range frames {
		enc := encodeFrame(f)
		var lenBuf [batchLenLen]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(enc)))
		buf = append(buf, lenBuf[:]...)
		buf = append(buf, enc...)
	}
	return buf
}

// decodeBatch unpacks a batch envelope back into its frames, skipping any
// entry whose length prefix or frame header is malformed.
func decodeBatch(b []byte) []frameOut {
	var frames []frameOut
	for len(b) >= batchLenLen {
		n := binary.BigEndian.Uint32(b[:batchLenLen])
		b = b[batchLenLen:]
		if uint64(n) > uint64(len(b)) {
			return frames
		}
		if f, ok := decodeFrame(b[:n]); ok {
			frames = append(frames, f)
		}
		b = b[n:]
	}
	return frames
}
