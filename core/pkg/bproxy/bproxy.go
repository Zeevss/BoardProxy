// Package bproxy — открытый (embeddable) клиент BoardProxy: SOCKS5/HTTP прокси,
// транспортом которого служит онлайн-доска. Оборачивает внутреннюю сборку
// клиента, добавляя жизненный цикл со статусами и периодические метрики через
// колбэки — по образцу pkg/icmptunnel. Публичная поверхность самодостаточна
// (Config/Status/Metrics), внутренние типы наружу не текут.
package bproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/bypass"
	"bproxy-core/internal/clientcore"
	"bproxy-core/internal/config"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/proxy"
	"bproxy-core/internal/sysproxy"
)

// KeylinkInfo contains safe display metadata extracted from a connection link.
// It deliberately excludes private/public key material.
type KeylinkInfo struct {
	Label  string
	Boards []string
}

// InspectKeylink validates a bproxy:// link and returns metadata suitable for UI.
func InspectKeylink(raw string) (KeylinkInfo, error) {
	credentials, err := keylink.Parse(raw)
	if err != nil {
		return KeylinkInfo{}, err
	}
	return KeylinkInfo{
		Label:  credentials.Label,
		Boards: append([]string(nil), credentials.Boards...),
	}, nil
}

// ErrAlreadyRunning is returned when Run is called concurrently on one Client.
var ErrAlreadyRunning = errors.New("bproxy: client already running")

// ErrTunnelUnavailable is returned by the persistent local proxy while the
// client is between mux sessions. The listener remains bound so a TUN engine
// does not need to restart; a new connection can be retried after reconnect.
var ErrTunnelUnavailable = errors.New("bproxy: tunnel temporarily unavailable")

// SocketProtector is implemented on Android by a small adapter around
// VpnService.protect(fd). It is invoked before each REST/WebSocket/DNS socket.
type SocketProtector interface {
	Protect(fd int) bool
}

// metricsInterval — период снятия метрик и расчёта скоростей.
const metricsInterval = time.Second

const (
	reconnectInitialBackoff = 500 * time.Millisecond
	reconnectMaxBackoff     = 10 * time.Second
)

// Status — состояние клиента.
type Status string

const (
	StatusDisconnected Status = "disconnected"
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	// StatusReconnecting means an established tunnel was lost (including a
	// server GOAWAY) and the client is retrying rendezvous.
	StatusReconnecting Status = "reconnecting"
	// StatusStopping is emitted as soon as local Stop/context cancellation
	// starts, before resources have finished draining.
	StatusStopping Status = "stopping"
	StatusError    Status = "error"
)

// Config конфигурирует клиента.
type Config struct {
	// Keylink — строка подключения bproxy:// (обязательна): приватный ключ
	// клиента, публичный ключ сервера и опционально доски.
	Keylink string
	// Listen — локальный адрес SOCKS5/HTTP прокси. Пусто — 127.0.0.1:1080.
	Listen string
	// Board — хэш доски; переопределяет доску из keylink. Пусто — берётся из
	// keylink.
	Board string
	// APIBase — REST-точка доски. Пусто — дефолт.
	APIBase string
	// HubPage — хэш hub-слайда. Пусто — детерминированный выбор.
	HubPage string
	// LogLevel — debug|info|warn|error. Пусто — info.
	LogLevel string
	// Logger — опциональный slog.Logger; если nil, логи гасятся.
	Logger *slog.Logger
	// BypassList — Go-regexp'ы: цели, чей хост под них попадает, идут напрямую в
	// сеть мимо туннеля. Пусто — весь трафик через доску. Список можно менять на
	// лету (UpdateBypassList).
	BypassList []string
	// LocalDNS — резолвить доменные имена локально и слать в туннель уже IP. По
	// умолчанию (false) имя резолвит сервер (DNS-запросы тоже идут через доску).
	LocalDNS bool
	// SystemProxy — на время работы прописать прокси в системные настройки ОС и
	// восстановить прежние при остановке.
	SystemProxy bool
	// RequireSystemProxy makes a failed OS proxy update fatal. Desktop GUI
	// clients use it so they never report "System Proxy" while only SOCKS is
	// actually running. CLI callers may keep the historical best-effort mode.
	RequireSystemProxy bool
	// Protector keeps BoardProxy's own control-plane sockets outside an OS VPN.
	// It is required for full-tunnel Android VpnService integration.
	Protector SocketProtector
	// EnableUDP enables SOCKS5 UDP ASSOCIATE and the mux datagram transport.
	EnableUDP bool
	// MaxLanes limits adaptive expansion of one logical connection. Zero uses
	// the core default.
	MaxLanes int
}

// StreamInfo — снимок одного проксируемого стрима.
type StreamInfo struct {
	ID        uint32
	Target    string
	Tx        uint64 // байт от приложения к цели (upload)
	Rx        uint64 // байт от цели к приложению (download)
	StartedAt time.Time
}

type LaneMetrics struct {
	ID               uint32
	CongestionWindow int
	Inflight         int
	PeerWindow       int
	EffectiveWindow  int
	TargetPayload    int
	RTT              time.Duration
	BaseRTT          time.Duration
	ConfirmedBytes   uint64
	Draining         bool
}

// Metrics — снимок состояния клиента за один тик.
type Metrics struct {
	Status          Status
	RTT             time.Duration
	Streams         int
	Datagrams       int
	TotalTx         uint64
	TotalRx         uint64
	RateTx          uint64 // байт/с за последний интервал
	RateRx          uint64
	TransportAcked  uint64 // подтверждённые пиром link-payload bytes
	RateConfirmedTx uint64 // подтверждённые байт/с за последний интервал
	BacklogFrames   int
	BacklogBytes    int
	BlockedWriters  int
	Lanes           []LaneMetrics
	Details         []StreamInfo
}

// Client — управляемый экземпляр клиента BoardProxy.
type Client struct {
	cfg Config
	log *slog.Logger

	bypass  *bypass.Matcher
	initErr error // ошибка компиляции стартового bypass-списка (вернётся из Run)

	status atomic.Int32

	cbStatus  func(Status, error)
	cbMetrics func(Metrics)

	mu      sync.Mutex
	sess    *mux.Session // текущая mux-сессия, nil до/после соединения
	stopped bool
	running bool
	cancel  context.CancelFunc

	// Внедряемые точки композиции оставлены приватными: production использует
	// clientcore.Dial/proxy.Serve, тесты жизненного цикла обходятся без сети.
	dial  func(context.Context, config.Config, *slog.Logger) (*mux.Session, error)
	serve func(context.Context, string, proxy.Dialer, *slog.Logger, proxy.Options) error
}

// sessionDialer gives the long-lived local proxy an atomically replaceable mux
// destination. Existing streams remain owned by their original mux session;
// only newly accepted proxy connections use the current value.
type sessionDialer struct {
	mu   sync.RWMutex
	sess *mux.Session
}

func (d *sessionDialer) set(sess *mux.Session) {
	d.mu.Lock()
	d.sess = sess
	d.mu.Unlock()
}

func (d *sessionDialer) clear(sess *mux.Session) {
	d.mu.Lock()
	if d.sess == sess {
		d.sess = nil
	}
	d.mu.Unlock()
}

func (d *sessionDialer) OpenStream(target string) (*mux.Stream, error) {
	d.mu.RLock()
	sess := d.sess
	d.mu.RUnlock()
	if sess == nil {
		return nil, ErrTunnelUnavailable
	}
	return sess.OpenStream(target)
}

func (d *sessionDialer) OpenDatagram() (*mux.Datagram, error) {
	d.mu.RLock()
	sess := d.sess
	d.mu.RUnlock()
	if sess == nil {
		return nil, ErrTunnelUnavailable
	}
	return sess.OpenDatagram()
}

const (
	sDisconnected int32 = iota
	sConnecting
	sConnected
	sReconnecting
	sStopping
	sError
)

var statusNames = [...]Status{
	StatusDisconnected,
	StatusConnecting,
	StatusConnected,
	StatusReconnecting,
	StatusStopping,
	StatusError,
}

// New создаёт клиента по конфигу. Колбэки регистрируются до Run. Ошибка
// компиляции bypass-списка не роняет конструктор (сигнатура без error) — она
// откладывается и возвращается из Run.
func New(cfg Config) *Client {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(discard{}, nil))
	}
	c := &Client{cfg: cfg, log: log, dial: clientcore.Dial, serve: proxy.Serve}
	// bypass.New всегда возвращает ненулевой Matcher (даже при ошибке), поэтому
	// UpdateBypassList/Match безопасны и до успешной инициализации.
	c.bypass, c.initErr = bypass.New(cfg.BypassList)
	return c
}

// UpdateBypassList на лету заменяет список bypass-паттернов у работающего
// клиента — реактивное обновление без переподключения (по образцу ICMP).
// Потокобезопасно; при ошибке компиляции список не меняется.
func (c *Client) UpdateBypassList(patterns []string) error {
	return c.bypass.Update(patterns)
}

// OnStatus регистрирует колбэк смены статуса (вызывается синхронно из Run).
func (c *Client) OnStatus(fn func(Status, error)) { c.cbStatus = fn }

// OnMetrics регистрирует колбэк периодических метрик.
func (c *Client) OnMetrics(fn func(Metrics)) { c.cbMetrics = fn }

// Status возвращает текущий статус.
func (c *Client) Status() Status { return statusNames[c.status.Load()] }

// Metrics возвращает мгновенный снимок метрик (без скоростей — их считает
// периодический цикл в Run).
func (c *Client) Metrics() Metrics {
	m := Metrics{Status: c.Status()}
	c.mu.Lock()
	sess := c.sess
	c.mu.Unlock()
	if sess == nil {
		return m
	}
	fillFromStats(&m, sess.Stats())
	return m
}

// Run подключается и обслуживает прокси, пока ctx не отменён или Stop не вызван.
// После хотя бы одного успешного подключения потеря mux-сессии (в том числе
// GOAWAY при остановке сервера) запускает rendezvous заново с backoff.
// Первичная ошибка подключения по-прежнему возвращается вызывающему сразу: это
// сохраняет явную диагностику неверного keylink/доски при запуске.
func (c *Client) Run(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	if c.running {
		c.mu.Unlock()
		return ErrAlreadyRunning
	}
	c.running = true
	c.cancel = cancel
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
	}()

	// Битый bypass-список — фатально и до подключения: не молча игнорируем.
	if c.initErr != nil {
		c.setStatus(StatusError, c.initErr)
		return c.initErr
	}
	opts := proxy.Options{EnableUDP: c.cfg.EnableUDP, LocalDNS: c.cfg.LocalDNS, Bypass: c.bypass.Match, Protector: c.cfg.Protector}
	route := &sessionDialer{}
	proxyCtx, stopProxy := context.WithCancel(ctx)
	defer stopProxy()
	var proxyDone chan error
	defer func() {
		if proxyDone != nil {
			stopProxy()
			<-proxyDone
		}
	}()
	connectedOnce := false
	systemProxySet := false
	defer func() {
		if systemProxySet {
			if err := sysproxy.Unset(); err != nil {
				c.log.Warn("system proxy not restored", "err", err)
			}
		}
	}()
	backoff := reconnectInitialBackoff
	for {
		if ctx.Err() != nil {
			c.finishStopping()
			return nil
		}
		if connectedOnce {
			if c.Status() != StatusReconnecting {
				c.setStatus(StatusReconnecting, nil)
			}
		} else {
			c.setStatus(StatusConnecting, nil)
		}

		sess, err := c.dial(ctx, c.toInternalConfig(), c.log)
		if err != nil {
			if ctx.Err() != nil {
				c.finishStopping()
				return nil
			}
			if !connectedOnce {
				c.setStatus(StatusError, err)
				return err
			}
			c.setStatus(StatusReconnecting, err)
			if !waitReconnect(ctx, backoff) {
				c.finishStopping()
				return nil
			}
			backoff = min(backoff*2, reconnectMaxBackoff)
			continue
		}

		connectedOnce = true
		backoff = reconnectInitialBackoff
		c.mu.Lock()
		c.sess = sess
		c.mu.Unlock()
		route.set(sess)
		c.setStatus(StatusConnected, nil)
		if proxyDone == nil {
			proxyDone = make(chan error, 1)
			go func() {
				proxyDone <- c.serve(proxyCtx, c.cfg.listenOrDefault(), route, c.log, opts)
			}()
		}
		// Включаем системный proxy только после успешного rendezvous. Иначе он
		// указывал бы на ещё не слушающий локальный порт и мог ломать самому
		// клиенту доступ к доске во время первоначального подключения.
		if c.cfg.SystemProxy && !systemProxySet {
			paddr := loopbackAddr(c.cfg.listenOrDefault())
			if err := sysproxy.Set(paddr); err != nil {
				c.log.Warn("system proxy not set", "err", err)
				if c.cfg.RequireSystemProxy {
					err = fmt.Errorf("enable system proxy: %w", err)
					c.setStatus(StatusError, err)
					return err
				}
			} else {
				systemProxySet = true
				c.log.Info("system proxy enabled", "proxy", paddr)
			}
		}

		metricsCtx, stopMetrics := context.WithCancel(ctx)
		metricsDone := make(chan struct{})
		go func() {
			defer close(metricsDone)
			c.metricsLoop(metricsCtx, sess)
		}()

		var proxyErr error
		proxyStopped := false
		peerGone := false
		select {
		case <-ctx.Done():
		case <-sess.Done():
			peerGone = true
		case proxyErr = <-proxyDone:
			proxyStopped = true
			proxyDone = nil // the single frontend result has been consumed
		}
		closeReason := sess.Err()
		stopMetrics()
		<-metricsDone

		c.mu.Lock()
		if c.sess == sess {
			c.sess = nil
		}
		c.mu.Unlock()
		route.clear(sess)
		_ = sess.Close()

		if ctx.Err() != nil {
			c.finishStopping()
			return nil
		}
		if proxyStopped && !errors.Is(proxyErr, context.Canceled) {
			if proxyErr == nil {
				proxyErr = errors.New("bproxy: local proxy stopped unexpectedly")
			}
			c.setStatus(StatusError, proxyErr)
			return proxyErr
		}
		if peerGone {
			if errors.Is(closeReason, mux.ErrPeerGoAway) {
				c.log.Info("server graceful shutdown started; reconnecting")
				c.setStatus(StatusReconnecting, nil)
			} else {
				c.log.Warn("tunnel session lost; reconnecting", "err", closeReason)
				c.setStatus(StatusReconnecting, closeReason)
			}
		}
		// Remote GOAWAY/link failure: следующая итерация сообщает
		// Reconnecting и выполняет новый rendezvous.
	}
}

// Stop инициирует остановку клиента.
func (c *Client) Stop() {
	c.mu.Lock()
	c.stopped = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		c.beginStopping()
		cancel()
	}
}

func (c *Client) finishStopping() {
	c.beginStopping()
	c.setStatus(StatusDisconnected, nil)
}

func (c *Client) beginStopping() {
	if c.Status() == StatusStopping {
		return
	}
	c.log.Info("client graceful shutdown started")
	c.setStatus(StatusStopping, nil)
}

func waitReconnect(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (c *Client) setStatus(s Status, err error) {
	var idx int32
	switch s {
	case StatusConnecting:
		idx = sConnecting
	case StatusConnected:
		idx = sConnected
	case StatusReconnecting:
		idx = sReconnecting
	case StatusStopping:
		idx = sStopping
	case StatusError:
		idx = sError
	default:
		idx = sDisconnected
	}
	c.status.Store(idx)
	if c.cbStatus != nil {
		c.cbStatus(s, err)
	}
}

func (c *Client) metricsLoop(ctx context.Context, sess *mux.Session) {
	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()
	var prevTx, prevRx, prevConfirmed uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.cbMetrics == nil {
				continue
			}
			m := Metrics{Status: c.Status()}
			fillFromStats(&m, sess.Stats())
			// Скорости — прирост за интервал (интервал = 1с, поэтому байт/с).
			m.RateTx = m.TotalTx - prevTx
			m.RateRx = m.TotalRx - prevRx
			m.RateConfirmedTx = m.TransportAcked - prevConfirmed
			prevTx, prevRx, prevConfirmed = m.TotalTx, m.TotalRx, m.TransportAcked
			c.cbMetrics(m)
		}
	}
}

func fillFromStats(m *Metrics, st mux.SessionStats) {
	m.RTT = st.RTT
	m.Streams = st.Streams
	m.Datagrams = st.Datagrams
	m.TotalTx = st.Written
	m.TotalRx = st.Received
	m.TransportAcked = st.TransportAcked
	m.BacklogFrames = st.BacklogFrames
	m.BacklogBytes = st.BacklogBytes
	m.BlockedWriters = st.BlockedWriters
	m.Lanes = make([]LaneMetrics, 0, len(st.Lanes))
	for _, lane := range st.Lanes {
		m.Lanes = append(m.Lanes, LaneMetrics{
			ID:               lane.ID,
			CongestionWindow: lane.CongestionWindow,
			Inflight:         lane.Inflight,
			PeerWindow:       lane.PeerWindow,
			EffectiveWindow:  lane.EffectiveWindow,
			TargetPayload:    lane.TargetPayload,
			RTT:              lane.RTT,
			BaseRTT:          lane.BaseRTT,
			ConfirmedBytes:   lane.ConfirmedBytes,
			Draining:         lane.Draining,
		})
	}
	m.Details = make([]StreamInfo, 0, len(st.PerStream))
	for _, s := range st.PerStream {
		m.Details = append(m.Details, StreamInfo{
			ID:        s.ID,
			Target:    s.Target,
			Tx:        s.Written,
			Rx:        s.Received,
			StartedAt: s.StartedAt,
		})
	}
}

func (c *Config) listenOrDefault() string {
	if c.Listen != "" {
		return c.Listen
	}
	return "127.0.0.1:1080"
}

func (c *Client) toInternalConfig() config.Config {
	cfg := config.Default()
	if c.cfg.APIBase != "" {
		cfg.Board.APIBase = c.cfg.APIBase
	}
	if c.cfg.LogLevel != "" {
		cfg.LogLevel = c.cfg.LogLevel
	}
	cfg.Board.Hash = c.cfg.Board
	cfg.Client.Keylink = c.cfg.Keylink
	cfg.Client.Listen = c.cfg.listenOrDefault()
	cfg.Client.Protector = c.cfg.Protector
	if c.cfg.MaxLanes > 0 {
		cfg.Client.MaxLanes = c.cfg.MaxLanes
	}
	cfg.Server.HubPage = c.cfg.HubPage
	return cfg
}

// loopbackAddr приводит адрес прослушивания к loopback-хосту для записи в
// системный прокси: слушать можно на 0.0.0.0, но система должна ходить на
// 127.0.0.1.
func loopbackAddr(bind string) string {
	_, port, err := net.SplitHostPort(bind)
	if err != nil {
		return bind
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// discard — io.Writer в никуда, для дефолтного молчаливого логгера.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
