// Package bond contains protocol-neutral identities shared by the v3
// rendezvous and the future multi-lane transport.
package bond

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

const (
	BundleIDSize  = 16
	JoinTokenSize = 32
)

var ErrInvalidBundleID = errors.New("bond: invalid bundle id")

// BundleID identifies one logical connection independently of its pages.
type BundleID [BundleIDSize]byte

func NewBundleID() (BundleID, error) {
	var id BundleID
	_, err := rand.Read(id[:])
	return id, err
}

func ParseBundleID(raw string) (BundleID, error) {
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != BundleIDSize {
		return BundleID{}, ErrInvalidBundleID
	}
	var id BundleID
	copy(id[:], decoded)
	return id, nil
}

func (id BundleID) String() string { return hex.EncodeToString(id[:]) }

func (id BundleID) IsZero() bool { return id == BundleID{} }

// LaneID identifies one physical page session inside a bundle.
type LaneID uint32

// Epoch changes whenever the entire logical bundle is re-created.
type Epoch uint32

const (
	FirstLane  LaneID = 1
	FirstEpoch Epoch  = 1
)

// JoinToken proves that a later handshake belongs to an existing bundle.
// It is returned only inside the authenticated Noise response.
type JoinToken [JoinTokenSize]byte

func NewJoinToken() (JoinToken, error) {
	var token JoinToken
	_, err := rand.Read(token[:])
	return token, err
}

func (t JoinToken) Equal(other JoinToken) bool {
	return subtle.ConstantTimeCompare(t[:], other[:]) == 1
}

func (t JoinToken) IsZero() bool { return t == JoinToken{} }
