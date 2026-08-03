package yandex

import (
	"sort"
	"sync"
	"time"
)

const reconnectMetricWindow = 5 * time.Minute

// ReconnectMetrics accumulates transport-level reconnect and snapshot costs.
// A server shares one instance across hub-control and lane sessions.
type ReconnectMetrics struct {
	mu        sync.Mutex
	startedAt time.Time
	roles     map[string]*roleMetrics
	recent    []recentMetric
}

type roleMetrics struct {
	Role                    string
	Board                   string
	DisconnectsTotal        uint64
	ReconnectsTotal         uint64
	ReconnectAttemptsFailed uint64
	CircuitOpenTotal        uint64
	SnapshotObjectsTotal    uint64
	SnapshotBytesTotal      uint64
	LastDisconnectAt        time.Time
	LastDisconnectReason    string
	LastConnectedFor        time.Duration
	LastReconnectAt         time.Time
	LastDowntime            time.Duration
	LastSnapshotObjects     int
	LastSnapshotBytes       uint64
}

type recentMetric struct {
	at            time.Time
	key           string
	snapshotBytes uint64
}

// ReconnectMetricsSnapshot is a detached aggregate suitable for management API
// conversion and periodic logs.
type ReconnectMetricsSnapshot struct {
	StartedAt                    time.Time
	DisconnectsTotal             uint64
	ReconnectsTotal              uint64
	ReconnectAttemptsFailed      uint64
	CircuitOpenTotal             uint64
	SnapshotObjectsTotal         uint64
	SnapshotBytesTotal           uint64
	ReconnectsLastMinute         int
	ReconnectsLastFiveMinutes    int
	SnapshotBytesLastMinute      uint64
	SnapshotBytesLastFiveMinutes uint64
	LastDisconnectAt             time.Time
	LastDisconnectReason         string
	LastConnectedFor             time.Duration
	LastReconnectAt              time.Time
	LastDowntime                 time.Duration
	LastSnapshotObjects          int
	LastSnapshotBytes            uint64
	PerRole                      []ReconnectRoleSnapshot
}

type ReconnectRoleSnapshot struct {
	Role                    string
	Board                   string
	DisconnectsTotal        uint64
	ReconnectsTotal         uint64
	ReconnectAttemptsFailed uint64
	CircuitOpenTotal        uint64
	SnapshotObjectsTotal    uint64
	SnapshotBytesTotal      uint64
	ReconnectsLastMinute    int
	SnapshotBytesLastMinute uint64
	LastDisconnectAt        time.Time
	LastDisconnectReason    string
	LastConnectedFor        time.Duration
	LastReconnectAt         time.Time
	LastDowntime            time.Duration
	LastSnapshotObjects     int
	LastSnapshotBytes       uint64
}

func NewReconnectMetrics() *ReconnectMetrics {
	return &ReconnectMetrics{startedAt: time.Now(), roles: make(map[string]*roleMetrics)}
}

func metricKey(board, role string) string { return board + "\x00" + role }

func (m *ReconnectMetrics) role(board, role string) (string, *roleMetrics) {
	key := metricKey(board, role)
	r := m.roles[key]
	if r == nil {
		r = &roleMetrics{Role: role, Board: board}
		m.roles[key] = r
	}
	return key, r
}

func (m *ReconnectMetrics) recordDisconnect(board, role, reason string, connectedFor time.Duration) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	_, r := m.role(board, role)
	r.DisconnectsTotal++
	r.LastDisconnectAt = now
	r.LastDisconnectReason = reason
	r.LastConnectedFor = connectedFor
	m.mu.Unlock()
}

func (m *ReconnectMetrics) recordAttemptFailure(board, role string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	_, r := m.role(board, role)
	r.ReconnectAttemptsFailed++
	m.mu.Unlock()
}

func (m *ReconnectMetrics) recordCircuitOpen(board, role string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	_, r := m.role(board, role)
	r.CircuitOpenTotal++
	m.mu.Unlock()
}

func (m *ReconnectMetrics) recordReconnect(board, role string, downtime time.Duration, snapshotObjects int, snapshotBytes uint64) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	key, r := m.role(board, role)
	r.ReconnectsTotal++
	r.SnapshotObjectsTotal += uint64(snapshotObjects)
	r.SnapshotBytesTotal += snapshotBytes
	r.LastReconnectAt = now
	r.LastDowntime = downtime
	r.LastSnapshotObjects = snapshotObjects
	r.LastSnapshotBytes = snapshotBytes
	m.recent = append(m.recent, recentMetric{at: now, key: key, snapshotBytes: snapshotBytes})
	m.prune(now)
	m.mu.Unlock()
}

func (m *ReconnectMetrics) prune(now time.Time) {
	cutoff := now.Add(-reconnectMetricWindow)
	first := 0
	for first < len(m.recent) && m.recent[first].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		m.recent = append([]recentMetric(nil), m.recent[first:]...)
	}
}

func (m *ReconnectMetrics) Snapshot() ReconnectMetricsSnapshot {
	if m == nil {
		return ReconnectMetricsSnapshot{}
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	out := ReconnectMetricsSnapshot{StartedAt: m.startedAt}
	perRole := make(map[string]*ReconnectRoleSnapshot, len(m.roles))
	for key, r := range m.roles {
		rout := ReconnectRoleSnapshot{
			Role: r.Role, Board: r.Board,
			DisconnectsTotal: r.DisconnectsTotal, ReconnectsTotal: r.ReconnectsTotal,
			ReconnectAttemptsFailed: r.ReconnectAttemptsFailed,
			CircuitOpenTotal:        r.CircuitOpenTotal,
			SnapshotObjectsTotal:    r.SnapshotObjectsTotal, SnapshotBytesTotal: r.SnapshotBytesTotal,
			LastDisconnectAt: r.LastDisconnectAt, LastDisconnectReason: r.LastDisconnectReason,
			LastConnectedFor: r.LastConnectedFor, LastReconnectAt: r.LastReconnectAt,
			LastDowntime: r.LastDowntime, LastSnapshotObjects: r.LastSnapshotObjects,
			LastSnapshotBytes: r.LastSnapshotBytes,
		}
		perRole[key] = &rout
		out.DisconnectsTotal += r.DisconnectsTotal
		out.ReconnectsTotal += r.ReconnectsTotal
		out.ReconnectAttemptsFailed += r.ReconnectAttemptsFailed
		out.CircuitOpenTotal += r.CircuitOpenTotal
		out.SnapshotObjectsTotal += r.SnapshotObjectsTotal
		out.SnapshotBytesTotal += r.SnapshotBytesTotal
		if r.LastDisconnectAt.After(out.LastDisconnectAt) {
			out.LastDisconnectAt = r.LastDisconnectAt
			out.LastDisconnectReason = r.LastDisconnectReason
			out.LastConnectedFor = r.LastConnectedFor
		}
		if r.LastReconnectAt.After(out.LastReconnectAt) {
			out.LastReconnectAt = r.LastReconnectAt
			out.LastDowntime = r.LastDowntime
			out.LastSnapshotObjects = r.LastSnapshotObjects
			out.LastSnapshotBytes = r.LastSnapshotBytes
		}
	}
	minuteAgo := now.Add(-time.Minute)
	for _, ev := range m.recent {
		out.ReconnectsLastFiveMinutes++
		out.SnapshotBytesLastFiveMinutes += ev.snapshotBytes
		if !ev.at.Before(minuteAgo) {
			out.ReconnectsLastMinute++
			out.SnapshotBytesLastMinute += ev.snapshotBytes
			if r := perRole[ev.key]; r != nil {
				r.ReconnectsLastMinute++
				r.SnapshotBytesLastMinute += ev.snapshotBytes
			}
		}
	}
	for _, r := range perRole {
		out.PerRole = append(out.PerRole, *r)
	}
	sort.Slice(out.PerRole, func(i, j int) bool {
		if out.PerRole[i].Board == out.PerRole[j].Board {
			return out.PerRole[i].Role < out.PerRole[j].Role
		}
		return out.PerRole[i].Board < out.PerRole[j].Board
	})
	return out
}
