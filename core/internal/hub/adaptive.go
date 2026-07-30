package hub

import (
	"context"
	"time"

	"bproxy-core/internal/bond"
	"bproxy-core/internal/mux"
)

const (
	adaptiveSampleInterval  = time.Second
	adaptiveDemandSamples   = 2
	adaptiveIdleSamples     = 30
	adaptiveScaleInterval   = 3 * time.Second
	adaptivePressureBackoff = 15 * time.Second
	adaptiveDrainTimeout    = 10 * time.Second
	adaptiveTransferDemand  = 512 << 10
)

type laneAction uint8

const (
	laneNoop laneAction = iota
	laneAdd
	laneDrainIdle
)

type lanePolicy struct {
	maxLanes int

	demandSamples int
	idleSamples   int
	cooldownUntil time.Time
}

func newLanePolicy(maxLanes int) *lanePolicy {
	if maxLanes < 1 {
		maxLanes = 1
	}
	return &lanePolicy{maxLanes: maxLanes}
}

func (p *lanePolicy) observe(now time.Time, lanes int, demand bool) laneAction {
	if demand {
		p.demandSamples++
		p.idleSamples = 0
	} else {
		p.demandSamples = 0
		p.idleSamples++
	}

	if now.Before(p.cooldownUntil) {
		return laneNoop
	}
	if p.idleSamples >= adaptiveIdleSamples && lanes > 1 {
		p.idleSamples = 0
		p.cooldownUntil = now.Add(adaptiveScaleInterval)
		return laneDrainIdle
	}
	if p.demandSamples >= adaptiveDemandSamples && lanes < p.maxLanes {
		p.demandSamples = 0
		return laneAdd
	}
	return laneNoop
}

func (p *lanePolicy) laneAdded(now time.Time) {
	p.cooldownUntil = now.Add(adaptiveScaleInterval)
}

func (p *lanePolicy) joinFailed(now time.Time) {
	p.cooldownUntil = now.Add(adaptivePressureBackoff)
}

func startAdaptiveLanes(cfg ClientConfig, b *bond.Conn, m *mux.Session, bundle BundleInfo, maxLanes int) {
	if cfg.Dialer == nil || maxLanes <= b.LaneCount() {
		return
	}
	go runAdaptiveLanes(cfg, b, m, bundle, maxLanes)
}

func runAdaptiveLanes(cfg ClientConfig, b *bond.Conn, m *mux.Session, bundle BundleInfo, maxLanes int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-m.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	log := rvLog(cfg.Link.Log)
	policy := newLanePolicy(maxLanes)
	lanes := map[bond.LaneID]BundleInfo{bundle.LaneID: bundle}
	var lastWritten, lastReceived uint64
	ticker := time.NewTicker(adaptiveSampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			stats := m.Stats()
			active := make(map[bond.LaneID]bool)
			for _, id := range b.LaneIDs() {
				active[id] = true
			}
			for id := range lanes {
				if !active[id] {
					delete(lanes, id)
				}
			}
			writtenDelta := stats.Written - lastWritten
			receivedDelta := stats.Received - lastReceived
			lastWritten, lastReceived = stats.Written, stats.Received
			demand := stats.BacklogFrames > 0 || stats.BlockedWriters > 0 ||
				writtenDelta >= adaptiveTransferDemand ||
				receivedDelta >= adaptiveTransferDemand
			switch action := policy.observe(now, b.LaneCount(), demand); action {
			case laneAdd:
				joinCtx, joinCancel := context.WithTimeout(ctx, rendezvousTimeout)
				info, err := joinBundleLane(joinCtx, cfg, b, bundle)
				joinCancel()
				if err != nil {
					policy.joinFailed(now)
					log.Warn("hub: adaptive lane unavailable",
						"bundle", bundle.ID.String(), "active_lanes", b.LaneCount(),
						"cooldown", adaptivePressureBackoff, "err", err)
					continue
				}
				lanes[info.LaneID] = info
				policy.laneAdded(time.Now())
				log.Info("hub: adaptive lane added",
					"bundle", bundle.ID.String(), "lane", info.LaneID,
					"active_lanes", b.LaneCount())
			case laneDrainIdle:
				if id := lastDynamicLane(lanes, bundle.LaneID); id != 0 {
					drainAdaptiveLane(ctx, log, b, lanes, bundle.ID, id, "idle")
				}
			}
		}
	}
}

func lastDynamicLane(lanes map[bond.LaneID]BundleInfo, primary bond.LaneID) bond.LaneID {
	var selected bond.LaneID
	for id := range lanes {
		if id != primary && id > selected {
			selected = id
		}
	}
	return selected
}

func drainAdaptiveLane(parent context.Context, log interface {
	Info(string, ...any)
}, b *bond.Conn, lanes map[bond.LaneID]BundleInfo, bundleID bond.BundleID, id bond.LaneID, reason string) {
	ctx, cancel := context.WithTimeout(parent, adaptiveDrainTimeout)
	err := b.DrainLane(ctx, id)
	cancel()
	delete(lanes, id)
	log.Info("hub: adaptive lane drained",
		"bundle", bundleID.String(), "lane", id, "reason", reason,
		"active_lanes", b.LaneCount(), "err", err)
}
