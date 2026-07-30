package board

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random 32-character hex object id, matching the board's
// client-generated object id/hash format. Object identity is owned by this
// layer, so both the driver and the link layer mint ids through here.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
