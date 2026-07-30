// Package config defines BoardProxy's runtime configuration and its defaults.
//
// Both binaries share Config; role-specific fields (client listen address,
// server hub page) are grouped but live in one struct so a single file or set
// of environment variables configures either role. Flag/file wiring lives in
// the cmd/ binaries; this package owns the shape and the defaults.
package config

import (
	"time"

	"bproxy-core/internal/netprotect"
)

// Board configures access to the whiteboard used as transport.
type Board struct {
	// APIBase is the REST entry point, e.g. https://boards.yandex.ru/api.
	APIBase string
	// Hash is the board (whiteboard) hash the proxy operates on.
	Hash string
	// GuestName is the guest display name used when joining. A random suffix is
	// appended per session so concurrent guests are distinguishable.
	GuestName string
}

// Transport tunes the reliable-link and mux layers.
type Transport struct {
	// Window is the receive window (rwnd) advertised to the peer: a ceiling on
	// how many unacknowledged objects per direction we're willing to buffer, not
	// a throughput knob (that's the adaptive limiter, see link.MaxConcurrency).
	// 0 uses the transport layer's own default, which already matches the
	// limiter's ceiling so Window never becomes the practical limit.
	Window int
	// MaxFramePayload is the largest mux-frame payload, in bytes, that fits in a
	// single board object after encoding. Larger writes are fragmented.
	MaxFramePayload int
	// StreamWindow is the per-stream receive window in bytes (mux flow control).
	StreamWindow int
	// MaxStreamWindow caps automatic per-stream receive-window growth.
	MaxStreamWindow int
	// AckTimeout bounds how long a board write waits for its board-level ack
	// before being retried (idempotently, by object id).
	AckTimeout time.Duration
	// CoalesceTarget, if set (>0), is a hard ceiling on the byte budget the
	// mux writer aims for when combining queued frames into a single board
	// object — an optional manual override. 0 (default) means fully
	// adaptive: the target is found continuously by link's sizer (see
	// internal/link/sizer.go) from observed RTT-per-byte, not a fixed value.
	CoalesceTarget int
	// StreamIdleTimeout resets an individual stream that has carried no
	// traffic for this long, even while other streams on the same page stay
	// active. Server.IdleTimeout is a separate whole-client heartbeat timeout.
	// 0 disables the sweep.
	StreamIdleTimeout time.Duration
}

// Client configures the local proxy front-end.
type Client struct {
	// Listen is the local address for the mixed SOCKS5/HTTP proxy, e.g.
	// 127.0.0.1:1080.
	Listen string
	// Keylink is the bproxy:// connection string carrying the client's private
	// key, the server's public key, and optionally the board hashes to use.
	Keylink string
	// Protector excludes client control-plane sockets from an OS VPN. Android
	// supplies VpnService.protect(fd); nil is the desktop/default behavior.
	Protector netprotect.Protector
	// MaxLanes is the maximum number of physical pages used by one bundle.
	MaxLanes int
}

// Server configures the hub and egress side.
type Server struct {
	// HubPage is the slide hash reserved as the rendezvous hub. Empty means
	// resolve it deterministically: the alphabetically first slide hash on the
	// board (see app.resolveHubSlide).
	HubPage string
	// IdleTimeout releases a client's page if no event (including the periodic
	// link heartbeat) arrives for this long. This reclaims unclean disconnects
	// even when their mux streams were left open. 0 disables it.
	IdleTimeout time.Duration
	// MaxLanes limits physical pages accepted into one client bundle.
	MaxLanes int
	// Socket is the unix-socket path for the local management API (clients /
	// boards). The server listens here; the CLI dials it.
	Socket string
	// WebAPI, if set, is a TCP address (host:port) where the SAME management
	// API also listens over plain HTTP — for remote/scripted access instead of
	// the local unix socket. Empty (default) disables it. There is no built-in
	// authentication: binding anything other than a loopback address exposes
	// full control (client/board CRUD, restart) to whoever can reach it: put
	// it behind your own auth/reverse proxy, or a token via WebAPIToken.
	WebAPI string
	// WebAPIToken, if set, requires "Authorization: Bearer <token>" on every
	// WebAPI request. Optional — WebAPI works without it (with a startup
	// warning if bound to a non-loopback address).
	WebAPIToken string
	// WebUIPassword, if set, enables password login for the web panel over
	// WebAPI: POST /login checks this password and issues a signed session
	// cookie that authorises subsequent requests. Empty disables password
	// login (only the bearer token, or open access on loopback, applies).
	WebUIPassword string
	// KeyPath is the file holding the server's static X25519 private key (32
	// raw bytes). If it does not exist, the server generates a new keypair and
	// writes it here on first start; if it exists, the server loads it — so
	// the server's identity (and therefore previously issued keylinks) survives
	// restarts. Deleting the file forces a fresh identity, invalidating every
	// keylink issued under the old one.
	//
	// This must not default to a temp directory: an OS reboot or tmpfiles.d
	// sweep would silently wipe it, defeating the whole point of persisting
	// identity across restarts. cmd/bproxy defaults it to a file next to the
	// running executable; this static fallback (relative to the working
	// directory) only applies if RunServer is used directly without going
	// through that flag wiring.
	KeyPath string
}

// Store configures the identity store.
type Store struct {
	// Path is the SQLite database file path. If it (or its parent directory)
	// does not exist, the server creates it on start. Server only.
	//
	// Like KeyPath, this must not default to a temp directory — it holds
	// provisioned users and served boards, which must survive reboots.
	// cmd/bproxy defaults it under the user's config directory
	// (os.UserConfigDir, i.e. ~/.config/bproxy on Linux); this static
	// fallback (relative to the working directory) only applies if RunServer
	// is used directly without going through that flag wiring.
	Path string
}

// Config is the full BoardProxy configuration.
type Config struct {
	LogLevel  string
	Board     Board
	Transport Transport
	Client    Client
	Server    Server
	Store     Store
}

// Default returns a Config populated with sensible defaults. Board.Hash is left
// empty and must be supplied by the caller.
func Default() Config {
	return Config{
		LogLevel: "info",
		Board: Board{
			APIBase:   "https://boards.yandex.ru/api",
			GuestName: "bproxy",
		},
		Transport: Transport{
			// Window: 0 — оставляем link-слою его собственный дефолт (см. поле выше).
			MaxFramePayload: 2048 * 1024 * 2, // 2 MiB
			StreamWindow:    1 << 20,
			MaxStreamWindow: 32 << 20,
			AckTimeout:      6 * time.Second,
			// CoalesceTarget: 0 — без ручного потолка, полностью адаптивно
			// (см. поле выше).
			StreamIdleTimeout: 2 * time.Minute,
		},
		Client: Client{
			Listen:   "127.0.0.1:1080",
			MaxLanes: 4,
		},
		Server: Server{
			// Три пропущенных heartbeat-интервала (link шлёт их раз в 30с):
			// достаточно терпимо к краткому лагу доски, но не удерживает страницу
			// аварийно завершившегося клиента пять минут.
			IdleTimeout: 90 * time.Second,
			MaxLanes:    4,
			Socket:      "/tmp/bproxy.sock",
			KeyPath:     "bproxy.key",
		},
		Store: Store{
			Path: "bproxy.db",
		},
	}
}
