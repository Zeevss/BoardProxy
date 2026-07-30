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

// Serve принимает клиентские сессии от хаба и обслуживает их стримы, пока ctx не
// отменён или сервер не закрыт. Не возвращается, пока не завершатся все уже
// принятые сессии.
func Serve(ctx context.Context, srv *hub.Server, log *slog.Logger) error {
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
			serveSession(ctx, m, log)
		}()
	}
}

func serveSession(ctx context.Context, m *mux.Session, log *slog.Logger) {
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
				serveStream(sessionCtx, st, log)
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
				serveDatagram(sessionCtx, d, log)
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

func serveStream(ctx context.Context, st *mux.Stream, log *slog.Logger) {
	target := st.Target()
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
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

func serveDatagram(ctx context.Context, d *mux.Datagram, log *slog.Logger) {
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
	go func() {
		defer close(readDone)
		buf := make([]byte, udpBufferSize)
		for {
			n, source, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
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
		target, err := resolveUDPAddr(ctx, packet.Target)
		if err != nil {
			log.Debug("egress udp resolve failed", "target", packet.Target, "err", err)
			continue
		}
		if _, err := conn.WriteToUDP(packet.Payload, target); err != nil {
			log.Debug("egress udp write failed", "target", packet.Target, "err", err)
			break
		}
	}
	_ = conn.Close()
	<-readDone
}

func resolveUDPAddr(ctx context.Context, target string) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	portNumber, err := net.LookupPort("udp", port)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: portNumber}, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("egress: no UDP addresses for %q", host)
	}
	return &net.UDPAddr{IP: net.IP(ips[0].AsSlice()), Port: portNumber}, nil
}
