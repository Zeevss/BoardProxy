// Package codec is the L1 "line coding" layer: it turns a link frame (raw
// bytes) into the text value of a whiteboard object and back.
//
// A codec also marks our objects so the link layer can tell BoardProxy traffic
// apart from unrelated objects on the same page (human notes, other tools).
// Decode reports ErrNotProtocol for any value that is not one of ours.
//
// v1 uses base64 (Base64Codec). A future codec may replace base64 with a
// denser text encoding; the interface stays the same.
package codec

import "errors"

// ErrNotProtocol is returned by Decode when a value carries no BoardProxy
// marker, i.e. it is not one of our objects and should be ignored.
var ErrNotProtocol = errors.New("codec: not a BoardProxy object")

// Codec encodes a frame into an object's text value and decodes it back.
type Codec interface {
	// Encode renders frame as the text value of a whiteboard object.
	Encode(frame []byte) (string, error)
	// Decode parses a value back into a frame. It returns ErrNotProtocol if the
	// value is not a BoardProxy object, or another error if it is ours but
	// corrupt.
	Decode(value string) ([]byte, error)
}
