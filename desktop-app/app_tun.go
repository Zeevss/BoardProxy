package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"boardproxy-desktop/internal/elevate"
	"boardproxy-desktop/internal/helperipc"
	"bproxy-core/pkg/bproxy"
)

// helperSession управляет привилегированным helper-процессом. Он запускается
// один раз за сессию GUI (с диалогом повышения прав) и переиспользуется: на
// подключение шлём start, на отключение — stop (процесс остаётся жив), на выход
// из приложения — shutdown. Так диалог прав возникает лишь однажды.
type helperSession struct {
	app     *App
	cmd     *exec.Cmd
	ln      net.Listener
	token   string
	cfgPath string

	ready chan struct{} // закрывается после успешной аутентификации helper'а

	writeMu sync.Mutex

	mu          sync.Mutex
	conn        net.Conn
	status      string
	metrics     MetricsDTO
	active      bool // поднят ли туннель прямо сейчас
	stopping    bool
	startCancel chan struct{} // отменяет start, ожидающий подключения helper'а
	sessionDone chan struct{} // завершение текущего tunnel-подключения
	procDone    chan struct{} // смерть helper-процесса/соединения
	finished    bool
}

// connectTun поднимает туннель через helper: при первом вызове за сессию
// запускает helper с диалогом повышения прав, далее переиспользует его.
func (a *App) connectTun(cfg ConnectConfig) error {
	a.mu.Lock()
	helper := a.helper
	a.mu.Unlock()

	if helper == nil {
		h, err := a.launchHelper()
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.helper = h
		a.mu.Unlock()
		helper = h
	}

	a.mu.Lock()
	a.mode = "tun"
	a.mu.Unlock()

	helper.startTunnel(cfg)
	return nil
}

// launchHelper готовит loopback-сокет, bootstrap с токеном и запускает тот же
// бинарь в режиме `--helper` с повышением прав. Быстрые ошибки возвращаются
// сразу; ожидание аутентификации идёт в фоне.
func (a *App) launchHelper() (*helperSession, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("не удалось определить путь приложения: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть локальный сокет: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	cfgPath, err := helperipc.WriteBootstrapFile(helperipc.Bootstrap{
		EventAddr: ln.Addr().String(),
		Token:     token,
	})
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("не удалось записать bootstrap: %w", err)
	}
	cmd, err := elevate.Launch(exe, "--helper", cfgPath)
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(cfgPath)
		return nil, fmt.Errorf("запуск с повышением прав: %w", err)
	}

	sess := &helperSession{
		app:      a,
		cmd:      cmd,
		ln:       ln,
		token:    token,
		cfgPath:  cfgPath,
		ready:    make(chan struct{}),
		procDone: make(chan struct{}),
		status:   string(bproxy.StatusConnecting),
	}
	go sess.acceptAuth()
	return sess, nil
}

// acceptAuth дожидается подключения helper'а и проверяет токен (первое сообщение
// hello). Провал (таймаут/отказ в правах/чужой процесс) финализирует сессию.
func (s *helperSession) acceptAuth() {
	if l, ok := s.ln.(*net.TCPListener); ok {
		_ = l.SetDeadline(time.Now().Add(helperipc.DialTimeout + 5*time.Second))
	}
	conn, err := s.ln.Accept()
	_ = os.Remove(s.cfgPath)
	if err != nil {
		s.mu.Lock()
		stopping := s.stopping
		s.mu.Unlock()
		if stopping {
			s.app.emit("tunnel:status", map[string]string{"status": string(bproxy.StatusDisconnected)})
			s.finish()
			return
		}
		s.app.emit("tunnel:status", map[string]string{
			"status": string(bproxy.StatusError),
			"error":  "helper не подключился (возможно, отказано в повышении прав)",
		})
		s.setStatus(string(bproxy.StatusError))
		s.finish()
		return
	}
	if l, ok := s.ln.(*net.TCPListener); ok {
		_ = l.SetDeadline(time.Time{})
	}

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := reader.ReadString('\n')
	_ = conn.SetReadDeadline(time.Time{})
	var hello helperipc.Event
	if err != nil || json.Unmarshal([]byte(strings.TrimSpace(line)), &hello) != nil ||
		hello.Type != helperipc.EventHello || hello.Token != s.token {
		_ = conn.Close()
		s.app.emit("tunnel:status", map[string]string{
			"status": string(bproxy.StatusError),
			"error":  "helper не прошёл аутентификацию",
		})
		s.setStatus(string(bproxy.StatusError))
		s.finish()
		return
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	close(s.ready)
	s.readLoop(reader)
}

// startTunnel фиксирует конфиг подключения и (после готовности helper'а) шлёт
// команду start.
func (s *helperSession) startTunnel(cfg ConnectConfig) {
	s.mu.Lock()
	if s.startCancel != nil {
		close(s.startCancel)
	}
	startCancel := make(chan struct{})
	s.startCancel = startCancel
	s.active = true
	s.stopping = false
	s.sessionDone = make(chan struct{})
	s.status = string(bproxy.StatusConnecting)
	s.mu.Unlock()

	go func() {
		select {
		case <-s.ready:
		case <-startCancel:
			return
		case <-s.procDone:
			return
		}
		select {
		case <-startCancel:
			return
		default:
		}
		sc := helperipc.SessionConfig{
			Keylink:  cfg.Link,
			Listen:   cfg.Listen,
			Bypass:   cfg.BypassList,
			MaxLanes: cfg.MaxLanes,
		}
		if err := s.writeCommand(helperipc.Command{Type: helperipc.CmdStart, Config: &sc}); err != nil {
			s.app.emit("tunnel:status", map[string]string{
				"status": string(bproxy.StatusError),
				"error":  "не удалось отправить start helper'у: " + err.Error(),
			})
		}
	}()
}

// stopTunnel останавливает текущий туннель, оставляя helper живым для повторного
// использования. Возвращает канал, закрывающийся по завершении подключения.
func (s *helperSession) stopTunnel() <-chan struct{} {
	s.mu.Lock()
	if !s.active {
		done := s.sessionDone
		s.mu.Unlock()
		if done != nil {
			return done
		}
		return closedChan()
	}
	s.active = false
	s.stopping = true
	if s.startCancel != nil {
		close(s.startCancel)
		s.startCancel = nil
	}
	done := s.sessionDone
	conn := s.conn
	s.mu.Unlock()

	if conn != nil {
		_ = s.writeCommand(helperipc.Command{Type: helperipc.CmdStop})
	} else {
		_ = s.ln.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
	// Сторож: если helper не подтвердит остановку, разблокируем ожидание.
	go func() {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			s.closeSessionDone()
		}
	}()
	return done
}

// shutdown завершает helper-процесс (при выходе из приложения).
func (s *helperSession) shutdown() <-chan struct{} {
	s.mu.Lock()
	procDone := s.procDone
	s.mu.Unlock()

	_ = s.writeCommand(helperipc.Command{Type: helperipc.CmdShutdown})
	go func() {
		select {
		case <-procDone:
			return
		case <-time.After(8 * time.Second):
		}
		if c := s.currentConn(); c != nil {
			_ = c.Close()
		}
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}()
	return procDone
}

func (s *helperSession) readLoop(reader *bufio.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev helperipc.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		s.handleEvent(ev)
	}
	// Сокет закрыт — helper-процесс умер.
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active {
		s.app.emit("tunnel:status", map[string]string{
			"status": string(bproxy.StatusError),
			"error":  "helper завершился",
		})
	}
	s.finish()
}

func (s *helperSession) handleEvent(ev helperipc.Event) {
	switch ev.Type {
	case helperipc.EventStatus:
		s.setStatus(ev.Status)
		s.app.emit("tunnel:status", map[string]string{"status": ev.Status, "error": ev.Error})
		switch ev.Status {
		case string(bproxy.StatusConnected):
			s.app.onConnected()
		case string(bproxy.StatusDisconnected):
			s.closeSessionDone()
			s.app.onTunStopped(s)
		case string(bproxy.StatusError):
			// Ошибка подключения или поднятия TUN завершает эту попытку и
			// инициирует откат controller без дополнительного клика.
			go s.stopTunnel()
		}
	case helperipc.EventLog:
		s.app.emitLog(ev.Level, ev.Msg)
	case helperipc.EventMetrics:
		var dto MetricsDTO
		if err := json.Unmarshal(ev.Metrics, &dto); err != nil {
			return
		}
		s.mu.Lock()
		s.metrics = dto
		s.mu.Unlock()
		s.app.emit("tunnel:metrics", dto)
	}
}

func (s *helperSession) closeSessionDone() {
	s.mu.Lock()
	done := s.sessionDone
	s.sessionDone = nil
	s.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (s *helperSession) finish() {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	conn := s.conn
	s.mu.Unlock()

	// helper умер/остановлен — снимаем системный прокси (если GUI его ставил).
	s.app.markDisconnected()

	_ = s.ln.Close()
	if conn != nil {
		_ = conn.Close()
	}
	s.closeSessionDone()

	waited := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(8 * time.Second):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}

	s.app.mu.Lock()
	if s.app.helper == s {
		s.app.helper = nil
		if s.app.mode == "tun" {
			s.app.mode = ""
		}
	}
	s.app.mu.Unlock()

	close(s.procDone)
}

func (s *helperSession) updateBypass(patterns []string) {
	if s.currentConn() == nil {
		return
	}
	_ = s.writeCommand(helperipc.Command{Type: helperipc.CmdBypass, Bypass: patterns})
}

func (s *helperSession) writeCommand(cmd helperipc.Command) error {
	conn := s.currentConn()
	if conn == nil {
		return errors.New("helper: нет соединения")
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = conn.Write(append(data, '\n'))
	return err
}

func (s *helperSession) currentConn() net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

func (s *helperSession) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *helperSession) currentStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return string(bproxy.StatusDisconnected)
	}
	if s.status == "" {
		return string(bproxy.StatusConnecting)
	}
	return s.status
}

func (s *helperSession) currentMetrics() MetricsDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.metrics
	if m.Status == "" {
		m.Status = s.status
	}
	if m.Streams == nil {
		m.Streams = []StreamDTO{}
	}
	return m
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// loopbackProxyAddr приводит адрес SOCKS к loopback-хосту для записи в системный
// прокси/движок.
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
