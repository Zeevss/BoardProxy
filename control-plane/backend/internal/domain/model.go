package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidID    = errors.New("invalid identifier")
	ErrInvalidState = errors.New("invalid domain state")
	ErrConflict     = errors.New("optimistic version conflict")
	ErrTokenInvalid = errors.New("enrollment token is invalid")
	ErrTokenExpired = errors.New("enrollment token has expired")
	ErrNotFound     = errors.New("resource not found")
)

type TrafficKind string

const (
	InterfaceTraffic TrafficKind = "interface"
	UserTraffic      TrafficKind = "user"
)

// ConfigRevision is an immutable compiled snapshot. Desired state is the
// latest revision selected for a node; it is not a separately mutable entity.
type ConfigRevision struct {
	NodeID           string    `json:"node_id"`
	Revision         uint64    `json:"revision"`
	PreviousRevision uint64    `json:"previous_revision"`
	CatalogVersion   uint64    `json:"catalog_version"`
	ConfigTOML       []byte    `json:"config_toml"`
	ConfigSHA256     string    `json:"config_sha256"`
	Cause            string    `json:"cause"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Certificate struct {
	PEM       []byte
	CAPEM     []byte
	ExpiresAt time.Time
}

type AppliedState struct {
	Revision uint64
	SHA256   string
}

func ValidID(value string) bool {
	if value == "" || len(value) > 128 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
