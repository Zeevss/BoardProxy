package domain

import "time"

type NodeStatus struct {
	NodeID          string       `json:"node_id"`
	Connected       bool         `json:"connected"`
	BootID          string       `json:"boot_id"`
	AgentVersion    string       `json:"agent_version"`
	CoreVersion     string       `json:"core_version"`
	CoreRunning     bool         `json:"core_running"`
	CoreReady       bool         `json:"core_ready"`
	DesiredRevision uint64       `json:"desired_revision"`
	AppliedRevision uint64       `json:"applied_revision"`
	ConfigSHA256    string       `json:"config_sha256"`
	LastError       string       `json:"last_error"`
	LastSeen        time.Time    `json:"last_seen"`
	LastApply       *ApplyResult `json:"last_apply,omitempty"`
	Version         uint64       `json:"version"`
}

func (s NodeStatus) Drifted() bool {
	return s.DesiredRevision != s.AppliedRevision ||
		(s.LastApply != nil && s.LastApply.Error != "")
}

type NodeHello struct {
	BootID          string
	AgentVersion    string
	CoreVersion     string
	AppliedRevision uint64
	ConfigSHA256    string
}

type NodeHeartbeat struct {
	SampledAt       time.Time
	CoreRunning     bool
	CoreReady       bool
	AppliedRevision uint64
	Error           string
}

type ApplyResult struct {
	DesiredRevision uint64    `json:"desired_revision"`
	RuntimeRevision uint64    `json:"runtime_revision"`
	ConfigSHA256    string    `json:"config_sha256"`
	Error           string    `json:"error"`
	AppliedAt       time.Time `json:"applied_at"`
}

type AuditEvent struct {
	ID              string    `json:"id"`
	NodeID          string    `json:"node_id"`
	Actor           string    `json:"actor"`
	Action          string    `json:"action"`
	ResourceType    string    `json:"resource_type"`
	ResourceID      string    `json:"resource_id"`
	ResourceVersion uint64    `json:"resource_version"`
	CatalogVersion  uint64    `json:"catalog_version"`
	OccurredAt      time.Time `json:"occurred_at"`
}
