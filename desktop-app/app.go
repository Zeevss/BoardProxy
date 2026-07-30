package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"bproxy-core/pkg/bproxy"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the thin Wails adapter around one BoardProxy client.
type App struct {
	ctx context.Context

	mu     sync.Mutex
	mode   string         // "" | "proxy" | "tun" — активный режим подключения
	client *bproxy.Client // in-process клиент (режим proxy)
	cancel context.CancelFunc
	done   chan struct{}
	// helper — привилегированный процесс для TUN. Живёт с первого TUN-подключения
	// до выхода из приложения (переиспользуется между подключениями), поэтому
	// может быть непустым и в простое.
	helper *helperSession

	// Системный прокси управляется здесь (в обоих режимах), чтобы его можно было
	// включать/выключать «на лету» без переподключения.
	listen       string
	wantSysProxy bool
	sysProxyOn   bool
	connected    bool

	tray trayUpdater // меню трея (nil в bindings-сборке)
}

// TrayProfile/TrayState — снимок для меню трея, присылается фронтендом.
type TrayProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TrayState struct {
	Status   string        `json:"status"`
	Profiles []TrayProfile `json:"profiles"`
	ActiveID string        `json:"activeId"`
}

// trayUpdater реализуется треем (tray.go). Интерфейс объявлен здесь, чтобы app.go
// компилировался и в bindings-сборке, где systray исключён.
type trayUpdater interface{ update(TrayState) }

// SyncTray обновляет меню трея (вызывается фронтендом при смене статуса/профилей).
func (a *App) SyncTray(state TrayState) {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	if tray != nil {
		tray.update(state)
	}
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

func (a *App) runtimeContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

// showWindow выводит главное окно на передний план (из трея / при повторном
// запуске бинаря).
func (a *App) showWindow() {
	if ctx := a.runtimeContext(); ctx != nil {
		wruntime.WindowShow(ctx)
		wruntime.WindowUnminimise(ctx)
	}
}

func (a *App) shutdown(context.Context) {
	// Останавливаем активное подключение…
	done := a.stopActive()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	// …и полностью гасим helper-процесс (он живёт всю сессию GUI).
	a.mu.Lock()
	helper := a.helper
	a.helper = nil
	a.mu.Unlock()
	if helper != nil {
		select {
		case <-helper.shutdown():
		case <-time.After(5 * time.Second):
		}
	}
}

type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name:    "BoardProxy",
		Version: "0.1.0",
		OS:      stdruntime.GOOS,
		Arch:    stdruntime.GOARCH,
	}
}

type LinkInfo struct {
	Label  string   `json:"label"`
	Boards []string `json:"boards"`
}

func (a *App) ParseLink(link string) (LinkInfo, error) {
	info, err := bproxy.InspectKeylink(link)
	if err != nil {
		return LinkInfo{}, err
	}
	return LinkInfo{Label: info.Label, Boards: info.Boards}, nil
}

type ConnectConfig struct {
	Link       string   `json:"link"`
	Listen     string   `json:"listen"`
	BypassList []string `json:"bypassList"`
	MaxLanes   int      `json:"maxLanes"`
	// SystemProxy — прописать локальный SOCKS как системный прокси ОС. Работает
	// и вместе с TunMode (тогда прокси указывает на SOCKS внутри helper'а).
	SystemProxy bool `json:"systemProxy"`
	// TunMode — поднять полный туннель (виртуальный интерфейс, весь трафик ОС
	// идёт через доску). Обслуживается привилегированным helper'ом с диалогом
	// повышения прав. SOCKS поднимается всегда, TUN — поверх него.
	TunMode bool `json:"tunMode"`
}

type StreamDTO struct {
	ID        uint32 `json:"id"`
	Target    string `json:"target"`
	Host      string `json:"host"`
	StartedAt int64  `json:"startedAt"`
	TotalUp   uint64 `json:"totalUp"`
	TotalDown uint64 `json:"totalDown"`
	RateUp    uint64 `json:"rateUp"`
	RateDown  uint64 `json:"rateDown"`
}

type LaneDTO struct {
	ID               uint32 `json:"id"`
	CongestionWindow int    `json:"congestionWindow"`
	Inflight         int    `json:"inflight"`
	PeerWindow       int    `json:"peerWindow"`
	EffectiveWindow  int    `json:"effectiveWindow"`
	TargetPayload    int    `json:"targetPayload"`
	RTTms            int64  `json:"rttMs"`
	BaseRTTms        int64  `json:"baseRttMs"`
	ConfirmedBytes   uint64 `json:"confirmedBytes"`
	Draining         bool   `json:"draining"`
}

type MetricsDTO struct {
	Status          string      `json:"status"`
	RTTms           int64       `json:"rttMs"`
	TotalUp         uint64      `json:"totalUp"`
	TotalDown       uint64      `json:"totalDown"`
	RateUp          uint64      `json:"rateUp"`
	RateDown        uint64      `json:"rateDown"`
	RateConfirmedTx uint64      `json:"rateConfirmedTx"`
	BacklogFrames   int         `json:"backlogFrames"`
	BacklogBytes    int         `json:"backlogBytes"`
	BlockedWriters  int         `json:"blockedWriters"`
	Lanes           []LaneDTO   `json:"lanes"`
	Streams         []StreamDTO `json:"streams"`
}

func toMetricsDTO(metrics bproxy.Metrics) MetricsDTO {
	dto := MetricsDTO{
		Status:          string(metrics.Status),
		RTTms:           metrics.RTT.Milliseconds(),
		TotalUp:         metrics.TotalTx,
		TotalDown:       metrics.TotalRx,
		RateUp:          metrics.RateTx,
		RateDown:        metrics.RateRx,
		RateConfirmedTx: metrics.RateConfirmedTx,
		BacklogFrames:   metrics.BacklogFrames,
		BacklogBytes:    metrics.BacklogBytes,
		BlockedWriters:  metrics.BlockedWriters,
		Lanes:           make([]LaneDTO, 0, len(metrics.Lanes)),
		Streams:         make([]StreamDTO, 0, len(metrics.Details)),
	}
	for _, lane := range metrics.Lanes {
		dto.Lanes = append(dto.Lanes, LaneDTO{
			ID:               lane.ID,
			CongestionWindow: lane.CongestionWindow,
			Inflight:         lane.Inflight,
			PeerWindow:       lane.PeerWindow,
			EffectiveWindow:  lane.EffectiveWindow,
			TargetPayload:    lane.TargetPayload,
			RTTms:            lane.RTT.Milliseconds(),
			BaseRTTms:        lane.BaseRTT.Milliseconds(),
			ConfirmedBytes:   lane.ConfirmedBytes,
			Draining:         lane.Draining,
		})
	}
	for _, stream := range metrics.Details {
		dto.Streams = append(dto.Streams, StreamDTO{
			ID:        stream.ID,
			Target:    stream.Target,
			StartedAt: stream.StartedAt.UnixMilli(),
			TotalUp:   stream.Tx,
			TotalDown: stream.Rx,
		})
	}
	return dto
}

func (a *App) Connect(cfg ConnectConfig) error {
	a.mu.Lock()
	if a.mode != "" {
		a.mu.Unlock()
		return errors.New("BoardProxy уже запущен")
	}
	// Системный прокси управляется в App (см. applySystemProxy) — включается после
	// подключения и переключается на лету.
	a.listen = cfg.Listen
	a.wantSysProxy = cfg.SystemProxy
	a.connected = false
	a.mu.Unlock()

	// Режим TUN обслуживает привилегированный helper (см. app_tun.go): GUI
	// остаётся без root, а маршруты/устройство поднимает отдельный процесс с
	// диалогом повышения прав. SOCKS в обоих режимах поднимается всегда.
	if cfg.TunMode {
		return a.connectTun(cfg)
	}
	return a.connectProxy(cfg)
}

// Reconnect останавливает текущее подключение и поднимает новое с cfg. Нужен для
// смены режима TUN «на лету» без ручного нажатия кнопки.
func (a *App) Reconnect(cfg ConnectConfig) error {
	done := a.stopActive()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
	}
	return a.Connect(cfg)
}

// SetSystemProxyEnabled включает/выключает системный прокси на лету (без
// переподключения). Настройка запоминается и применяется при следующем
// подключении, если сейчас не подключено.
func (a *App) SetSystemProxyEnabled(enabled bool) {
	a.mu.Lock()
	a.wantSysProxy = enabled
	connected := a.connected
	a.mu.Unlock()
	if connected {
		a.applySystemProxy(enabled)
	}
}

// onConnected вызывается при переходе в StatusConnected (оба режима): применяет
// желаемое состояние системного прокси.
func (a *App) onConnected() {
	a.mu.Lock()
	a.connected = true
	want := a.wantSysProxy
	a.mu.Unlock()
	a.applySystemProxy(want)
}

// markDisconnected сбрасывает системный прокси и флаг подключения.
func (a *App) markDisconnected() {
	a.applySystemProxy(false)
	a.mu.Lock()
	a.connected = false
	a.mu.Unlock()
}

// applySystemProxy идемпотентно включает/выключает системный прокси ОС,
// указывая его на текущий локальный SOCKS.
func (a *App) applySystemProxy(on bool) {
	a.mu.Lock()
	listen := a.listen
	already := a.sysProxyOn
	a.mu.Unlock()

	switch {
	case on && !already:
		if err := bproxy.SetSystemProxy(loopbackProxyAddr(listen)); err != nil {
			a.emitLog("WARN", "системный прокси не включён: "+err.Error())
			return
		}
		a.mu.Lock()
		a.sysProxyOn = true
		a.mu.Unlock()
	case !on && already:
		if err := bproxy.UnsetSystemProxy(); err != nil {
			a.emitLog("WARN", "системный прокси не восстановлен: "+err.Error())
		}
		a.mu.Lock()
		a.sysProxyOn = false
		a.mu.Unlock()
	}
}

// connectProxy запускает клиента BoardProxy внутри процесса GUI (локальный
// SOCKS5/HTTP, опционально системный прокси). Прав администратора не требует.
func (a *App) connectProxy(cfg ConnectConfig) error {
	logger := slog.New(&eventLogHandler{app: a})
	client := bproxy.New(bproxy.Config{
		Keylink:    cfg.Link,
		Listen:     cfg.Listen,
		LogLevel:   "info",
		Logger:     logger,
		BypassList: cfg.BypassList,
		EnableUDP:  true,
		MaxLanes:   cfg.MaxLanes,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	a.mu.Lock()
	a.mode = "proxy"
	a.client = client
	a.cancel = cancel
	a.done = done
	a.mu.Unlock()

	client.OnStatus(func(status bproxy.Status, statusErr error) {
		message := ""
		if statusErr != nil {
			message = statusErr.Error()
		}
		a.emit("tunnel:status", map[string]string{"status": string(status), "error": message})
		if status == bproxy.StatusConnected {
			a.onConnected()
		}
	})
	client.OnMetrics(func(metrics bproxy.Metrics) {
		a.emit("tunnel:metrics", toMetricsDTO(metrics))
	})

	go func() {
		err := client.Run(ctx)
		a.markDisconnected()
		a.mu.Lock()
		if a.client == client {
			a.client = nil
			a.cancel = nil
			a.done = nil
			if a.mode == "proxy" {
				a.mode = ""
			}
		}
		close(done)
		a.mu.Unlock()

		if err != nil && client.Status() != bproxy.StatusError {
			a.emit("tunnel:status", map[string]string{
				"status": string(bproxy.StatusError),
				"error":  err.Error(),
			})
		}
	}()
	return nil
}

// emit шлёт событие во фронтенд, если Wails-контекст уже готов. Смены статуса
// дублируются в stdout (метрики намеренно не печатаются — они идут раз в секунду
// и заглушили бы полезный вывод).
func (a *App) emit(name string, data any) {
	if name == "tunnel:status" {
		if m, ok := data.(map[string]string); ok {
			line := "[status] " + m["status"]
			if m["error"] != "" {
				line += " err=" + m["error"]
			}
			printLine(line)
		}
	}
	if ctx := a.runtimeContext(); ctx != nil {
		wruntime.EventsEmit(ctx, name, data)
	}
}

// emitLog шлёт лог во фронтенд и дублирует его в stdout. Через него проходят и
// логи helper'а (они приходят по сокету).
func (a *App) emitLog(level, msg string) {
	printLine("[" + level + "] " + msg)
	if ctx := a.runtimeContext(); ctx != nil {
		wruntime.EventsEmit(ctx, "tunnel:log", map[string]string{"level": level, "msg": msg})
	}
}

// printLine печатает строку в stdout с меткой времени.
func printLine(line string) {
	fmt.Printf("%s %s\n", time.Now().Format("15:04:05.000"), line)
}

func (a *App) Disconnect() { a.stopActive() }

// stopActive останавливает текущее подключение (proxy или tun-туннель). В режиме
// TUN helper-процесс остаётся жив для повторного использования (диалог прав —
// один раз за сессию). Возвращает канал завершения.
func (a *App) stopActive() <-chan struct{} {
	a.mu.Lock()
	mode := a.mode
	client := a.client
	cancel := a.cancel
	done := a.done
	helper := a.helper
	a.mode = ""
	a.mu.Unlock()

	a.markDisconnected() // снять системный прокси до фактической остановки

	switch mode {
	case "tun":
		if helper != nil {
			return helper.stopTunnel()
		}
	case "proxy":
		if cancel != nil {
			cancel()
		}
		if client != nil {
			client.Stop()
		}
		if done != nil {
			return done
		}
	}
	return closedChan()
}

func (a *App) GetStatus() string {
	a.mu.Lock()
	mode := a.mode
	client := a.client
	helper := a.helper
	a.mu.Unlock()
	switch mode {
	case "tun":
		if helper != nil {
			return helper.currentStatus()
		}
	case "proxy":
		if client != nil {
			return string(client.Status())
		}
	}
	return string(bproxy.StatusDisconnected)
}

func (a *App) GetMetrics() MetricsDTO {
	a.mu.Lock()
	mode := a.mode
	client := a.client
	helper := a.helper
	a.mu.Unlock()
	switch mode {
	case "tun":
		if helper != nil {
			return helper.currentMetrics()
		}
	case "proxy":
		if client != nil {
			return toMetricsDTO(client.Metrics())
		}
	}
	return MetricsDTO{Status: string(bproxy.StatusDisconnected), Streams: []StreamDTO{}}
}

func (a *App) UpdateBypassList(patterns []string) error {
	a.mu.Lock()
	mode := a.mode
	client := a.client
	helper := a.helper
	a.mu.Unlock()
	switch mode {
	case "tun":
		if helper != nil {
			helper.updateBypass(patterns)
		}
		return nil
	case "proxy":
		if client != nil {
			return client.UpdateBypassList(patterns)
		}
	}
	return nil
}

type eventLogHandler struct {
	app   *App
	attrs []slog.Attr
	group string
}

func (h *eventLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *eventLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.app.emitLog(record.Level.String(), h.format(record))
	return nil
}

func (h *eventLogHandler) format(record slog.Record) string {
	var message strings.Builder
	message.WriteString(record.Message)
	for _, attr := range h.attrs {
		appendLogAttr(&message, "", attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		appendLogAttr(&message, h.group, attr)
		return true
	})
	return message.String()
}

func (h *eventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, attr := range attrs {
		if h.group != "" {
			attr.Key = h.group + "." + attr.Key
		}
		clone.attrs = append(clone.attrs, attr)
	}
	return &clone
}

func (h *eventLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	if clone.group == "" {
		clone.group = name
	} else {
		clone.group += "." + name
	}
	return &clone
}

func appendLogAttr(message *strings.Builder, prefix string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := attr.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			appendLogAttr(message, key, child)
		}
		return
	}
	message.WriteByte(' ')
	message.WriteString(key)
	message.WriteByte('=')
	if attr.Value.Kind() == slog.KindString {
		message.WriteString(strconv.Quote(attr.Value.String()))
		return
	}
	message.WriteString(fmt.Sprint(attr.Value.Any()))
}
