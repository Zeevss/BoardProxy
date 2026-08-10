// Package telemetry contains transport-independent, secret-free runtime views.
// Adapters such as HTTP and gRPC may expose these values without depending on
// the server composition root.
package telemetry

import "time"

type Stats struct {
	StartedAt         time.Time      `json:"started_at"`
	Revision          uint64         `json:"revision"`
	UsersConfigured   int            `json:"users_configured"`
	UsersEnabled      int            `json:"users_enabled"`
	UsersOnline       int            `json:"users_online"`
	BoardsConfigured  int            `json:"boards_configured"`
	BoardsEnabled     int            `json:"boards_enabled"`
	BoardsRunning     int            `json:"boards_running"`
	ActiveConnections int            `json:"active_connections"`
	ActiveLanes       int            `json:"active_lanes"`
	ActiveStreams     int            `json:"active_streams"`
	RXBytesSinceStart uint64         `json:"rx_bytes_since_start"`
	TXBytesSinceStart uint64         `json:"tx_bytes_since_start"`
	Users             []UserStats    `json:"users"`
	Boards            []BoardStats   `json:"boards"`
	Network           NetworkStats   `json:"network"`
	Transport         TransportStats `json:"transport"`
}

type UserStats struct {
	Tag         string     `json:"tag"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Online      bool       `json:"online"`
	LastSeen    *time.Time `json:"last_seen_since_start,omitempty"`
	Connections int        `json:"connections"`
	Lanes       int        `json:"lanes"`
	Streams     int        `json:"streams"`
	RXBytes     uint64     `json:"rx_bytes_since_start"`
	TXBytes     uint64     `json:"tx_bytes_since_start"`
	MaxSessions int        `json:"max_sessions"`
	MaxLanes    int        `json:"max_lanes"`
}

type BoardStats struct {
	Tag                    string `json:"tag"`
	Name                   string `json:"name"`
	Hash                   string `json:"hash"`
	Enabled                bool   `json:"enabled"`
	State                  string `json:"state"`
	Error                  string `json:"error,omitempty"`
	Clients                int    `json:"clients"`
	FreePages              int    `json:"free_pages"`
	RXBytes                uint64 `json:"rx_bytes"`
	TXBytes                uint64 `json:"tx_bytes"`
	PageCleanupRuns        uint64 `json:"page_cleanup_runs"`
	PageCleanupDeleted     uint64 `json:"page_cleanup_deleted"`
	PageCleanupFailures    uint64 `json:"page_cleanup_failures"`
	PageCleanupQuarantined uint64 `json:"page_cleanup_quarantined"`
}

type NetworkStats struct {
	Available         bool      `json:"available"`
	Scope             string    `json:"scope"`
	Interfaces        []string  `json:"interfaces"`
	SampledAt         time.Time `json:"sampled_at"`
	RXBytesSinceStart uint64    `json:"rx_bytes_since_start"`
	TXBytesSinceStart uint64    `json:"tx_bytes_since_start"`
	RXBytesPerSecond  float64   `json:"rx_bytes_per_second"`
	TXBytesPerSecond  float64   `json:"tx_bytes_per_second"`
}

type TransportStats struct {
	DisconnectsTotal        uint64     `json:"disconnects_total"`
	ReconnectsTotal         uint64     `json:"reconnects_total"`
	ReconnectAttemptsFailed uint64     `json:"reconnect_attempts_failed"`
	CircuitOpenTotal        uint64     `json:"circuit_open_total"`
	SnapshotObjectsTotal    uint64     `json:"snapshot_objects_total"`
	SnapshotBytesTotal      uint64     `json:"snapshot_bytes_total"`
	ReconnectsLastMinute    int        `json:"reconnects_last_minute"`
	SnapshotBytesLastMinute uint64     `json:"snapshot_bytes_last_minute"`
	LastDisconnectAt        *time.Time `json:"last_disconnect_at,omitempty"`
	LastDisconnectReason    string     `json:"last_disconnect_reason,omitempty"`
	LastReconnectAt         *time.Time `json:"last_reconnect_at,omitempty"`
	LastDowntimeMillis      int64      `json:"last_downtime_ms"`
}
