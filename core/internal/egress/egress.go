// Пакет egress — «выходной узел» сервера: точка, где трафик покидает доску и
// уходит в реальный интернет. Он принимает mux-стримы от подключившихся клиентов,
// дозванивается до целевого адреса из SYN и качает байты в обе стороны.
package egress

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"bproxy-core/internal/hub"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/relay"
)

// dialTimeout ограничивает установку соединения с целевым хостом.
const dialTimeout = 30 * time.Second

// shutdownDrainTimeout ограничивает, сколько Serve/serveSession ждут уже
// принятые сессии/стримы перед возвратом — стрим, застрявший на молчащем
// удалённом пире без своего пути отмены, не должен вешать shutdown навсегда.
const shutdownDrainTimeout = 5 * time.Second

// tcpKeepAlivePeriod — период keepalive-проб на соединении с целевым хостом,
// чтобы быстрее заметить молчащего мёртвого удалённого пира, отдельно от
// per-stream idle-детекции на уровне mux (та ловит "тихо, но живо", а не "не
// отвечает").
const tcpKeepAlivePeriod = 60 * time.Second

const udpBufferSize = 65535

const maxUDPRemotes = 1024

type Options struct {
	// AllowPrivate permits RFC1918 and IPv6 ULA targets. Loopback, link-local,
	// multicast and unspecified addresses are always denied.
	AllowPrivate         bool
	allowLoopbackForTest bool
}

// Serve принимает клиентские сессии от хаба и обслуживает их стримы, пока ctx не
// отменён или сервер не закрыт. Не возвращается, пока не завершатся все уже
// принятые сессии.
func Serve(ctx context.Context, srv *hub.Server, log *slog.Logger, opts Options) error {
	var wg sync.WaitGroup
	defer drainWithTimeout(&wg, shutdownDrainTimeout)
	for {
		m, err := srv.Accept(ctx)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			serveSession(ctx, m, log, opts)
		}()
	}
}

func serveSession(ctx context.Context, m *mux.Session, log *slog.Logger, opts Options) {
	defer m.Close()
	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	go func() {
		select {
		case <-m.Done():
			cancelSession()
		case <-sessionCtx.Done():
		}
	}()
	var handlers sync.WaitGroup
	var acceptors sync.WaitGroup
	acceptors.Add(2)
	go func() {
		defer acceptors.Done()
		for {
			st, err := m.AcceptStream(sessionCtx)
			if err != nil {
				return
			}
			handlers.Add(1)
			go func(st *mux.Stream) {
				defer handlers.Done()
				serveStream(sessionCtx, st, log, opts)
			}(st)
		}
	}()
	go func() {
		defer acceptors.Done()
		for {
			d, err := m.AcceptDatagram(sessionCtx)
			if err != nil {
				return
			}
			handlers.Add(1)
			go func(d *mux.Datagram) {
				defer handlers.Done()
				serveDatagram(sessionCtx, d, log, opts)
			}(d)
		}
	}()
	<-sessionCtx.Done()
	acceptors.Wait()
	drainWithTimeout(&handlers, shutdownDrainTimeout)
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

func serveStream(ctx context.Context, st *mux.Stream, log *slog.Logger, opts Options) {
	target := st.Target()
	resolved, err := resolveTCPAddr(ctx, target, opts)
	if err != nil {
		log.Warn("egress target rejected", "target", target, "err", err)
		_ = st.Reset()
		return
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", resolved)
	if err != nil {
		log.Debug("egress dial failed", "target", target, "err", err)
		_ = st.Reset()
		return
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	log.Debug("egress connected", "target", target)

	tc := conn.(*net.TCPConn)
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(tcpKeepAlivePeriod)

	relay.Pipe(st, tc)
	_ = st.Close()
}

func serveDatagram(ctx context.Context, d *mux.Datagram, log *slog.Logger, opts Options) {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		log.Debug("egress udp socket failed", "err", err)
		_ = d.Close()
		return
	}
	defer conn.Close()
	defer d.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()

	readDone := make(chan struct{})
	var remotesMu sync.RWMutex
	remotes := make(map[string]struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, udpBufferSize)
		for {
			n, source, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			remotesMu.RLock()
			_, allowed := remotes[source.String()]
			remotesMu.RUnlock()
			if !allowed {
				continue
			}
			if err := d.Send(source.String(), buf[:n]); err != nil {
				return
			}
		}
	}()

	for {
		packet, err := d.Receive(ctx)
		if err != nil {
			break
		}
		target, err := resolveUDPAddr(ctx, packet.Target, opts)
		if err != nil {
			log.Debug("egress udp resolve failed", "target", packet.Target, "err", err)
			continue
		}
		remotesMu.Lock()
		if _, exists := remotes[target.String()]; !exists && len(remotes) >= maxUDPRemotes {
			remotesMu.Unlock()
			log.Debug("egress udp remote limit reached", "target", packet.Target)
			continue
		}
		remotes[target.String()] = struct{}{}
		remotesMu.Unlock()
		if _, err := conn.WriteToUDP(packet.Payload, target); err != nil {
			log.Debug("egress udp write failed", "target", packet.Target, "err", err)
			break
		}
	}
	_ = conn.Close()
	<-readDone
}

func resolveUDPAddr(ctx context.Context, target string, opts Options) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	portNumber, err := net.LookupPort("udp", port)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if !egressIPAllowed(ip, opts) {
			return nil, fmt.Errorf("egress: destination address is not allowed")
		}
		return &net.UDPAddr{IP: ip, Port: portNumber}, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		parsed := net.IP(ip.AsSlice())
		if egressIPAllowed(parsed, opts) {
			return &net.UDPAddr{IP: parsed, Port: portNumber}, nil
		}
	}
	return nil, fmt.Errorf("egress: target %q resolved only to disallowed addresses", host)
}

func resolveTCPAddr(ctx context.Context, target string, opts Options) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", fmt.Errorf("egress: invalid target: %w", err)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !egressIPAllowed(ip, opts) {
			return "", fmt.Errorf("egress: destination address is not allowed")
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		parsed := net.IP(ip.AsSlice())
		if egressIPAllowed(parsed, opts) {
			return net.JoinHostPort(parsed.String(), port), nil
		}
	}
	return "", fmt.Errorf("egress: target %q resolved only to disallowed addresses", host)
}

func egressIPAllowed(ip net.IP, opts Options) bool {
	if ip != nil && ip.IsLoopback() && opts.allowLoopbackForTest {
		return true
	}
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return opts.AllowPrivate || !ip.IsPrivate()
}
