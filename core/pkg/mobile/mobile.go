// Package mobile is the gomobile-friendly facade for the BoardProxy client.
// It exposes only strings, booleans, errors, interfaces, and opaque objects so
// `gomobile bind` can generate a small, stable Java/Kotlin API.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"bproxy-core/pkg/bproxy"
	subscribesdk "github.com/Zeevss/BoardProxy/subscribe/sdk"
)

var mobileSubscriptionClient = &subscribesdk.Client{
	HTTP:  &http.Client{Timeout: 12 * time.Second},
	Cache: subscribesdk.NewMemoryCache(),
}

// ResolveSubscription fetches and authenticates a subscription URL through
// the public endpoint with Yandex recovery and returns the snapshot as JSON.
// The URL and contained keylinks are credentials and must never be logged.
func ResolveSubscription(subscriptionURL string) (string, error) {
	if strings.TrimSpace(subscriptionURL) == "" {
		return "", errors.New("mobile: subscription URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	snapshot, err := mobileSubscriptionClient.Fetch(ctx, strings.TrimSpace(subscriptionURL))
	if err != nil {
		return "", fmt.Errorf("mobile: resolve subscription: %w", err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("mobile: encode subscription: %w", err)
	}
	return string(raw), nil
}

// Listener receives callbacks from background Go goroutines. Android
// implementations should marshal UI work onto the main dispatcher.
type Listener interface {
	OnStatus(status, message string)
	OnLog(level, message string)
	OnMetrics(metricsJSON string)
}

// SocketProtector is implemented by Android with VpnService.protect(fd).
// Returning false aborts that outbound connection to prevent a VPN loop.
type SocketProtector interface {
	Protect(fd int) bool
	// DNSAddress returns a real resolver on Android's underlying network. Go's
	// platform view may otherwise expose only the local netd stub (::1), which
	// is not reachable through a protected raw socket.
	DNSAddress() string
}

type mobileConfig struct {
	Keylink   string   `json:"keylink"`
	Listen    string   `json:"listen"`
	Board     string   `json:"board"`
	APIBase   string   `json:"api_base"`
	HubPage   string   `json:"hub_page"`
	LogLevel  string   `json:"log_level"`
	Bypass    []string `json:"bypass"`
	LocalDNS  bool     `json:"local_dns"`
	EnableUDP bool     `json:"enable_udp"`
	MaxLanes  int      `json:"max_lanes"`
}

// Client is the opaque gomobile-facing lifecycle interface. Create a new
// instance after a completed Stop/AwaitTermination cycle if the Android service
// needs to start again.
type Client interface {
	Start() error
	Stop()
	Reconnect() bool
	AwaitTermination() error
	Status() string
	MetricsJSON() string
}

type client struct {
	core *bproxy.Client

	mu      sync.Mutex
	started bool
	done    chan struct{}
	runErr  error
}

// CreateClient parses a JSON configuration and creates the mobile facade. The
// keylink is sensitive and should come from encrypted Android app storage, not
// Intent extras or logs.
func CreateClient(configJSON string, listener Listener, protector SocketProtector) (Client, error) {
	var cfg mobileConfig
	dec := json.NewDecoder(strings.NewReader(configJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("mobile: invalid config: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("mobile: invalid config: trailing JSON value")
	}
	if cfg.Keylink == "" {
		return nil, errors.New("mobile: keylink is required")
	}

	log := slog.New(&callbackHandler{listener: listener, minLevel: parseLogLevel(cfg.LogLevel)})
	core := bproxy.New(bproxy.Config{
		Keylink:      cfg.Keylink,
		Listen:       cfg.Listen,
		Board:        cfg.Board,
		APIBase:      cfg.APIBase,
		HubPage:      cfg.HubPage,
		LogLevel:     cfg.LogLevel,
		Logger:       log,
		BypassList:   cfg.Bypass,
		LocalDNS:     cfg.LocalDNS,
		Protector:    protector,
		EnableUDP:    cfg.EnableUDP,
		MaxLanes:     cfg.MaxLanes,
		RetryInitial: true,  // VpnService is supervised and may start before connectivity returns.
		SystemProxy:  false, // Android routing is owned by VpnService.
	})
	core.OnStatus(func(status bproxy.Status, err error) {
		if listener == nil {
			return
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		listener.OnStatus(string(status), message)
	})
	core.OnMetrics(func(metrics bproxy.Metrics) {
		if listener == nil {
			return
		}
		if raw, err := json.Marshal(metricsDTO(metrics)); err == nil {
			listener.OnMetrics(string(raw))
		}
	})
	return &client{core: core, done: make(chan struct{})}, nil
}

// Start starts BoardProxy asynchronously and returns immediately. Terminal
// errors are reported through OnStatus and can also be obtained from
// AwaitTermination.
func (c *client) Start() error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("mobile: client already started")
	}
	c.started = true
	c.mu.Unlock()
	go func() {
		err := c.core.Run(backgroundContext())
		c.mu.Lock()
		c.runErr = err
		c.mu.Unlock()
		close(c.done)
	}()
	return nil
}

// Stop requests graceful shutdown and does not block the calling Android
// thread. Call AwaitTermination from a worker coroutine when resource teardown
// must finish.
func (c *client) Stop() { c.core.Stop() }

// Reconnect replaces the current BoardProxy transport without stopping the
// local SOCKS listener. It is safe to call for transient Android network events.
func (c *client) Reconnect() bool { return c.core.Reconnect() }

// AwaitTermination blocks until the Go client has fully stopped.
func (c *client) AwaitTermination() error {
	c.mu.Lock()
	started := c.started
	c.mu.Unlock()
	if !started {
		return errors.New("mobile: client not started")
	}
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runErr
}

// Status returns connecting, connected, reconnecting, stopping, disconnected,
// or error.
func (c *client) Status() string { return string(c.core.Status()) }

// MetricsJSON returns the current metrics snapshot as JSON.
func (c *client) MetricsJSON() string {
	raw, err := json.Marshal(metricsDTO(c.core.Metrics()))
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// SupportsUDP reports support for SOCKS5 UDP ASSOCIATE over mux datagrams.
func SupportsUDP() bool { return true }

// backgroundContext is isolated for tests and keeps context.Context out of the
// generated Java API.
var backgroundContext = newBackgroundContext
