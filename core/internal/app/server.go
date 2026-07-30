package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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

	// Набор досок для обслуживания: активные хабы из store плюс явно заданные в
	// -board/BPROXY_BOARD (через запятую). Битая доска не валит остальные.
	boards := resolveBoards(ctx, cfg.Board.Hash, st)
	set := &hubSet{}
	for _, board := range boards {
		srv, err := startHub(runCtx, cfg, board, log, serverStatic, st)
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
			Stats:        statsFunc(st, set),
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
			if err := egress.Serve(runCtx, nh.srv, log); err != nil &&
				!errors.Is(err, context.Canceled) {
				log.Error("egress", "board", nh.board, "err", err)
			}
		}(nh)
	}
	if !set.empty() {
		go serverMetricsLoop(runCtx, set, log)
	}
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
	// Аутентификация web-API: bearer-токен (для скриптов) и/или пароль веб-панели
	// (сессионная cookie). Unix-сокет остаётся без неё — там граница файловые права.
	h = mgmt.WebAuth(mgmt.WebAuthConfig{
		Token:      cfg.Server.WebAPIToken,
		UIPassword: cfg.Server.WebUIPassword,
	}, h)
	if cfg.Server.WebAPIToken == "" && cfg.Server.WebUIPassword == "" && !isLoopbackAddr(cfg.Server.WebAPI) {
		log.Warn("web API bound to a non-loopback address without --web-api-token or --web-ui-password — "+
			"anyone who can reach it has full control (client/board CRUD, restart)",
			"addr", cfg.Server.WebAPI)
	}

	srv := &http.Server{Addr: cfg.Server.WebAPI, Handler: h}
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

// isLoopbackAddr сообщает, резолвится ли хост addr ("host:port") в loopback.
// Пустой/неразбираемый хост (например только ":8080") считается НЕ loopback —
// это то же самое, что 0.0.0.0, слушает на всех интерфейсах.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
func startHub(ctx context.Context, cfg config.Config, board string, log *slog.Logger, serverStatic crypto.Keypair, st *sqlite.Store) (*hub.Server, error) {
	// Локальная копия cfg с конкретной доской — boardOptions/resolveHubSlide
	// читают cfg.Board.Hash.
	bcfg := cfg
	bcfg.Board.Hash = board

	hubSess, err := yandex.Join(ctx, boardOptions(bcfg))
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
		HubSession:        hubSess,
		HubSlide:          hubSlide,
		Pool:              pool,
		Dialer:            yandexDialer{boardOptions(bcfg)},
		ServerStatic:      serverStatic,
		Users:             st,
		Codec:             codec.Z85Codec{},
		Link:              linkOptions(bcfg, log),
		MaxPayload:        cfg.Transport.MaxFramePayload,
		StreamWindow:      cfg.Transport.StreamWindow,
		MaxStreamWindow:   cfg.Transport.MaxStreamWindow,
		CoalesceTarget:    cfg.Transport.CoalesceTarget,
		StreamIdleTimeout: cfg.Transport.StreamIdleTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxLanes:          cfg.Server.MaxLanes,
	})
	if err != nil {
		return nil, fmt.Errorf("start hub: %w", err)
	}
	// Регистрируем обслуживаемую доску (чтобы boards ls её показывал).
	if _, err := st.UpsertHub(ctx, board, board, hubSlide); err != nil {
		log.Warn("register hub", "err", err)
	}
	log.Info("server ready", "board", board, "hub", hubSlide, "pages", len(pool),
		"window", link.ResolveRecvWindow(cfg.Transport.Window), "max_frame_payload", cfg.Transport.MaxFramePayload,
		"stream_window", cfg.Transport.StreamWindow, "max_stream_window", cfg.Transport.MaxStreamWindow,
		"max_lanes", cfg.Server.MaxLanes, "coalesce_target", "adaptive", "coalesce_ceiling", cfg.Transport.CoalesceTarget,
		"stream_idle_timeout", cfg.Transport.StreamIdleTimeout)
	return srv, nil
}

// serverMetricsLoop периодически логирует агрегатную нагрузку сервера (число
// клиентов, свободные страницы, суммарный трафик). Персистентный учёт по
// пользователям (connections.rx/tx_bytes) появится вместе с lifecycle
// connections — здесь только оперативная видимость.
func serverMetricsLoop(ctx context.Context, set *hubSet, log *slog.Logger) {
	t := time.NewTicker(serverMetricsInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var clients, freePages int
			var written, received uint64
			for _, nh := range set.hubs {
				s := nh.srv.Stats()
				clients += s.Clients
				freePages += s.FreePages
				written += s.Written
				received += s.Received
			}
			log.Info("server stats", "hubs", len(set.hubs), "clients", clients,
				"free_pages", freePages, "tx_bytes", written, "rx_bytes", received)
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
func statsFunc(st *sqlite.Store, set *hubSet) func() mgmt.ServerStats {
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
			// Трафик активных сессий ещё не осел в store — добавляем его к своду.
			out.RxBytes += s.Received
			out.TxBytes += s.Written
			out.PerBoard = append(out.PerBoard, mgmt.BoardStat{
				ID:            nh.board,
				Name:          nh.name,
				ClientsOnline: s.Clients,
				FreePages:     s.FreePages,
				RxBytes:       s.Received,
				TxBytes:       s.Written,
			})
		}
		return out
	}
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
	return func(_ context.Context, r io.Reader) error {
		staging := dbPath + ".import"
		f, err := os.Create(staging)
		if err != nil {
			return fmt.Errorf("create staging: %w", err)
		}
		if _, err := io.Copy(f, r); err != nil {
			_ = f.Close()
			_ = os.Remove(staging)
			return fmt.Errorf("write staging: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(staging)
			return fmt.Errorf("close staging: %w", err)
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
