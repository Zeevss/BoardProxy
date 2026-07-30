package codec

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Base64Codec is the v1 codec: marker + base64(frame). Padding is omitted
// (RawStdEncoding) to keep object values a little shorter.
type Base64Codec struct{}

var _ Codec = Base64Codec{}

func (Base64Codec) Encode(frame []byte) (string, error) {
	return marker + base64.RawStdEncoding.EncodeToString(frame), nil
}

func (Base64Codec) Decode(value string) ([]byte, error) {
	rest, ok := strings.CutPrefix(value, marker)
	if !ok {
		return nil, ErrNotProtocol
	}
	frame, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil {
		return nil, fmt.Errorf("codec: corrupt base64 payload: %w", err)
	}
	return frame, nil
}
