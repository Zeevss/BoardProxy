package domain

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maximumLanes = 32

type ResourceState string

const (
	ResourceEnabled  ResourceState = "enabled"
	ResourceDisabled ResourceState = "disabled"
	ResourceRevoked  ResourceState = "revoked"
)

func (s ResourceState) Validate() error {
	switch s {
	case ResourceEnabled, ResourceDisabled, ResourceRevoked:
		return nil
	default:
		return invalid("unsupported resource state %q", s)
	}
}

func (s ResourceState) Enabled() bool { return s == ResourceEnabled }

type Node struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	State     ResourceState `json:"state"`
	Core      CoreSettings  `json:"core"`
	Version   uint64        `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type CoreSettings struct {
	Server        ServerSettings        `json:"server"`
	Transport     TransportSettings     `json:"transport"`
	Management    ManagementSettings    `json:"management"`
	Observability ObservabilitySettings `json:"observability"`
}

type ServerSettings struct {
	PrivateKey         string   `json:"private_key"`
	IdleTimeout        Duration `json:"idle_timeout"`
	AllowPrivateEgress bool     `json:"allow_private_egress"`
}

type TransportSettings struct {
	Window            int      `json:"window"`
	MaxFramePayload   int      `json:"max_frame_payload"`
	StreamWindow      int      `json:"stream_window"`
	MaxStreamWindow   int      `json:"max_stream_window"`
	AckTimeout        Duration `json:"ack_timeout"`
	CoalesceTarget    int      `json:"coalesce_target"`
	StreamIdleTimeout Duration `json:"stream_idle_timeout"`
}

type ManagementSettings struct {
	GRPCListen string `json:"grpc_listen"`
	HTTPListen string `json:"http_listen"`
}

type ObservabilitySettings struct {
	Enabled  bool   `json:"enabled"`
	LogLevel string `json:"log_level"`
}

func DefaultCoreSettings(serverPrivateKey string) CoreSettings {
	return CoreSettings{
		Server: ServerSettings{PrivateKey: serverPrivateKey, IdleTimeout: Duration(90 * time.Second)},
		Transport: TransportSettings{
			MaxFramePayload: 4 << 20, StreamWindow: 1 << 20,
			MaxStreamWindow: 32 << 20, AckTimeout: Duration(6 * time.Second),
		},
		Management:    ManagementSettings{GRPCListen: "unix:///run/bproxy/control.sock"},
		Observability: ObservabilitySettings{Enabled: true, LogLevel: "info"},
	}
}

type Board struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Hash      string        `json:"hash"`
	HubSlide  string        `json:"hub_slide"`
	APIBase   string        `json:"api_base"`
	GuestName string        `json:"guest_name"`
	State     ResourceState `json:"state"`
	MaxLanes  int           `json:"max_lanes"`
	Version   uint64        `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type User struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	PrivateKey  string        `json:"private_key"`
	PublicKey   string        `json:"public_key"`
	State       ResourceState `json:"state"`
	MaxSessions int           `json:"max_sessions"`
	MaxLanes    int           `json:"max_lanes"`
	Version     uint64        `json:"version"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type AssignedUser struct {
	UserID   string   `json:"user_id"`
	BoardIDs []string `json:"board_ids"`
}

type NodeAssignment struct {
	NodeID    string         `json:"node_id"`
	BoardIDs  []string       `json:"board_ids"`
	Users     []AssignedUser `json:"users"`
	Version   uint64         `json:"version"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Catalog is the transactional control aggregate compiled into one core TOML.
// Resource versions protect browser/API edits; Version protects the aggregate
// against concurrent writers in the repository.
type Catalog struct {
	Node       Node           `json:"node"`
	Boards     []Board        `json:"boards"`
	Users      []User         `json:"users"`
	Assignment NodeAssignment `json:"assignment"`
	Version    uint64         `json:"version"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func (c Catalog) Validate() error {
	if c.Version == 0 {
		return invalid("catalog version must be positive")
	}
	if err := c.Node.Validate(); err != nil {
		return fmt.Errorf("catalog node: %w", err)
	}
	if c.Assignment.Version == 0 || c.Assignment.NodeID != c.Node.ID {
		return invalid("assignment must have a version and reference the catalog node")
	}

	boards := make(map[string]Board, len(c.Boards))
	hashes := make(map[string]string, len(c.Boards))
	for _, board := range c.Boards {
		if err := board.Validate(); err != nil {
			return fmt.Errorf("catalog board %q: %w", board.ID, err)
		}
		if _, exists := boards[board.ID]; exists {
			return invalid("duplicate board %q", board.ID)
		}
		if previous := hashes[board.Hash]; previous != "" {
			return invalid("boards %q and %q share hash %q", previous, board.ID, board.Hash)
		}
		boards[board.ID] = board
		hashes[board.Hash] = board.ID
	}

	users := make(map[string]User, len(c.Users))
	identities := make(map[string]string, len(c.Users))
	for _, user := range c.Users {
		identity, err := user.IdentityPublicKey()
		if err != nil {
			return fmt.Errorf("catalog user %q: %w", user.ID, err)
		}
		if _, exists := users[user.ID]; exists {
			return invalid("duplicate user %q", user.ID)
		}
		fingerprint := base64.RawStdEncoding.EncodeToString(identity)
		if previous := identities[fingerprint]; previous != "" {
			return invalid("users %q and %q share an identity", previous, user.ID)
		}
		users[user.ID] = user
		identities[fingerprint] = user.ID
	}
	if err := c.Assignment.validate(boards, users); err != nil {
		return err
	}
	return nil
}

func (n Node) Validate() error {
	if !ValidID(n.ID) || strings.TrimSpace(n.Name) == "" || n.Version == 0 {
		return invalid("id, name and version are required")
	}
	if err := n.State.Validate(); err != nil {
		return err
	}
	if _, err := decodeKey(n.Core.Server.PrivateKey, true); err != nil {
		return fmt.Errorf("server private key: %w", err)
	}
	if n.Core.Server.IdleTimeout < 0 || n.Core.Transport.StreamIdleTimeout < 0 || n.Core.Transport.AckTimeout <= 0 {
		return invalid("durations are invalid")
	}
	transport := n.Core.Transport
	if transport.Window < 0 || transport.CoalesceTarget < 0 || transport.MaxFramePayload <= 0 ||
		transport.StreamWindow <= 0 || transport.MaxStreamWindow < transport.StreamWindow {
		return invalid("transport limits are invalid")
	}
	if err := validateManagement(n.Core.Management); err != nil {
		return err
	}
	switch n.Core.Observability.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return invalid("unsupported log level %q", n.Core.Observability.LogLevel)
	}
	return nil
}

func (b Board) Validate() error {
	if !ValidID(b.ID) || strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Hash) == "" || b.Version == 0 {
		return invalid("id, name, hash and version are required")
	}
	if b.MaxLanes < 1 || b.MaxLanes > maximumLanes {
		return invalid("max lanes must be between 1 and %d", maximumLanes)
	}
	if err := b.State.Validate(); err != nil {
		return err
	}
	if b.APIBase != "" {
		endpoint, err := url.ParseRequestURI(b.APIBase)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return invalid("API base must be an absolute HTTP(S) URL")
		}
	}
	return nil
}

func (u User) Validate() error {
	if !ValidID(u.ID) || strings.TrimSpace(u.Name) == "" || u.Version == 0 {
		return invalid("id, name and version are required")
	}
	if u.MaxSessions < 0 || u.MaxLanes < 1 || u.MaxLanes > maximumLanes {
		return invalid("user limits are invalid")
	}
	_, err := u.IdentityPublicKey()
	return err
}

func (u User) IdentityPublicKey() ([]byte, error) {
	if err := basicUserValidation(u); err != nil {
		return nil, err
	}
	if (u.PrivateKey == "") == (u.PublicKey == "") {
		return nil, invalid("exactly one private or public key is required")
	}
	curve := ecdh.X25519()
	if u.PrivateKey != "" {
		raw, err := decodeKey(u.PrivateKey, true)
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		private, err := curve.NewPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		return private.PublicKey().Bytes(), nil
	}
	raw, err := decodeKey(u.PublicKey, false)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	if _, err := curve.NewPublicKey(raw); err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	return raw, nil
}

func basicUserValidation(u User) error {
	if !ValidID(u.ID) || strings.TrimSpace(u.Name) == "" || u.Version == 0 {
		return invalid("id, name and version are required")
	}
	if u.MaxSessions < 0 || u.MaxLanes < 1 || u.MaxLanes > maximumLanes {
		return invalid("user limits are invalid")
	}
	if err := u.State.Validate(); err != nil {
		return err
	}
	return nil
}

func (a NodeAssignment) validate(boards map[string]Board, users map[string]User) error {
	assignedBoards := make(map[string]bool, len(a.BoardIDs))
	for _, boardID := range a.BoardIDs {
		if _, exists := boards[boardID]; !exists {
			return invalid("assignment references unknown board %q", boardID)
		}
		if assignedBoards[boardID] {
			return invalid("assignment contains duplicate board %q", boardID)
		}
		assignedBoards[boardID] = true
	}
	if len(assignedBoards) == 0 {
		return invalid("assignment must contain at least one board")
	}
	assignedUsers := make(map[string]bool, len(a.Users))
	for _, assignment := range a.Users {
		if _, exists := users[assignment.UserID]; !exists {
			return invalid("assignment references unknown user %q", assignment.UserID)
		}
		if assignedUsers[assignment.UserID] {
			return invalid("assignment contains duplicate user %q", assignment.UserID)
		}
		assignedUsers[assignment.UserID] = true
		if len(assignment.BoardIDs) == 0 {
			return invalid("user %q must have at least one board", assignment.UserID)
		}
		seen := make(map[string]bool, len(assignment.BoardIDs))
		for _, boardID := range assignment.BoardIDs {
			if !assignedBoards[boardID] {
				return invalid("user %q references board %q not assigned to node", assignment.UserID, boardID)
			}
			if seen[boardID] {
				return invalid("user %q contains duplicate board %q", assignment.UserID, boardID)
			}
			seen[boardID] = true
		}
	}
	return nil
}

func (c Catalog) AssignedResources() ([]Board, []struct {
	User   User
	Boards []string
}, error) {
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	boardByID := make(map[string]Board, len(c.Boards))
	for _, board := range c.Boards {
		boardByID[board.ID] = board
	}
	boards := make([]Board, 0, len(c.Assignment.BoardIDs))
	for _, id := range c.Assignment.BoardIDs {
		boards = append(boards, boardByID[id])
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].ID < boards[j].ID })
	userByID := make(map[string]User, len(c.Users))
	for _, user := range c.Users {
		userByID[user.ID] = user
	}
	assignedUsers := make([]struct {
		User   User
		Boards []string
	}, 0, len(c.Assignment.Users))
	for _, assignment := range c.Assignment.Users {
		boardIDs := append([]string(nil), assignment.BoardIDs...)
		sort.Strings(boardIDs)
		assignedUsers = append(assignedUsers, struct {
			User   User
			Boards []string
		}{User: userByID[assignment.UserID], Boards: boardIDs})
	}
	sort.Slice(assignedUsers, func(i, j int) bool { return assignedUsers[i].User.ID < assignedUsers[j].User.ID })
	return boards, assignedUsers, nil
}

func decodeKey(encoded string, private bool) ([]byte, error) {
	if !strings.HasPrefix(encoded, "base64:") {
		return nil, errors.New("expected base64:<32-byte-key>")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "base64:"))
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("key is %d bytes, want 32", len(raw))
	}
	if !private {
		allZero := true
		for _, value := range raw {
			allZero = allZero && value == 0
		}
		if allZero {
			return nil, errors.New("public key is all zero")
		}
	}
	return raw, nil
}

func validateManagement(settings ManagementSettings) error {
	if settings.GRPCListen == "" {
		return invalid("management gRPC listener is required")
	}
	if strings.HasPrefix(settings.GRPCListen, "unix://") {
		if strings.TrimPrefix(settings.GRPCListen, "unix://") == "" {
			return invalid("management gRPC Unix path is empty")
		}
	} else if err := requireLoopback(settings.GRPCListen); err != nil {
		return fmt.Errorf("management gRPC listener: %w", err)
	}
	if settings.HTTPListen != "" {
		if err := requireLoopback(settings.HTTPListen); err != nil {
			return fmt.Errorf("management HTTP listener: %w", err)
		}
	}
	return nil
}

func requireLoopback(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(address, "tcp://"))
	if err != nil || port == "" {
		return invalid("expected host:port")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return invalid("plaintext listener must bind to loopback")
	}
	return nil
}

func invalid(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidState, fmt.Sprintf(format, values...))
}
