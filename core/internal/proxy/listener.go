// Пакет proxy — клиентский фронт: локальный смешанный SOCKS5/HTTP прокси. Каждое
// принятое соединение он определяет по первому байту (0x05 — SOCKS5, иначе HTTP),
// выясняет целевой адрес и открывает под него mux-стрим через доску. Опционально
// резолвит DNS локально (Options.LocalDNS) и пускает часть адресов мимо туннеля
// напрямую (Options.Bypass).
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"bproxy-core/internal/mux"
	"bproxy-core/internal/netprotect"
	"bproxy-core/internal/relay"
)

// shutdownDrainTimeout bounds how long serve waits for already-accepted
// connections to finish before returning. A connection stuck on a peer that
// never sends anything and never closes (no cancellation path of its own)
// must not hang shutdown forever.
const shutdownDrainTimeout = 5 * time.Second

// tcpKeepAlivePeriod — период keepalive-проб на локальном TCP-сокете, чтобы
// быстрее заметить мёртвого локального клиента (например уснувший ноутбук),
// отдельно от per-stream idle-детекции на уровне mux (та ловит "тихо, но
// живо", а не "не отвечает").
const tcpKeepAlivePeriod = 60 * time.Second

// bypassDialTimeout ограничивает прямой (мимо туннеля) dial до bypass-цели.
const bypassDialTimeout = 10 * time.Second

// dnsTimeout ограничивает локальный DNS-резолв при Options.LocalDNS.
const dnsTimeout = 5 * time.Second

// proxyHandshakeTimeout bounds clients that connect but never finish the
// SOCKS/HTTP greeting. It is cleared before payload relay begins.
const proxyHandshakeTimeout = 15 * time.Second

// Dialer открывает стрим до целевого адреса (реализуется *mux.Session).
type Dialer interface {
	OpenStream(target string) (*mux.Stream, error)
}

// DatagramDialer is the optional UDP capability implemented by mux.Session.
// Keeping it separate preserves TCP-only test doubles and produces an explicit
// SOCKS error when UDP is disabled by client configuration.
type DatagramDialer interface {
	OpenDatagram() (*mux.Datagram, error)
}

// lifecycleDialer is implemented by mux.Session. Keeping it separate from
// Dialer preserves small test doubles while allowing Serve to stop accepting
// local connections as soon as the tunnel session receives GOAWAY or dies.
type lifecycleDialer interface {
	Done() <-chan struct{}
}

// Options настраивает маршрутизацию целей.
type Options struct {
	// EnableUDP permits SOCKS5 UDP ASSOCIATE. Disabled clients receive an
	// explicit command-not-supported response instead of a silent black hole.
	EnableUDP bool
	// LocalDNS: если true — клиент резолвит доменное имя цели локально и шлёт в
	// туннель уже IP. По умолчанию (false) имя уходит в туннель как есть, и его
	// резолвит сервер (egress) — так DNS-запросы тоже идут через доску.
	LocalDNS bool
	// Bypass, если задан, решает по хосту цели, пустить ли её напрямую в сеть
	// мимо туннеля (true — напрямую). nil — bypass выключен.
	Bypass func(host string) bool
	// Protector excludes direct bypass and local-DNS sockets from an OS VPN.
	// Android supplies VpnService.protect(fd); nil keeps desktop behavior.
	Protector netprotect.Protector
}

// targetConn — соединение до цели: mux-стрим через туннель либо прямой TCP при
// bypass. Обе реализации удовлетворяют relay.Stream и io.Closer.
type targetConn interface {
	relay.Stream
	io.Closer
}

// router выбирает путь до цели (туннель или прямой dial) и держит настройки
// резолва/bypass, общие для всех соединений одного Serve.
type router struct {
	d    Dialer
	opts Options
	log  *slog.Logger
}

// dial открывает соединение до target ("host:port"): при совпадении bypass —
// прямой TCP мимо туннеля; иначе mux-стрим через доску, с опциональным локальным
// резолвом имени в IP (LocalDNS).
func (r *router) dial(target string) (targetConn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		host, port = target, ""
	}
	// Bypass проверяем по исходному имени хоста — до локального резолва.
	if r.opts.Bypass != nil && r.opts.Bypass(host) {
		r.log.Debug("bypass: direct dial", "target", target)
		dialer := netprotect.Dialer(r.opts.Protector)
		dialer.Timeout = bypassDialTimeout
		c, err := dialer.Dial("tcp", target)
		if err != nil {
			return nil, err
		}
		tc, ok := c.(*net.TCPConn)
		if !ok {
			_ = c.Close()
			return nil, fmt.Errorf("proxy: unexpected conn type %T", c)
		}
		return tc, nil
	}
	dialTarget := target
	if r.opts.LocalDNS && port != "" && net.ParseIP(host) == nil {
		ip, err := resolveHost(host, r.opts.Protector)
		if err != nil {
			return nil, err
		}
		dialTarget = net.JoinHostPort(ip, port)
		r.log.Debug("local dns", "host", host, "ip", ip)
	}
	return r.d.OpenStream(dialTarget)
}

func (r *router) openDatagram() (*mux.Datagram, error) {
	if !r.opts.EnableUDP {
		return nil, errUnsupported
	}
	d, ok := r.d.(DatagramDialer)
	if !ok {
		return nil, errUnsupported
	}
	return d.OpenDatagram()
}

// resolveHost резолвит имя локально, предпочитая IPv4.
func resolveHost(host string, protector netprotect.Protector) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	resolver := net.DefaultResolver
	if protector != nil {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return netprotect.Dialer(protector).DialContext(ctx, network, address)
			},
		}
	}
	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("proxy: no addresses for %q", host)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return ips[0].String(), nil
}

// Serve запускает локальный прокси на addr, обслуживая соединения через d, пока
// ctx не отменён. opts задаёт локальный DNS и bypass (нулевое значение — прежнее
// поведение: имя резолвит сервер, bypass выключен).
func Serve(ctx context.Context, addr string, d Dialer, log *slog.Logger, opts Options) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if lifecycle, ok := d.(lifecycleDialer); ok {
		go func() {
			select {
			case <-lifecycle.Done():
				cancel()
			case <-serveCtx.Done():
			}
		}()
	}
	lc := net.ListenConfig{}
	ln, err := lc.Listen(serveCtx, "tcp", addr)
	if err != nil {
		return err
	}
	log.Info("proxy listening", "addr", addr, "local_dns", opts.LocalDNS, "bypass", opts.Bypass != nil)
	return serve(serveCtx, ln, &router{d: d, opts: opts, log: log}, log)
}

// serve обслуживает соединения на готовом listener'е. Не возвращается, пока не
// завершатся все уже принятые соединения — иначе процесс может выйти, пока
// handleConn ещё дописывает/закрывает локальный TCP-сокет.
func serve(ctx context.Context, ln net.Listener, r *router, log *slog.Logger) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	var wg sync.WaitGroup
	defer drainWithTimeout(&wg, shutdownDrainTimeout)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(tcpKeepAlivePeriod)
		}
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			// Закрываем уже принятый локальный сокет при shutdown/reconnect. Без
			// этого клиент, который не прислал даже первый байт, навсегда зависал в
			// Peek, а Serve лишь переставал ждать его через timeout, оставляя утечку.
			stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stopClose()
			handleConn(conn, r, log)
		}(conn)
	}
}

// drainWithTimeout waits for wg, giving up after d so a goroutine stuck on an
// unresponsive peer cannot hang shutdown indefinitely.
func drainWithTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func handleConn(conn net.Conn, r *router, log *slog.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(proxyHandshakeTimeout))
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	if first[0] == socks5Version {
		serveSOCKS5(conn, br, r, log)
	} else {
		serveHTTP(conn, br, r, log)
	}
}

func clearConnDeadline(conn net.Conn) {
	_ = conn.SetDeadline(time.Time{})
}

// bufConn представляет клиентское соединение как relay.Stream: чтение идёт из
// буфера (там могут лежать уже прочитанные байты), запись и half-close — в сокет.
type bufConn struct {
	r *bufio.Reader
	c net.Conn
}

func (b bufConn) Read(p []byte) (int, error)  { return b.r.Read(p) }
func (b bufConn) Write(p []byte) (int, error) { return b.c.Write(p) }

func (b bufConn) CloseWrite() error {
	if t, ok := b.c.(*net.TCPConn); ok {
		return t.CloseWrite()
	}
	return nil
}

// errUnsupported — общая ошибка для неподдерживаемых команд прокси.
var errUnsupported = errors.New("proxy: unsupported command")
