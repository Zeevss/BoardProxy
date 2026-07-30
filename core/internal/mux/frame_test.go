package mux

import (
	"reflect"
	"testing"

	"bproxy-core/internal/proto"
)

func TestEncodeDecodeBatchRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		frames []frameOut
	}{
		{"empty", nil},
		{"single small", []frameOut{
			{typ: proto.FrameData, stream: 1, payload: []byte("hi")},
		}},
		{"single empty payload", []frameOut{
			{typ: proto.FrameGoAway},
		}},
		{"multiple mixed sizes", []frameOut{
			{typ: proto.FrameSyn, stream: 1, payload: []byte("example.com:443")},
			{typ: proto.FrameData, stream: 1, payload: []byte("x")},
			{typ: proto.FrameData, stream: 2, payload: make([]byte, 10000)},
			{typ: proto.FrameFin, stream: 1},
			{typ: proto.FrameReset, stream: 2},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeBatch(tc.frames)
			got := decodeBatch(encoded)
			if len(got) != len(tc.frames) {
				t.Fatalf("decoded %d frames, want %d", len(got), len(tc.frames))
			}
			for i := range tc.frames {
				want := tc.frames[i]
				if got[i].typ != want.typ || got[i].stream != want.stream || !reflect.DeepEqual(got[i].payload, normalizePayload(want.payload)) {
					t.Fatalf("frame %d = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

// normalizePayload mirrors decodeFrame's behaviour for an empty payload: it
// slices "the rest" of the buffer, which is a non-nil empty slice rather than
// a nil one.
func normalizePayload(p []byte) []byte {
	if len(p) == 0 {
		return []byte{}
	}
	return p
}

func TestDecodeBatchTruncatedLengthPrefixStopsCleanly(t *testing.T) {
	// A dangling 2-byte tail (less than batchLenLen) after one valid frame
	// must be silently dropped, not panic or desync.
	full := encodeBatch([]frameOut{{typ: proto.FrameData, stream: 1, payload: []byte("ok")}})
	truncated := append(full, 0x00, 0x01)
	got := decodeBatch(truncated)
	if len(got) != 1 || got[0].stream != 1 {
		t.Fatalf("got %+v, want the one well-formed leading frame", got)
	}
}

func TestDecodeBatchLengthPrefixExceedsRemainingBytesStopsCleanly(t *testing.T) {
	full := encodeBatch([]frameOut{{typ: proto.FrameData, stream: 1, payload: []byte("ok")}})
	// Corrupt the length prefix of a fabricated second entry to claim more
	// bytes than actually follow.
	bogus := append(append([]byte{}, full...), 0x7f, 0xff, 0xff, 0xff)
	got := decodeBatch(bogus)
	if len(got) != 1 || got[0].stream != 1 {
		t.Fatalf("got %+v, want only the one well-formed leading frame", got)
	}
}

func TestEncodedLenMatchesEncodeBatchSize(t *testing.T) {
	frames := []frameOut{
		{typ: proto.FrameData, stream: 1, payload: []byte("abc")},
		{typ: proto.FrameSyn, stream: 2, payload: []byte("target:80")},
	}
	total := 0
	for _, f := range frames {
		total += encodedLen(f)
	}
	if got := len(encodeBatch(frames)); got != total {
		t.Fatalf("encodeBatch produced %d bytes, encodedLen sum = %d", got, total)
	}
}

func TestDatagramEncodingRoundTrip(t *testing.T) {
	encoded, ok := encodeDatagram("example.com:53", []byte{0, 1, 2, 255})
	if !ok {
		t.Fatal("encodeDatagram rejected valid packet")
	}
	target, payload, ok := decodeDatagram(encoded)
	if !ok || target != "example.com:53" || !reflect.DeepEqual(payload, []byte{0, 1, 2, 255}) {
		t.Fatalf("decoded target=%q payload=%v ok=%v", target, payload, ok)
	}
}

func TestV3StreamOffsetEncodings(t *testing.T) {
	data := encodeStreamData(123, []byte("payload"))
	offset, payload, ok := decodeStreamData(data)
	if !ok || offset != 123 || string(payload) != "payload" {
		t.Fatalf("DATA offset=%d payload=%q ok=%v", offset, payload, ok)
	}
	if _, _, ok := decodeStreamData(make([]byte, 8)); ok {
		t.Fatal("empty v4 DATA accepted")
	}
	fin := encodeFinalOffset(987)
	if offset, ok := decodeFinalOffset(fin); !ok || offset != 987 {
		t.Fatalf("FIN final offset=%d ok=%v", offset, ok)
	}
	if maximum, ok := decodeMaxStreamData(encodeMaxStreamData(2048)); !ok || maximum != 2048 {
		t.Fatalf("MAX_STREAM_DATA=%d ok=%v", maximum, ok)
	}
}
