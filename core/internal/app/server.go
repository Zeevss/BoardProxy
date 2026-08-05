package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/config"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/egress"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/link"
	"bproxy-core/internal/logging"
	"bproxy-core/internal/mgmt"
	"bproxy-core/internal/netstats"
	"bproxy-core/internal/store"
	"bproxy-core/internal/store/sqlite"
)

// webAPIShutdownTimeout bounds how long the web API's http.Server waits for
// in-flight requests to finish on shutdown.
const webAPIShutdownTimeout = 3 * time.Second

// ErrRestart возвращается RunServer, когда через управляющий сокет запрошен
// плавный перезапуск. Вызывающий (cmd serve) перезапускает RunServer в цикле,
// заново резолвя доску (например, только что добавленную через `boards add`).
var ErrRestart = errors.New("app: restart requested")

// serverMetricsInterval — период лога агрегатной нагрузки сервера.
const serverMetricsInterval = 30 * time.Second

// Five successful reconnects per minute already means the connection is
// cycling much faster than normal and may repeatedly download page snapshots.
const reconnectStormPerMinute = 5

// RunServer поднимает сервер: открывает store, резолвит обслуживаемую доску
// (явный ‑board или первый активный хаб из store), поднимает хаб + egress и
// управляющий сокет. Без доски стартует в board-less режиме — только
// управляющий сокет, чтобы можно было добавить доску и перезапуститься.
func RunServer(ctx context.Context, cfg config.Config, log *slog.Logger, logs *logging.Buffer) error {
	serverStatic, err := serverKeypair(cfg.Server.KeyPath)
	if err != nil {
		return fmt.Errorf("server key: %w", err)
	}
	// Отложенный импорт бэкапа: если панель загрузила дамп и запросила рестарт,
	// он лежит staging-файлом рядом с БД — вносим его на место ДО открытия store.
	if err := applyPendingRestore(cfg.Store.Path, log); err != nil {
		return fmt.Errorf("apply pending restore: %w", err)
	}
	st, err := sqlite.Open(ctx, cfg.Store.Path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	// runCtx отменяется либо родителем (shutdown), либо запросом рестарта — так
	// перезапуск переиспользует весь штатный graceful-shutdown.
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	networkMetrics := netstats.Start(runCtx)
	reconnectMetrics := yandex.NewReconnectMetrics()

	// Набор досок для обслуживания: активные хабы из store плюс явно заданные в
	// -board/BPROXY_BOARD (через запятую). Битая доска не валит остальные.
	boards := resolveBoards(ctx, cfg.Board.Hash, st)
	set := &hubSet{}
	for _, board := range boards {
		srv, err := startHub(runCtx, cfg, board, log, serverStatic, st, reconnectMetrics)
		if err != nil {
			log.Warn("skip board: hub failed to start", "board", board, "err", err)
			continue
		}
		name := board
		if hb, herr := hubByID(ctx, st, board); herr == nil && hb.Name != "" {
			name = hb.Name
		}
		set.hubs = append(set.hubs, namedHub{board: board, name: name, srv: srv})
		defer srv.Close()
	}
	if set.empty() {
		log.Warn("no board configured — add one with `boards add <hash>` then `restart` " +
			"(or start with -b/--board); the management socket is up so you can do that now")
	}

	stats := statsFunc(st, set, networkMetrics, reconnectMetrics)
	if cfg.Server.Socket != "" || cfg.Server.WebAPI != "" {
		var disc mgmt.Disconnector
		var conns mgmt.ConnectionsProvider
		if !set.empty() {
			disc = set
			conns = set
		}
		h := mgmt.Handler(mgmt.Config{
			Store:        st,
			ServerPublic: serverStatic.Public(),
			Board:        cfg.Board.Hash,
			Disconnector: disc,
			Connections:  conns,
			Restart:      func() { cancelRun(ErrRestart) },
			Logs:         logsFunc(logs),
			Stats:        stats,
			Backup:       backupFunc(st),
			Restore:      restoreFunc(cfg.Store.Path, func() { cancelRun(ErrRestart) }),
		})

		if cfg.Server.Socket != "" {
			go func() {
				if err := mgmt.Serve(runCtx, cfg.Server.Socket, h); err != nil {
					log.Error("management api", "err", err)
				}
			}()
			log.Info("management socket", "path", cfg.Server.Socket)
		}

		if cfg.Server.WebAPI != "" {
			startWebAPI(runCtx, cfg, h, log)
		}
	}

	// egress на каждый поднятый хаб; сервер живёт, пока runCtx не отменён.
	var wg sync.WaitGroup
	for _, nh := range set.hubs {
		wg.Add(1)
		go func(nh namedHub) {
			defer wg.Done()
			if err := egress.Serve(runCtx, nh.srv, log, egress.Options{
				AllowPrivate: cfg.Server.AllowPrivateEgress,
			}); err != nil &&
				!errors.Is(err, context.Canceled) {
				log.Error("egress", "board", nh.board, "err", err)
			}
		}(nh)
	}
	go serverMetricsLoop(runCtx, stats, log)
	<-runCtx.Done()
	wg.Wait()

	if errors.Is(context.Cause(runCtx), ErrRestart) {
		return ErrRestart
	}
	return nil
}

// namedHub — поднятый хаб вместе с хэшем и именем обслуживаемой доски.
type namedHub struct {
	board string
	name  string
	srv   *hub.Server
}

// hubSet — набор поднятых хабов одного сервера. Реализует mgmt.Disconnector и
// mgmt.ConnectionsProvider фан-аутом по всем хабам (клиент может быть подключён к
// нескольким доскам).
type hubSet struct{ hubs []namedHub }

func (h *hubSet) empty() bool { return len(h.hubs) == 0 }

// DisconnectUser рвёт живые сессии пользователя во всех хабах, возвращает сумму.
func (h *hubSet) DisconnectUser(ctx context.Context, userID int64) int {
	total := 0
	for _, nh := range h.hubs {
		total += nh.srv.DisconnectUser(ctx, userID)
	}
	return total
}

// UserConnections собирает живые соединения пользователя по всем хабам.
func (h *hubSet) UserConnections(userID int64) []hub.ConnectionInfo {
	var out []hub.ConnectionInfo
	for _, nh := range h.hubs {
		out = append(out, nh.srv.UserConnections(userID)...)
	}
	return out
}

// startWebAPI поднимает управляющий API поверх обычного TCP/HTTP (в дополнение
// к unix-сокету) — для удалённого/скриптового доступа. Встроенной
// аутентификации нет: биндинг на что-то кроме loopback без WebAPIToken отдаёт
// полный контроль (CRUD клиентов/хабов, рестарт) любому, кто дотянется до
// адреса, поэтому при таком сочетании логируем явное предупреждение при
// старте, а не молчим.
func startWebAPI(ctx context.Context, cfg config.Config, h http.Handler, log *slog.Logger) {
	if cfg.Server.WebAPIToken == "" && cfg.Server.WebUIPassword == "" {
		log.Error("web API disabled: configure --web-api-token or --web-ui-password",
			"addr", cfg.Server.WebAPI)
		return
	}
	// Аутентификация web-API: bearer-токен (для скриптов) и/или пароль веб-панели
	// (сессионная cookie). Unix-сокет остаётся без неё — там граница файловые права.
	h = mgmt.WebAuth(mgmt.WebAuthConfig{
		Token:      cfg.Server.WebAPIToken,
		UIPassword: cfg.Server.WebUIPassword,
	}, h)
	srv := &http.Server{
		Addr:              cfg.Server.WebAPI,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("web api", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), webAPIShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	auth := cfg.Server.WebAPIToken != "" || cfg.Server.WebUIPassword != ""
	log.Info("web api listening", "addr", cfg.Server.WebAPI, "auth", auth)
}

// resolveBoards возвращает набор досок для обслуживания: объединение активных
// хабов из store и явно заданных во флаге (через запятую), с сохранением порядка
// и без дублей. Пустой результат — board-less старт.
func resolveBoards(ctx context.Context, flag string, st *sqlite.Store) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	// Активные хабы из store — приоритет по порядку создания (ListHubs сортирует).
	if hubs, err := st.ListHubs(ctx); err == nil {
		for _, h := range hubs {
			if h.Status == store.HubActive {
				add(h.ID)
			}
		}
	}
	// Явно заданные во флаге/env (через запятую) — добавляем и заводим, если новые.
	for _, id := range strings.Split(flag, ",") {
		add(id)
	}
	return out
}

// hubByID ищет запись хаба по id (store не отдаёт «по одному» — фильтруем список).
func hubByID(ctx context.Context, st *sqlite.Store, id string) (store.Hub, error) {
	hubs, err := st.ListHubs(ctx)
	if err != nil {
		return store.Hub{}, err
	}
	for _, h := range hubs {
		if h.ID == id {
			return h, nil
		}
	}
	return store.Hub{}, store.ErrNotFound
}

// startHub присоединяется к доске board, поднимает хаб и регистрирует его в
// store. board задаётся явно (а не берётся из cfg.Board.Hash) — так один сервер
// поднимает несколько досок из общего cfg.
func startHub(ctx context.Context, cfg config.Config, board string, log *slog.Logger, serverStatic crypto.Keypair, st *sqlite.Store, reconnectMetrics *yandex.ReconnectMetrics) (*hub.Server, error) {
	// Локальная копия cfg с конкретной доской — boardOptions/resolveHubSlide
	// читают cfg.Board.Hash.
	bcfg := cfg
	bcfg.Board.Hash = board
	// Для уже известной доски лимит из панели имеет приоритет над глобальным
	// дефолтом. Значение фиксируется на время жизни hub и меняется рестартом.
	storedHub, hubLookupErr := hubByID(ctx, st, board)
	hubWasStored := hubLookupErr == nil
	if hubWasStored && storedHub.MaxLanes >= 1 && storedHub.MaxLanes <= 32 {
		bcfg.Server.MaxLanes = storedHub.MaxLanes
	}

	laneOptions := boardOptions(bcfg)
	laneOptions.Role = "server-lane"
	laneOptions.Metrics = reconnectMetrics
	laneOptions.Log = log.With("component", "board", "role", "server-lane", "board", board)
	hubOptions := laneOptions
	hubOptions.ReconnectForever = true
	hubOptions.Role = "hub-control"
	hubOptions.Log = log.With("component", "board", "role", "hub-control", "board", board)
	hubSess, err := yandex.Join(ctx, hubOptions)
	if err != nil {
		return nil, fmt.Errorf("join board: %w", err)
	}
	hubSlide, err := resolveHubSlide(bcfg, hubSess)
	if err != nil {
		_ = hubSess.Close()
		return nil, fmt.Errorf("resolve hub page: %w", err)
	}
	pool := poolExcluding(hubSess.Slides(), hubSlide)
	if len(pool) == 0 {
		_ = hubSess.Close()
		return nil, fmt.Errorf("board has no free pages besides the hub slide")
	}
	srv, err := hub.NewServer(ctx, hub.ServerConfig{
		HubSession:         hubSess,
		HubSlide:           hubSlide,
		Pool:               pool,
		Dialer:             yandexDialer{laneOptions},
		ServerStatic:       serverStatic,
		Users:              st,
		Codec:              codec.Z85Codec{},
		Link:               linkOptions(bcfg, log),
		MaxPayload:         cfg.Transport.MaxFramePayload,
		StreamWindow:       cfg.Transport.StreamWindow,
		MaxStreamWindow:    cfg.Transport.MaxStreamWindow,
		CoalesceTarget:     cfg.Transport.CoalesceTarget,
		StreamIdleTimeout:  cfg.Transport.StreamIdleTimeout,
		IdleTimeout:        cfg.Server.IdleTimeout,
		MaxLanes:           bcfg.Server.MaxLanes,
		MaxSessionsPerUser: cfg.Server.MaxSessionsPerUser,
	})
	if err != nil {
		return nil, fmt.Errorf("start hub: %w", err)
	}
	// Регистрируем обслуживаемую доску (чтобы boards ls её показывал).
	if _, err := st.UpsertHub(ctx, board, board, hubSlide); err != nil {
		log.Warn("register hub", "err", err)
	} else if !hubWasStored {
		if err := st.SetHubMaxLanes(ctx, board, bcfg.Server.MaxLanes); err != nil {
			log.Warn("persist hub max lanes", "err", err)
		}
	}
	log.Info("server ready", "board", board, "hub", hubSlide, "pages", len(pool),
		"window", link.ResolveRecvWindow(cfg.Transport.Window), "max_frame_payload", cfg.Transport.MaxFramePayload,
		"stream_window", cfg.Transport.StreamWindow, "max_stream_window", cfg.Transport.MaxStreamWindow,
		"max_lanes", bcfg.Server.MaxLanes, "coalesce_target", "adaptive", "coalesce_ceiling", cfg.Transport.CoalesceTarget,
		"stream_idle_timeout", cfg.Transport.StreamIdleTimeout)
	return srv, nil
}

// serverMetricsLoop logs the same aggregate snapshot exposed by GET /stats and
// raises an explicit warning when successful reconnects form a storm.
func serverMetricsLoop(ctx context.Context, stats func() mgmt.ServerStats, log *slog.Logger) {
	t := time.NewTicker(serverMetricsInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s := stats()
			log.Info("server stats",
				"hubs", s.HubsUp, "online_users", s.OnlineUsers,
				"connections", s.ActiveConnections, "lanes", s.ActiveLanes,
				"streams", s.ActiveStreams, "free_pages", s.FreePages,
				"payload_rx_bytes", s.RxBytes, "payload_tx_bytes", s.TxBytes,
				"network_scope", s.Network.Scope, "network_interfaces", strings.Join(s.Network.Interfaces, ","),
				"network_rx_bytes", s.Network.RxBytes, "network_tx_bytes", s.Network.TxBytes,
				"network_rx_bps", uint64(s.Network.RxBytesPerSecond),
				"network_tx_bps", uint64(s.Network.TxBytesPerSecond),
				"reconnects_1m", s.Transport.ReconnectsLastMinute,
				"snapshot_bytes_1m", s.Transport.SnapshotBytesLastMinute,
				"reconnect_circuit_open_total", s.Transport.CircuitOpenTotal,
				"page_cleanup_runs", s.PageCleanupRuns,
				"page_cleanup_deleted", s.PageCleanupDeleted,
				"page_cleanup_failures", s.PageCleanupFailures,
				"page_cleanup_quarantined", s.PageCleanupQuarantined)
			if s.Transport.ReconnectsLastMinute >= reconnectStormPerMinute {
				log.Warn("board reconnect storm",
					"reconnects_1m", s.Transport.ReconnectsLastMinute,
					"reconnects_5m", s.Transport.ReconnectsLastFiveMinutes,
					"snapshot_bytes_1m", s.Transport.SnapshotBytesLastMinute,
					"snapshot_bytes_5m", s.Transport.SnapshotBytesLastFiveMinutes,
					"last_disconnect_reason", s.Transport.LastDisconnectReason)
			}
		}
	}
}

// storeStatsTimeout ограничивает запросы к store при сборке /stats — эти чтения
// не должны подвешивать HTTP-ответ дашборда.
const storeStatsTimeout = 3 * time.Second

// logsFunc адаптирует кольцевой буфер лога к mgmt.Config.Logs. Nil-буфер
// (например в тестах) даёт nil-функцию — эндпойнт /logs тогда отдаёт пустой
// список.
func logsFunc(buf *logging.Buffer) func(int) []mgmt.LogEntry {
	if buf == nil {
		return nil
	}
	return func(limit int) []mgmt.LogEntry {
		entries := buf.Entries(limit)
		out := make([]mgmt.LogEntry, len(entries))
		for i, e := range entries {
			out[i] = mgmt.LogEntry{Time: e.Time, Level: e.Level, Message: e.Message}
		}
		return out
	}
}

// statsFunc собирает агрегатную статистику для дашборда: персистентные счётчики
// из store (клиенты, доски, суммарный трафик за завершённые сессии) плюс живой
// снимок хаба (клиенты онлайн, свободные страницы, трафик активных сессий).
func statsFunc(st *sqlite.Store, set *hubSet, network *netstats.Monitor, reconnects *yandex.ReconnectMetrics) func() mgmt.ServerStats {
	return func() mgmt.ServerStats {
		ctx, cancel := context.WithTimeout(context.Background(), storeStatsTimeout)
		defer cancel()
		out := mgmt.ServerStats{HubsUp: len(set.hubs)}
		if users, err := st.ListUsers(ctx); err == nil {
			out.Clients = len(users)
			for _, u := range users {
				if u.Status == store.UserActive {
					out.ClientsActive++
				}
				out.RxBytes += u.RxBytes
				out.TxBytes += u.TxBytes
				connections := set.UserConnections(u.ID)
				us := mgmt.UserStat{
					ID: u.ID, Name: u.Name, Status: string(u.Status),
					Connections: len(connections), RxBytes: u.RxBytes, TxBytes: u.TxBytes,
				}
				if !u.LastSeen.IsZero() {
					lastSeen := u.LastSeen
					us.LastSeen = &lastSeen
				}
				for _, conn := range connections {
					us.ActiveRxBytes += conn.Received
					us.ActiveTxBytes += conn.Written
					us.Lanes += len(conn.Lanes)
					us.Streams += len(conn.Streams)
				}
				us.Online = us.Connections > 0
				if us.Online {
					out.OnlineUsers++
				}
				us.RxBytes += us.ActiveRxBytes
				us.TxBytes += us.ActiveTxBytes
				out.ActiveConnections += us.Connections
				out.ActiveLanes += us.Lanes
				out.ActiveStreams += us.Streams
				out.Users = append(out.Users, us)
			}
		}
		if hubs, err := st.ListHubs(ctx); err == nil {
			out.Boards = len(hubs)
			for _, hb := range hubs {
				if hb.Status == store.HubActive {
					out.BoardsActive++
				}
			}
		}
		// Живой снимок по каждому поднятому хабу плюс агрегаты.
		for _, nh := range set.hubs {
			s := nh.srv.Stats()
			out.ServingBoards = append(out.ServingBoards, nh.board)
			out.ClientsOnline += s.Clients
			out.FreePages += s.FreePages
			out.PageCleanupRuns += s.PageCleanupRuns
			out.PageCleanupDeleted += s.PageCleanupDeleted
			out.PageCleanupFailures += s.PageCleanupFailures
			out.PageCleanupQuarantined += s.PageCleanupQuarantined
			// Трафик активных сессий ещё не осел в store — добавляем его к своду.
			out.RxBytes += s.Received
			out.TxBytes += s.Written
			out.PerBoard = append(out.PerBoard, mgmt.BoardStat{
				ID:                     nh.board,
				Name:                   nh.name,
				ClientsOnline:          s.Clients,
				FreePages:              s.FreePages,
				RxBytes:                s.Received,
				TxBytes:                s.Written,
				PageCleanupRuns:        s.PageCleanupRuns,
				PageCleanupDeleted:     s.PageCleanupDeleted,
				PageCleanupFailures:    s.PageCleanupFailures,
				PageCleanupQuarantined: s.PageCleanupQuarantined,
			})
		}
		if network != nil {
			out.Network = networkStat(network.Snapshot())
		}
		out.Transport = transportStat(reconnects.Snapshot())
		return out
	}
}

func networkStat(s netstats.Snapshot) mgmt.NetworkStat {
	return mgmt.NetworkStat{
		Available: s.Available, Scope: s.Scope, Interfaces: s.Interfaces,
		StartedAt: s.StartedAt, SampledAt: s.SampledAt,
		RxBytes: s.RXBytes, TxBytes: s.TXBytes,
		RxBytesSinceStart: s.RXBytesSinceStart, TxBytesSinceStart: s.TXBytesSinceStart,
		RxBytesPerSecond: s.RXBytesPerSecond, TxBytesPerSecond: s.TXBytesPerSecond,
	}
}

func transportStat(s yandex.ReconnectMetricsSnapshot) mgmt.TransportStat {
	out := mgmt.TransportStat{
		StartedAt:        s.StartedAt,
		DisconnectsTotal: s.DisconnectsTotal, ReconnectsTotal: s.ReconnectsTotal,
		ReconnectAttemptsFailed: s.ReconnectAttemptsFailed,
		CircuitOpenTotal:        s.CircuitOpenTotal,
		SnapshotObjectsTotal:    s.SnapshotObjectsTotal, SnapshotBytesTotal: s.SnapshotBytesTotal,
		ReconnectsLastMinute:         s.ReconnectsLastMinute,
		ReconnectsLastFiveMinutes:    s.ReconnectsLastFiveMinutes,
		SnapshotBytesLastMinute:      s.SnapshotBytesLastMinute,
		SnapshotBytesLastFiveMinutes: s.SnapshotBytesLastFiveMinutes,
		LastDisconnectAt:             timePointer(s.LastDisconnectAt), LastDisconnectReason: s.LastDisconnectReason,
		LastConnectedForMillis: s.LastConnectedFor.Milliseconds(),
		LastReconnectAt:        timePointer(s.LastReconnectAt), LastDowntimeMillis: s.LastDowntime.Milliseconds(),
		LastSnapshotObjects: s.LastSnapshotObjects, LastSnapshotBytes: s.LastSnapshotBytes,
	}
	for _, r := range s.PerRole {
		out.PerRole = append(out.PerRole, mgmt.ReconnectRoleStat{
			Role: r.Role, Board: r.Board,
			DisconnectsTotal: r.DisconnectsTotal, ReconnectsTotal: r.ReconnectsTotal,
			ReconnectAttemptsFailed: r.ReconnectAttemptsFailed,
			CircuitOpenTotal:        r.CircuitOpenTotal,
			SnapshotObjectsTotal:    r.SnapshotObjectsTotal, SnapshotBytesTotal: r.SnapshotBytesTotal,
			ReconnectsLastMinute:    r.ReconnectsLastMinute,
			SnapshotBytesLastMinute: r.SnapshotBytesLastMinute,
			LastDisconnectAt:        timePointer(r.LastDisconnectAt), LastDisconnectReason: r.LastDisconnectReason,
			LastConnectedForMillis: r.LastConnectedFor.Milliseconds(),
			LastReconnectAt:        timePointer(r.LastReconnectAt), LastDowntimeMillis: r.LastDowntime.Milliseconds(),
			LastSnapshotObjects: r.LastSnapshotObjects, LastSnapshotBytes: r.LastSnapshotBytes,
		})
	}
	return out
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// backupFunc отдаёт консистентный снимок БД потоком. Снимок делается во
// временный файл (VACUUM INTO), который удаляется при закрытии возвращённого
// ReadCloser.
func backupFunc(st *sqlite.Store) func(context.Context) (io.ReadCloser, int64, error) {
	return func(ctx context.Context) (io.ReadCloser, int64, error) {
		tmp, err := os.CreateTemp("", "bproxy-backup-*.db")
		if err != nil {
			return nil, 0, err
		}
		path := tmp.Name()
		// VACUUM INTO сам создаёт файл и отказывается перезаписывать существующий,
		// поэтому пустышку от CreateTemp убираем, оставляя только имя.
		_ = tmp.Close()
		_ = os.Remove(path)
		if err := st.Backup(ctx, path); err != nil {
			_ = os.Remove(path)
			return nil, 0, err
		}
		f, err := os.Open(path)
		if err != nil {
			_ = os.Remove(path)
			return nil, 0, err
		}
		var size int64
		if fi, err := f.Stat(); err == nil {
			size = fi.Size()
		}
		return &tempFileReader{File: f, path: path}, size, nil
	}
}

// tempFileReader — открытый временный файл, который удаляет себя при Close.
type tempFileReader struct {
	*os.File
	path string
}

func (t *tempFileReader) Close() error {
	err := t.File.Close()
	_ = os.Remove(t.path)
	return err
}

// restoreFunc принимает загруженный дамп БД и кладёт его staging-файлом рядом с
// БД (<path>.import), затем инициирует плавный перезапуск: фактическая подмена
// файла происходит в applyPendingRestore при следующем старте, до открытия
// store, чтобы не гонять запись по уже открытому файлу.
func restoreFunc(dbPath string, restart func()) func(context.Context, io.Reader) error {
	return func(ctx context.Context, r io.Reader) error {
		staging := dbPath + ".import"
		f, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("create staging: %w", err)
		}
		if _, err := io.Copy(f, r); err != nil {
			_ = f.Close()
			_ = os.Remove(staging)
			return fmt.Errorf("write staging: %w", err)
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(staging)
			return fmt.Errorf("sync staging: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(staging)
			return fmt.Errorf("close staging: %w", err)
		}
		if err := sqlite.Validate(ctx, staging); err != nil {
			_ = os.Remove(staging)
			return fmt.Errorf("validate staging: %w", err)
		}
		if restart != nil {
			restart()
		}
		return nil
	}
}

// applyPendingRestore вносит отложенный импорт БД: если рядом с БД лежит
// staging-файл <path>.import, атомарно переносит его на место БД и удаляет
// сопутствующие -wal/-shm (они относятся к прежнему файлу). Вызывается в начале
// RunServer, до открытия store.
func applyPendingRestore(dbPath string, log *slog.Logger) error {
	staging := dbPath + ".import"
	if _, err := os.Stat(staging); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(staging, dbPath); err != nil {
		return fmt.Errorf("swap in imported db: %w", err)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			log.Warn("remove stale sqlite sidecar", "path", sidecar, "err", err)
		}
	}
	log.Info("imported database applied", "path", dbPath)
	return nil
}
