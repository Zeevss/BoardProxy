package yandex

import (
	"testing"
	"time"
)

func TestReconnectMetricsAggregatesRoles(t *testing.T) {
	m := NewReconnectMetrics()
	m.recordDisconnect("board-a", "server-lane", "heartbeat timeout", 3*time.Second)
	m.recordAttemptFailure("board-a", "server-lane")
	m.recordCircuitOpen("board-a", "server-lane")
	m.recordReconnect("board-a", "server-lane", time.Second, 7, 1024)
	m.recordReconnect("board-a", "hub-control", 2*time.Second, 2, 256)

	got := m.Snapshot()
	if got.DisconnectsTotal != 1 || got.ReconnectsTotal != 2 || got.ReconnectAttemptsFailed != 1 || got.CircuitOpenTotal != 1 {
		t.Fatalf("totals = %+v", got)
	}
	if got.SnapshotObjectsTotal != 9 || got.SnapshotBytesTotal != 1280 || got.ReconnectsLastMinute != 2 {
		t.Fatalf("snapshot totals = %+v", got)
	}
	if len(got.PerRole) != 2 {
		t.Fatalf("per role = %+v", got.PerRole)
	}
}
