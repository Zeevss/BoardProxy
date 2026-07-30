// Пакет helper — привилегированный демон TUN, встроенный в тот же бинарь, что и
// GUI. Запускается как `<self> --helper <bootstrap>` с повышением прав
// (pkexec/osascript/UAC): GUI и helper — один исполняемый файл, второй бинарь
// рядом не нужен. Демон подключается обратно к GUI по loopback-сокету,
// аутентифицируется токеном и живёт, принимая команды start/stop/shutdown/bypass.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"boardproxy-desktop/internal/dnsproxy"
	"boardproxy-desktop/internal/helperipc"
	"boardproxy-desktop/internal/tun"
	"boardproxy-desktop/internal/wintundll"
	"bproxy-core/pkg/bproxy"
)

// dnsUpstream — вышестоящий резолвер, куда локальный DNS-форвардер шлёт запросы
// (уходят через туннель по маршруту по умолчанию).
const dnsUpstream = "1.1.1.1:53"

// Run запускает helper-демон по bootstrap-файлу (адрес обратного подключения и
// токен). Возвращается при закрытии сокета GUI, команде shutdown или сигнале.
func Run(bootstrapPath string) error {
	boot, err := helperipc.ReadBootstrapFile(bootstrapPath)
	if err != nil {
		return fmt.Errorf("read bootstrap: %w", err)
	}
	conn, err := net.DialTimeout("tcp", boot.EventAddr, helperipc.DialTimeout)
	if err != nil {
		return fmt.Errorf("dial GUI: %w", err)
	}
	defer conn.Close()

	d := &daemon{sender: &sender{conn: conn}}
	d.sender.send(helperipc.Event{Type: helperipc.EventHello, Token: boot.Token})
	d.serve(conn)
	return nil
}

// daemon держит текущую сессию туннеля (если есть) и обслуживает команды GUI.
type daemon struct {
	sender *sender

	mu   sync.Mutex
	sess *session
}

func (d *daemon) serve(conn net.Conn) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	defer d.stopSession() // гарантированный откат при выходе

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var cmd helperipc.Command
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			continue
		}
		switch cmd.Type {
		case helperipc.CmdStart:
			if cmd.Config != nil {
				d.startSession(*cmd.Config)
			}
		case helperipc.CmdStop:
			d.stopSession()
		case helperipc.CmdBypass:
			d.updateBypass(cmd.Bypass)
		case helperipc.CmdShutdown:
			d.stopSession()
			return
		}
	}
}

// session — одно активное подключение (клиент + TUN + DNS-форвардер).
type session struct {
	controller *tun.Controller
	client     *bproxy.Client
	cancel     context.CancelFunc
	done       chan struct{}

	mu  sync.Mutex
	dns *dnsproxy.Proxy
}

func (s *session) resolver() helperipc.NameResolver {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dns == nil {
		return nil
	}
	return s.dns
}

func (d *daemon) startSession(cfg helperipc.SessionConfig) {
	d.stopSession() // одна сессия за раз

	logger := slog.New(&eventLogHandler{s: d.sender})
	controller, err := tun.New()
	if err != nil {
		d.sender.send(helperipc.Event{Type: helperipc.EventStatus, Status: string(bproxy.StatusError), Error: err.Error()})
		return
	}

	tunAddr := cfg.TunAddr
	if tunAddr == "" {
		tunAddr = tun.DefaultTunAddr
	}

	client := bproxy.New(bproxy.Config{
		Keylink:    cfg.Keylink,
		Listen:     cfg.Listen,
		LogLevel:   "info",
		Logger:     logger,
		BypassList: cfg.Bypass,
		MaxLanes:   cfg.MaxLanes,
		EnableUDP:  true,
		Protector:  controller.Protector(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	sess := &session{controller: controller, client: client, cancel: cancel, done: make(chan struct{})}

	var tunOnce sync.Once
	client.OnStatus(func(status bproxy.Status, statusErr error) {
		msg := ""
		if statusErr != nil {
			msg = statusErr.Error()
		}
		d.sender.send(helperipc.Event{Type: helperipc.EventStatus, Status: string(status), Error: msg})
		if status == bproxy.StatusConnected {
			tunOnce.Do(func() {
				go raiseTunnel(sess, cfg, tunAddr, logger, d.sender, cancel)
			})
		}
	})
	client.OnMetrics(func(m bproxy.Metrics) {
		d.sender.send(helperipc.Event{Type: helperipc.EventMetrics, Metrics: helperipc.MetricsJSON(m, sess.resolver())})
	})

	go func() {
		_ = client.Run(ctx)
		sess.teardown()
		close(sess.done)
	}()

	d.mu.Lock()
	d.sess = sess
	d.mu.Unlock()
}

// raiseTunnel поднимает TUN и локальный DNS-форвардер после подключения к доске.
func raiseTunnel(sess *session, cfg helperipc.SessionConfig, tunAddr string, logger *slog.Logger, s *sender, cancel context.CancelFunc) {
	logger.Info("поднятие TUN…")
	// На Windows tun2socks грузит wintun.dll из каталога exe — распаковываем её
	// рядом с бинарём (встроена в exe; no-op на других ОС).
	if err := wintundll.Ensure(); err != nil {
		logger.Error("не удалось подготовить wintun.dll", "err", err)
		s.send(helperipc.Event{Type: helperipc.EventStatus, Status: string(bproxy.StatusError), Error: "wintun: " + err.Error()})
		cancel()
		return
	}
	// Система резолвит через локальный форвардер на адресе TUN: он пишет IP→домен
	// для статистики и уводит DNS в туннель.
	if err := sess.controller.Start(tun.Params{
		ProxyAddr: loopbackProxyAddr(cfg.Listen),
		TunAddr:   tunAddr,
		Gateway:   cfg.Gateway,
		MTU:       cfg.MTU,
		DNS:       tunAddr,
	}); err != nil {
		logger.Error("не удалось поднять TUN", "err", err)
		s.send(helperipc.Event{Type: helperipc.EventStatus, Status: string(bproxy.StatusError), Error: "TUN: " + err.Error()})
		cancel()
		return
	}
	// Локальный резолвер поднимаем ДО того, как пропишем его системе: иначе между
	// сменой настроек DNS и стартом форвардера система остаётся без резолва.
	// Прописать резолвер нужно в любом случае: прежний системный DNS обычно
	// указывает на локальный роутер, недостижимый через туннель.
	resolver := tunAddr
	dns, err := dnsproxy.Start(net.JoinHostPort(tunAddr, "53"), dnsUpstream)
	if err != nil {
		resolver = tun.FallbackDNS()
		logger.Warn("локальный DNS-форвардер не запущен, используем публичный резолвер",
			"resolver", resolver, "err", err)
	} else {
		sess.mu.Lock()
		sess.dns = dns
		sess.mu.Unlock()
	}
	if err := sess.controller.ApplyDNS(resolver); err != nil {
		logger.Warn("системный DNS не изменён", "err", err)
	} else {
		logger.Info("системный DNS переключён", "resolver", resolver)
	}
	logger.Info("TUN активен: весь трафик идёт через доску")
}

func (s *session) teardown() {
	if s.controller != nil {
		_ = s.controller.Stop()
	}
	s.mu.Lock()
	dns := s.dns
	s.mu.Unlock()
	dns.Stop()
}

func (d *daemon) stopSession() {
	d.mu.Lock()
	sess := d.sess
	d.sess = nil
	d.mu.Unlock()
	if sess == nil {
		return
	}
	sess.cancel()
	sess.client.Stop()
	<-sess.done
}

func (d *daemon) updateBypass(patterns []string) {
	d.mu.Lock()
	sess := d.sess
	d.mu.Unlock()
	if sess != nil {
		_ = sess.client.UpdateBypassList(patterns)
	}
}

// sender потокобезопасно пишет события в сокет GUI.
type sender struct {
	mu   sync.Mutex
	conn net.Conn
}

func (s *sender) send(ev helperipc.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.conn.Write(append(data, '\n'))
}

// eventLogHandler превращает записи slog в события лога для GUI.
type eventLogHandler struct {
	s     *sender
	attrs []slog.Attr
}

func (h *eventLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *eventLogHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		appendAttr(&b, a)
	}
	r.Attrs(func(a slog.Attr) bool { appendAttr(&b, a); return true })
	h.s.send(helperipc.Event{Type: helperipc.EventLog, Level: r.Level.String(), Msg: b.String()})
	return nil
}

func (h *eventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *eventLogHandler) WithGroup(string) slog.Handler { return h }

func appendAttr(b *strings.Builder, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	b.WriteByte(' ')
	b.WriteString(a.Key)
	b.WriteByte('=')
	if a.Value.Kind() == slog.KindString {
		b.WriteString(strconv.Quote(a.Value.String()))
		return
	}
	b.WriteString(fmt.Sprint(a.Value.Any()))
}

// loopbackProxyAddr приводит адрес SOCKS к loopback: движок tun2socks должен
// ходить на 127.0.0.1, даже если прокси слушает на 0.0.0.0.
func loopbackProxyAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "127.0.0.1:1080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
