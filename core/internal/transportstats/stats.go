package transportstats

import "time"

// Lane is a read-only diagnostic snapshot of one physical transport lane.
type Lane struct {
	ID               uint32
	CongestionWindow int
	Inflight         int
	PeerWindow       int
	EffectiveWindow  int
	TargetPayload    int
	RTT              time.Duration
	BaseRTT          time.Duration
	ConfirmedBytes   uint64
	Draining         bool
}

type Provider interface {
	TransportStats() []Lane
}
