// Package serverconfig defines and validates the versioned, operator-facing
// server configuration. It deliberately does not expose the transport runtime
// structs: the file format is a stable boundary, while runtime structs may
// evolve with the implementation.
package serverconfig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"bproxy-core/internal/crypto"

	"github.com/BurntSushi/toml"
)

const Version = 1

// Duration is a human-readable TOML duration such as "90s" or "5m".
type Duration time.Duration

func (d *Duration) UnmarshalText(raw []byte) error {
	v, err := time.ParseDuration(string(raw))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) Duration() time.Duration { return time.Duration(d) }

type Config struct {
	Version       int           `toml:"version"`
	Server        Server        `toml:"server"`
	Transport     Transport     `toml:"transport"`
	Management    Management    `toml:"management"`
	Observability Observability `toml:"observability"`
	Boards        []Board       `toml:"boards"`
	Users         []User        `toml:"users"`
}

type Server struct {
	PrivateKey         string   `toml:"private_key"`
	IdleTimeout        Duration `toml:"idle_timeout"`
	AllowPrivateEgress bool     `toml:"allow_private_egress"`
}

type Transport struct {
	Window            int      `toml:"window"`
	MaxFramePayload   int      `toml:"max_frame_payload"`
	StreamWindow      int      `toml:"stream_window"`
	MaxStreamWindow   int      `toml:"max_stream_window"`
	AckTimeout        Duration `toml:"ack_timeout"`
	CoalesceTarget    int      `toml:"coalesce_target"`
	StreamIdleTimeout Duration `toml:"stream_idle_timeout"`
}

type Management struct {
	// GRPCListen accepts unix:///path or a loopback tcp address.
	GRPCListen string `toml:"grpc_listen"`
	// HTTPListen exposes only health, logs and ephemeral statistics. Empty
	// disables the adapter. It is intentionally not a configuration API.
	HTTPListen string `toml:"http_listen"`
}

type Observability struct {
	Enabled  bool   `toml:"enabled"`
	LogLevel string `toml:"log_level"`
}

type Board struct {
	Tag       string `toml:"tag"`
	Name      string `toml:"name"`
	Hash      string `toml:"hash"`
	HubSlide  string `toml:"hub_slide"`
	APIBase   string `toml:"api_base"`
	GuestName string `toml:"guest_name"`
	Enabled   *bool  `toml:"enabled"`
	MaxLanes  int    `toml:"max_lanes"`
}

func (b Board) IsEnabled() bool { return b.Enabled == nil || *b.Enabled }

type User struct {
	Tag        string `toml:"tag"`
	Name       string `toml:"name"`
	PrivateKey string `toml:"private_key"`
	// PublicKey is a migration-only alternative for users whose old private
	// key was issued once and is no longer available to the server.
	PublicKey   string   `toml:"public_key"`
	Enabled     *bool    `toml:"enabled"`
	Boards      []string `toml:"boards"`
	MaxSessions int      `toml:"max_sessions"`
	MaxLanes    int      `toml:"max_lanes"`
}

func (u User) IsEnabled() bool { return u.Enabled == nil || *u.Enabled }

// UserIdentity is the compiled, secret-aware identity used by the runtime.
type UserIdentity struct {
	Private []byte
	Public  []byte
}

// Defaults returns a config with all optional operational defaults applied.
func Defaults() Config {
	return Config{
		Version: Version,
		Server:  Server{IdleTimeout: Duration(90 * time.Second)},
		Transport: Transport{
			MaxFramePayload: 4 << 20,
			StreamWindow:    1 << 20,
			MaxStreamWindow: 32 << 20,
			AckTimeout:      Duration(6 * time.Second),
		},
		Management:    Management{GRPCListen: "unix:///tmp/bproxy-control.sock"},
		Observability: Observability{Enabled: true, LogLevel: "info"},
	}
}

// Load reads a strict TOML configuration from a file or stdin:/-. Unknown
// fields are rejected so misspelled security and limit settings cannot silently
// fall back to defaults.
func Load(source string, stdin io.Reader) (Config, error) {
	if strings.TrimSpace(source) == "" {
		return Config{}, errors.New("serverconfig: config source is required")
	}
	var r io.Reader
	if source == "stdin:" || source == "-" {
		if stdin == nil {
			return Config{}, errors.New("serverconfig: stdin is unavailable")
		}
		r = stdin
	} else {
		f, err := os.Open(source)
		if err != nil {
			return Config{}, fmt.Errorf("serverconfig: open %q: %w", source, err)
		}
		defer f.Close()
		r = f
	}
	return Decode(r)
}

func Decode(r io.Reader) (Config, error) {
	cfg := Defaults()
	md, err := toml.NewDecoder(r).Decode(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("serverconfig: decode TOML: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		fields := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			fields = append(fields, key.String())
		}
		sort.Strings(fields)
		return Config{}, fmt.Errorf("serverconfig: unknown fields: %s", strings.Join(fields, ", "))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("serverconfig: version must be %d", Version)
	}
	if _, err := DecodePrivateKey(c.Server.PrivateKey); err != nil {
		return fmt.Errorf("serverconfig: server.private_key: %w", err)
	}
	if c.Server.IdleTimeout.Duration() < 0 || c.Transport.AckTimeout.Duration() <= 0 || c.Transport.StreamIdleTimeout.Duration() < 0 {
		return errors.New("serverconfig: durations must be positive (idle timeouts may be zero)")
	}
	if c.Transport.MaxFramePayload <= 0 || c.Transport.StreamWindow <= 0 || c.Transport.MaxStreamWindow < c.Transport.StreamWindow {
		return errors.New("serverconfig: invalid transport payload/window settings")
	}
	if c.Transport.Window < 0 || c.Transport.CoalesceTarget < 0 {
		return errors.New("serverconfig: transport.window and coalesce_target must not be negative")
	}
	if err := validateManagement(c.Management); err != nil {
		return err
	}
	if c.Observability.LogLevel == "" {
		return errors.New("serverconfig: observability.log_level is required")
	}
	switch c.Observability.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("serverconfig: unsupported log level %q", c.Observability.LogLevel)
	}

	boardTags := make(map[string]bool, len(c.Boards))
	boardHashes := make(map[string]string, len(c.Boards))
	for i, b := range c.Boards {
		path := fmt.Sprintf("boards[%d]", i)
		if err := validateTag(b.Tag); err != nil {
			return fmt.Errorf("serverconfig: %s.tag: %w", path, err)
		}
		if boardTags[b.Tag] {
			return fmt.Errorf("serverconfig: duplicate board tag %q", b.Tag)
		}
		boardTags[b.Tag] = true
		if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Hash) == "" {
			return fmt.Errorf("serverconfig: %s name and hash are required", path)
		}
		if prior := boardHashes[b.Hash]; prior != "" {
			return fmt.Errorf("serverconfig: board hash %q is shared by %q and %q", b.Hash, prior, b.Tag)
		}
		boardHashes[b.Hash] = b.Tag
		if b.MaxLanes < 1 || b.MaxLanes > 32 {
			return fmt.Errorf("serverconfig: %s.max_lanes must be between 1 and 32", path)
		}
		if b.APIBase != "" {
			endpoint, err := url.ParseRequestURI(b.APIBase)
			if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
				return fmt.Errorf("serverconfig: %s.api_base must be an absolute http(s) URL", path)
			}
		}
	}

	userTags := make(map[string]bool, len(c.Users))
	publicKeys := make(map[string]string, len(c.Users))
	for i, u := range c.Users {
		path := fmt.Sprintf("users[%d]", i)
		if err := validateTag(u.Tag); err != nil {
			return fmt.Errorf("serverconfig: %s.tag: %w", path, err)
		}
		if userTags[u.Tag] {
			return fmt.Errorf("serverconfig: duplicate user tag %q", u.Tag)
		}
		userTags[u.Tag] = true
		if strings.TrimSpace(u.Name) == "" {
			return fmt.Errorf("serverconfig: %s.name is required", path)
		}
		identity, err := u.Identity()
		if err != nil {
			return fmt.Errorf("serverconfig: %s: %w", path, err)
		}
		fingerprint := base64.RawStdEncoding.EncodeToString(identity.Public)
		if prior := publicKeys[fingerprint]; prior != "" {
			return fmt.Errorf("serverconfig: users %q and %q have the same public key", prior, u.Tag)
		}
		publicKeys[fingerprint] = u.Tag
		if len(u.Boards) == 0 {
			return fmt.Errorf("serverconfig: %s.boards must explicitly list at least one board", path)
		}
		seenBoards := make(map[string]bool, len(u.Boards))
		for _, tag := range u.Boards {
			if !boardTags[tag] {
				return fmt.Errorf("serverconfig: %s references unknown board %q", path, tag)
			}
			if seenBoards[tag] {
				return fmt.Errorf("serverconfig: %s contains duplicate board %q", path, tag)
			}
			seenBoards[tag] = true
		}
		if u.MaxSessions < 0 {
			return fmt.Errorf("serverconfig: %s.max_sessions must not be negative", path)
		}
		if u.MaxLanes < 1 || u.MaxLanes > 32 {
			return fmt.Errorf("serverconfig: %s.max_lanes must be between 1 and 32", path)
		}
	}
	return nil
}

func validateManagement(m Management) error {
	if m.GRPCListen == "" {
		return errors.New("serverconfig: management.grpc_listen is required")
	}
	if strings.HasPrefix(m.GRPCListen, "unix://") {
		if strings.TrimPrefix(m.GRPCListen, "unix://") == "" {
			return errors.New("serverconfig: management.grpc_listen has an empty unix path")
		}
	} else if err := requireLoopbackAddress(m.GRPCListen); err != nil {
		return fmt.Errorf("serverconfig: management.grpc_listen: %w", err)
	}
	if m.HTTPListen != "" {
		if err := requireLoopbackAddress(m.HTTPListen); err != nil {
			return fmt.Errorf("serverconfig: management.http_listen: %w", err)
		}
	}
	return nil
}

func requireLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(address, "tcp://"))
	if err != nil || port == "" {
		return fmt.Errorf("expected host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("plaintext management listeners must bind to loopback")
	}
	return nil
}

func validateTag(tag string) error {
	if tag == "" {
		return errors.New("tag is required")
	}
	for _, r := range tag {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("tag %q contains unsupported character %q", tag, r)
		}
	}
	return nil
}

func DecodePrivateKey(encoded string) ([]byte, error) {
	if !strings.HasPrefix(encoded, "base64:") {
		return nil, errors.New("expected base64:<32-byte-key>")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "base64:"))
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != crypto.KeySize {
		return nil, fmt.Errorf("key is %d bytes, want %d", len(raw), crypto.KeySize)
	}
	return raw, nil
}

func DecodePublicKey(encoded string) ([]byte, error) {
	if !strings.HasPrefix(encoded, "base64:") {
		return nil, errors.New("expected base64:<32-byte-key>")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "base64:"))
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	if err := crypto.ValidatePublic(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (u User) Identity() (UserIdentity, error) {
	if (u.PrivateKey == "") == (u.PublicKey == "") {
		return UserIdentity{}, errors.New("exactly one of private_key or public_key is required")
	}
	if u.PrivateKey != "" {
		private, err := DecodePrivateKey(u.PrivateKey)
		if err != nil {
			return UserIdentity{}, fmt.Errorf("private_key: %w", err)
		}
		kp, err := crypto.KeypairFromPrivate(private)
		if err != nil {
			return UserIdentity{}, fmt.Errorf("private_key: %w", err)
		}
		return UserIdentity{Private: private, Public: kp.Public()}, nil
	}
	public, err := DecodePublicKey(u.PublicKey)
	if err != nil {
		return UserIdentity{}, fmt.Errorf("public_key: %w", err)
	}
	return UserIdentity{Public: public}, nil
}
