package hub

import (
	"testing"
	"time"
)

func TestLanePolicyAddsLanesAtBoundedInterval(t *testing.T) {
	p := newLanePolicy(4)
	now := time.Unix(100, 0)
	if got := p.observe(now, 1, true); got != laneNoop {
		t.Fatalf("first sample action = %v", got)
	}
	addedAt := now.Add(time.Second)
	if got := p.observe(addedAt, 1, true); got != laneAdd {
		t.Fatalf("sustained demand action = %v", got)
	}
	p.laneAdded(addedAt)
	if got := p.observe(addedAt.Add(time.Second), 2, true); got != laneNoop {
		t.Fatalf("lane added during scale interval: %v", got)
	}
	if got := p.observe(addedAt.Add(adaptiveScaleInterval), 2, true); got != laneAdd {
		t.Fatalf("next scale action = %v", got)
	}
}

func TestLanePolicyDrainsIdleLane(t *testing.T) {
	p := newLanePolicy(4)
	now := time.Unix(200, 0)
	for i := 0; i < adaptiveIdleSamples-1; i++ {
		if got := p.observe(now.Add(time.Duration(i)*time.Second), 2, false); got != laneNoop {
			t.Fatalf("early idle action at %d = %v", i, got)
		}
	}
	if got := p.observe(now.Add(adaptiveIdleSamples*time.Second), 2, false); got != laneDrainIdle {
		t.Fatalf("idle action = %v", got)
	}
}
