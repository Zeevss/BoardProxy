package hub

import (
	"encoding/binary"
	"math"

	"bproxy-core/internal/bond"
)

type bundleRequestKind byte

const (
	bundleRequestNew  bundleRequestKind = 1
	bundleRequestJoin bundleRequestKind = 2
)

type bundleRequest struct {
	kind  bundleRequestKind
	id    bond.BundleID
	epoch bond.Epoch
	token bond.JoinToken
}

func encodeNewBundleRequest(id bond.BundleID) []byte {
	b := make([]byte, 1+bond.BundleIDSize)
	b[0] = byte(bundleRequestNew)
	copy(b[1:], id[:])
	return b
}

func encodeJoinBundleRequest(id bond.BundleID, epoch bond.Epoch, token bond.JoinToken) []byte {
	b := make([]byte, 1+bond.BundleIDSize+4+bond.JoinTokenSize)
	b[0] = byte(bundleRequestJoin)
	copy(b[1:], id[:])
	binary.BigEndian.PutUint32(b[1+bond.BundleIDSize:], uint32(epoch))
	copy(b[1+bond.BundleIDSize+4:], token[:])
	return b
}

func decodeBundleRequest(b []byte) (bundleRequest, bool) {
	if len(b) < 1+bond.BundleIDSize {
		return bundleRequest{}, false
	}
	var req bundleRequest
	req.kind = bundleRequestKind(b[0])
	copy(req.id[:], b[1:1+bond.BundleIDSize])
	if req.id.IsZero() {
		return bundleRequest{}, false
	}
	switch req.kind {
	case bundleRequestNew:
		return req, len(b) == 1+bond.BundleIDSize
	case bundleRequestJoin:
		if len(b) != 1+bond.BundleIDSize+4+bond.JoinTokenSize {
			return bundleRequest{}, false
		}
		req.epoch = bond.Epoch(binary.BigEndian.Uint32(b[1+bond.BundleIDSize:]))
		copy(req.token[:], b[1+bond.BundleIDSize+4:])
		return req, req.epoch != 0 && !req.token.IsZero()
	default:
		return bundleRequest{}, false
	}
}

type bundleAssignment struct {
	id       bond.BundleID
	lane     bond.LaneID
	epoch    bond.Epoch
	token    bond.JoinToken
	maxLanes uint8
	page     string
}

func encodeBundleAssignment(a bundleAssignment, version byte) ([]byte, bool) {
	if a.id.IsZero() || a.lane == 0 || a.epoch == 0 || a.token.IsZero() ||
		a.page == "" || len(a.page) > math.MaxUint16 || (version >= 5 && a.maxLanes == 0) {
		return nil, false
	}
	fixed := bond.BundleIDSize + 4 + 4 + bond.JoinTokenSize + 2
	if version >= 5 {
		fixed++
	}
	b := make([]byte, fixed+len(a.page))
	off := 0
	copy(b[off:], a.id[:])
	off += bond.BundleIDSize
	binary.BigEndian.PutUint32(b[off:], uint32(a.lane))
	off += 4
	binary.BigEndian.PutUint32(b[off:], uint32(a.epoch))
	off += 4
	copy(b[off:], a.token[:])
	off += bond.JoinTokenSize
	if version >= 5 {
		b[off] = a.maxLanes
		off++
	}
	binary.BigEndian.PutUint16(b[off:], uint16(len(a.page)))
	off += 2
	copy(b[off:], a.page)
	return b, true
}

func decodeBundleAssignment(b []byte, version byte) (bundleAssignment, bool) {
	fixed := bond.BundleIDSize + 4 + 4 + bond.JoinTokenSize + 2
	if version >= 5 {
		fixed++
	}
	if len(b) < fixed {
		return bundleAssignment{}, false
	}
	var a bundleAssignment
	off := 0
	copy(a.id[:], b[off:off+bond.BundleIDSize])
	off += bond.BundleIDSize
	a.lane = bond.LaneID(binary.BigEndian.Uint32(b[off:]))
	off += 4
	a.epoch = bond.Epoch(binary.BigEndian.Uint32(b[off:]))
	off += 4
	copy(a.token[:], b[off:off+bond.JoinTokenSize])
	off += bond.JoinTokenSize
	if version >= 5 {
		a.maxLanes = b[off]
		off++
	}
	pageLen := int(binary.BigEndian.Uint16(b[off:]))
	off += 2
	if a.id.IsZero() || a.lane == 0 || a.epoch == 0 || a.token.IsZero() ||
		(version >= 5 && a.maxLanes == 0) ||
		pageLen == 0 || len(b) != off+pageLen {
		return bundleAssignment{}, false
	}
	a.page = string(b[off:])
	return a, true
}
