// Package mgmt — локальный управляющий слой сервера BoardProxy: HTTP-API над
// unix-сокетом для управления клиентами (пользователями) и досками (хабами).
// CLI (bproxy clients/boards) — его HTTP-клиент.
package mgmt

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/store"
)

// Store — то, что нужно управляющему слою от хранилища (узкий срез store.Store).
type Store interface {
	CreateUser(ctx context.Context, pubKey []byte, name string) (store.User, error)
	UserByID(ctx context.Context, id int64) (store.User, error)
	ListUsers(ctx context.Context) ([]store.User, error)
	SetUserStatus(ctx context.Context, id int64, status store.UserStatus) error
	SetUserName(ctx context.Context, id int64, name string) error
	DeleteUser(ctx context.Context, id int64) error
	UpsertHub(ctx context.Context, id, name, hubSlide string) (store.Hub, error)
	ListHubs(ctx context.Context) ([]store.Hub, error)
	SetHubStatus(ctx context.Context, id string, status store.HubStatus) error
	SetHubName(ctx context.Context, id string, name string) error
	SetHubMaxLanes(ctx context.Context, id string, maxLanes int) error
	DeleteHub(ctx context.Context, id string) error
}

// AccessKeyStore is kept separate from Store because issuing management
// credentials is available only on the local Unix socket, never through the
// remotely exposed management HTTP API.
type AccessKeyStore interface {
	CreateAccessKey(ctx context.Context, name, prefix string, digest []byte) (store.AccessKey, error)
	ListAccessKeys(ctx context.Context) ([]store.AccessKey, error)
	RevokeAccessKey(ctx context.Context, id int64) error
}

// Disconnector корректно закрывает живые сессии пользователя (см.
// hub.Server.DisconnectUser). Нужен, чтобы отключение клиента рвало его живые
// соединения, а не оставляло их висеть на линии.
type Disconnector interface {
	DisconnectUser(ctx context.Context, userID int64) int
}

// ConnectionsProvider даёт снимок живых соединений клиента (см.
// hub.Server.UserConnections) — для просмотра активных подключений через
// управление. Nil — эндпойнт соединений и live-часть трафика в ClientInfo
// недоступны (например в тестах без поднятого хаба).
type ConnectionsProvider interface {
	UserConnections(userID int64) []hub.ConnectionInfo
}

// Config конфигурирует управляющий слой.
type Config struct {
	Store Store
	// AccessKeys enables local access-key management endpoints. The application
	// exposes them on the Unix socket and filters them out from remote HTTP.
	AccessKeys AccessKeyStore
	// ServerPublic — публичный ключ сервера; уходит в keylink при создании
	// клиента.
	ServerPublic []byte
	// Board — обслуживаемая доска; уходит в keylink при создании клиента. Пусто
	// (board-less старт) — доска берётся из первого активного хаба store.
	Board string
	// Disconnector, если задан, рвёт живые сессии пользователя при его удалении.
	Disconnector Disconnector
	// Connections, если задан, даёт живые соединения и live-трафик клиента.
	Connections ConnectionsProvider
	// Restart, если задан, запускает плавный перезапуск сервера (POST /restart).
	Restart func()
	// Logs, если задан, отдаёт последние limit записей лога сервера (GET /logs).
	// Nil — эндпойнт отдаёт пустой список.
	Logs func(limit int) []LogEntry
	// Stats, если задан, отдаёт агрегатную статистику сервера (GET /stats).
	Stats func() ServerStats
	// Backup, если задан, отдаёт консистентный снимок БД потоком (GET /backup).
	// Вызывающий обязан закрыть ReadCloser.
	Backup func(ctx context.Context) (io.ReadCloser, int64, error)
	// Restore, если задан, принимает загруженный дамп БД и применяет его
	// (POST /backup) — как правило, с последующим плавным перезапуском сервера.
	Restore func(ctx context.Context, r io.Reader) error
}

// LogEntry — одна строка лога в API (совпадает с формой logging.Entry, но mgmt
// не зависит от logging: вызывающий передаёт готовый срез через Config.Logs).
type LogEntry struct {
	Time    time.Time `json:"ts"`
	Level   string    `json:"level"`
	Message string    `json:"msg"`
}

// ServerStats — агрегатная статистика для дашборда.
type ServerStats struct {
	Clients                int    `json:"clients"`        // всего заведённых клиентов
	ClientsActive          int    `json:"clients_active"` // из них со статусом active
	ClientsOnline          int    `json:"clients_online"` // сейчас на линии (живые сессии по всем хабам)
	Boards                 int    `json:"boards"`         // всего досок
	BoardsActive           int    `json:"boards_active"`  // из них активных
	FreePages              int    `json:"free_pages"`     // свободных страниц (сумма по хабам)
	RxBytes                uint64 `json:"rx_bytes"`       // суммарно получено от клиентов
	TxBytes                uint64 `json:"tx_bytes"`       // суммарно отправлено клиентам
	OnlineUsers            int    `json:"online_users"`
	ActiveConnections      int    `json:"active_connections"`
	ActiveLanes            int    `json:"active_lanes"`
	ActiveStreams          int    `json:"active_streams"`
	PageCleanupRuns        uint64 `json:"page_cleanup_runs"`
	PageCleanupDeleted     uint64 `json:"page_cleanup_deleted"`
	PageCleanupFailures    uint64 `json:"page_cleanup_failures"`
	PageCleanupQuarantined uint64 `json:"page_cleanup_quarantined"`
	// ServingBoards — обслуживаемые сейчас доски (поднятые хабы). Пусто —
	// board-less старт.
	ServingBoards []string `json:"serving_boards"`
	// HubsUp — сколько хабов сейчас поднято.
	HubsUp int `json:"hubs_up"`
	// PerBoard — разбивка живого состояния по каждому поднятому хабу.
	PerBoard []BoardStat `json:"per_board"`
	// Users — трафик и live-нагрузка по каждому заведённому пользователю.
	Users []UserStat `json:"users"`
	// Network — raw kernel counters default-route интерфейса core. В Docker это
	// сторона bproxy bridge внутри network namespace контейнера.
	Network NetworkStat `json:"network"`
	// Transport — причины/частота reconnect и стоимость page snapshots.
	Transport TransportStat `json:"transport"`
}

// BoardStat — живое состояние одного поднятого хаба (для разбивки на дашборде).
type BoardStat struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	ClientsOnline          int    `json:"clients_online"`
	FreePages              int    `json:"free_pages"`
	RxBytes                uint64 `json:"rx_bytes"` // трафик активных сессий этого хаба
	TxBytes                uint64 `json:"tx_bytes"`
	PageCleanupRuns        uint64 `json:"page_cleanup_runs"`
	PageCleanupDeleted     uint64 `json:"page_cleanup_deleted"`
	PageCleanupFailures    uint64 `json:"page_cleanup_failures"`
	PageCleanupQuarantined uint64 `json:"page_cleanup_quarantined"`
}

// UserStat combines persisted traffic with the user's currently active mux
// sessions. It intentionally exposes counts, not stream targets.
type UserStat struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Online        bool       `json:"online"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	Connections   int        `json:"connections"`
	Lanes         int        `json:"lanes"`
	Streams       int        `json:"streams"`
	RxBytes       uint64     `json:"rx_bytes"`
	TxBytes       uint64     `json:"tx_bytes"`
	ActiveRxBytes uint64     `json:"active_rx_bytes"`
	ActiveTxBytes uint64     `json:"active_tx_bytes"`
}

// NetworkStat is raw network activity, including WebSocket framing, page
// snapshots, management traffic and protocol overhead.
type NetworkStat struct {
	Available         bool      `json:"available"`
	Scope             string    `json:"scope"`
	Interfaces        []string  `json:"interfaces"`
	StartedAt         time.Time `json:"started_at"`
	SampledAt         time.Time `json:"sampled_at"`
	RxBytes           uint64    `json:"rx_bytes"`
	TxBytes           uint64    `json:"tx_bytes"`
	RxBytesSinceStart uint64    `json:"rx_bytes_since_start"`
	TxBytesSinceStart uint64    `json:"tx_bytes_since_start"`
	RxBytesPerSecond  float64   `json:"rx_bytes_per_second"`
	TxBytesPerSecond  float64   `json:"tx_bytes_per_second"`
}

// TransportStat captures reconnect churn that can create raw traffic without
// corresponding proxy payload.
type TransportStat struct {
	StartedAt                    time.Time           `json:"started_at"`
	DisconnectsTotal             uint64              `json:"disconnects_total"`
	ReconnectsTotal              uint64              `json:"reconnects_total"`
	ReconnectAttemptsFailed      uint64              `json:"reconnect_attempts_failed"`
	CircuitOpenTotal             uint64              `json:"circuit_open_total"`
	SnapshotObjectsTotal         uint64              `json:"snapshot_objects_total"`
	SnapshotBytesTotal           uint64              `json:"snapshot_bytes_total"`
	ReconnectsLastMinute         int                 `json:"reconnects_last_minute"`
	ReconnectsLastFiveMinutes    int                 `json:"reconnects_last_five_minutes"`
	SnapshotBytesLastMinute      uint64              `json:"snapshot_bytes_last_minute"`
	SnapshotBytesLastFiveMinutes uint64              `json:"snapshot_bytes_last_five_minutes"`
	LastDisconnectAt             *time.Time          `json:"last_disconnect_at,omitempty"`
	LastDisconnectReason         string              `json:"last_disconnect_reason,omitempty"`
	LastConnectedForMillis       int64               `json:"last_connected_for_ms"`
	LastReconnectAt              *time.Time          `json:"last_reconnect_at,omitempty"`
	LastDowntimeMillis           int64               `json:"last_downtime_ms"`
	LastSnapshotObjects          int                 `json:"last_snapshot_objects"`
	LastSnapshotBytes            uint64              `json:"last_snapshot_bytes"`
	PerRole                      []ReconnectRoleStat `json:"per_role"`
}

type ReconnectRoleStat struct {
	Role                    string     `json:"role"`
	Board                   string     `json:"board"`
	DisconnectsTotal        uint64     `json:"disconnects_total"`
	ReconnectsTotal         uint64     `json:"reconnects_total"`
	ReconnectAttemptsFailed uint64     `json:"reconnect_attempts_failed"`
	CircuitOpenTotal        uint64     `json:"circuit_open_total"`
	SnapshotObjectsTotal    uint64     `json:"snapshot_objects_total"`
	SnapshotBytesTotal      uint64     `json:"snapshot_bytes_total"`
	ReconnectsLastMinute    int        `json:"reconnects_last_minute"`
	SnapshotBytesLastMinute uint64     `json:"snapshot_bytes_last_minute"`
	LastDisconnectAt        *time.Time `json:"last_disconnect_at,omitempty"`
	LastDisconnectReason    string     `json:"last_disconnect_reason,omitempty"`
	LastConnectedForMillis  int64      `json:"last_connected_for_ms"`
	LastReconnectAt         *time.Time `json:"last_reconnect_at,omitempty"`
	LastDowntimeMillis      int64      `json:"last_downtime_ms"`
	LastSnapshotObjects     int        `json:"last_snapshot_objects"`
	LastSnapshotBytes       uint64     `json:"last_snapshot_bytes"`
}

// ClientInfo — представление пользователя в API.
type ClientInfo struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	PublicKey string     `json:"public_key"` // base64
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	// RxBytes/TxBytes — накопленный трафик клиента: персистентный (завершённые
	// сессии, store) плюс текущий живой (если Config.Connections задан и клиент
	// сейчас подключён). RxBytes — получено от клиента (upload), TxBytes —
	// отправлено клиенту (download).
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// UpdateClientRequest — тело PATCH /clients/{id}. Поля опциональны и
// применяются только если заданы (nil — не трогать). Status=disabled рвёт
// живые сессии клиента, как и DELETE.
type UpdateClientRequest struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// StreamInfo — представление одного открытого стрима в API.
type StreamInfo struct {
	ID        uint32    `json:"id"`
	Target    string    `json:"target"`
	Written   uint64    `json:"written"`
	Received  uint64    `json:"received"`
	StartedAt time.Time `json:"started_at"`
}

// ConnectionInfo — представление одного живого соединения клиента в API.
type ConnectionInfo struct {
	BundleID  string       `json:"bundle_id,omitempty"`
	LaneID    uint32       `json:"lane_id,omitempty"`
	Epoch     uint32       `json:"epoch,omitempty"`
	Page      string       `json:"page"`
	Lanes     []LaneInfo   `json:"lanes,omitempty"`
	Written   uint64       `json:"written"`
	Received  uint64       `json:"received"`
	RTTMillis int64        `json:"rtt_ms"`
	Streams   []StreamInfo `json:"streams"`
}

type LaneInfo struct {
	ID        uint32 `json:"id"`
	Page      string `json:"page"`
	RTTMillis int64  `json:"rtt_ms"`
}

// CreateClientRequest — тело POST /clients.
type CreateClientRequest struct {
	Name string `json:"name"`
}

// CreateClientResponse — ответ POST /clients: keylink возвращается один раз
// (приватный ключ клиента в нём и на сервере не хранится).
type CreateClientResponse struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Keylink string `json:"keylink"`
}

type CreateAccessKeyRequest struct {
	Name string `json:"name"`
}

type AccessKeyInfo struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// CreateAccessKeyResponse contains the raw token exactly once. Only its digest
// remains in the server store after this response.
type CreateAccessKeyResponse struct {
	AccessKeyInfo
	Token string `json:"token"`
}

// BoardInfo — представление хаба в API.
type BoardInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	HubSlide  string    `json:"hub_slide"`
	Status    string    `json:"status"`
	MaxLanes  int       `json:"max_lanes"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateBoardRequest — тело POST /boards.
type CreateBoardRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MaxLanes int    `json:"max_lanes"`
}

// UpdateBoardRequest — тело PATCH /boards/{id}. Поля опциональны и
// применяются только если заданы.
type UpdateBoardRequest struct {
	Name     *string `json:"name,omitempty"`
	Status   *string `json:"status,omitempty"`
	MaxLanes *int    `json:"max_lanes,omitempty"`
}

// Handler собирает HTTP-роутинг управляющего API.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	h := &handler{cfg: cfg}
	mux.HandleFunc("GET /clients", h.listClients)
	mux.HandleFunc("POST /clients", h.createClient)
	mux.HandleFunc("GET /clients/{id}", h.getClient)
	mux.HandleFunc("PATCH /clients/{id}", h.updateClient)
	mux.HandleFunc("DELETE /clients/{id}", h.removeClient)
	mux.HandleFunc("GET /clients/{id}/connections", h.getClientConnections)
	mux.HandleFunc("GET /boards", h.listBoards)
	mux.HandleFunc("POST /boards", h.createBoard)
	mux.HandleFunc("GET /boards/{id}", h.getBoard)
	mux.HandleFunc("PATCH /boards/{id}", h.updateBoard)
	mux.HandleFunc("DELETE /boards/{id}", h.removeBoard)
	mux.HandleFunc("GET /access-keys", h.listAccessKeys)
	mux.HandleFunc("POST /access-keys", h.createAccessKey)
	mux.HandleFunc("DELETE /access-keys/{id}", h.revokeAccessKey)
	mux.HandleFunc("POST /restart", h.restart)
	mux.HandleFunc("GET /logs", h.getLogs)
	mux.HandleFunc("GET /stats", h.getStats)
	mux.HandleFunc("GET /backup", h.exportBackup)
	mux.HandleFunc("POST /backup", h.importBackup)
	return mux
}

// RemoteHandler removes local-only credential-management endpoints from a
// handler that is going to be exposed over TCP. A panel bearer token must not
// be able to mint another token or revoke operator access.
func RemoteHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/access-keys" || strings.HasPrefix(r.URL.Path, "/access-keys/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxBackupUpload — потолок на размер загружаемого дампа БД (256 MiB), чтобы
// импорт не съел память/диск от произвольно большого тела запроса.
const maxBackupUpload = 256 << 20

type handler struct{ cfg Config }

func (h *handler) createAccessKey(w http.ResponseWriter, r *http.Request) {
	if h.cfg.AccessKeys == nil {
		httpError(w, http.StatusNotImplemented, errors.New("access key management not supported"))
		return
	}
	var req CreateAccessKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpError(w, http.StatusBadRequest, errors.New("name required"))
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	token := "bpa_" + base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	key, err := h.cfg.AccessKeys.CreateAccessKey(r.Context(), req.Name, token[:12], digest[:])
	if err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateAccessKeyResponse{AccessKeyInfo: toAccessKeyInfo(key), Token: token})
}

func (h *handler) listAccessKeys(w http.ResponseWriter, r *http.Request) {
	if h.cfg.AccessKeys == nil {
		httpError(w, http.StatusNotImplemented, errors.New("access key management not supported"))
		return
	}
	keys, err := h.cfg.AccessKeys.ListAccessKeys(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]AccessKeyInfo, 0, len(keys))
	for _, key := range keys {
		out = append(out, toAccessKeyInfo(key))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) revokeAccessKey(w http.ResponseWriter, r *http.Request) {
	if h.cfg.AccessKeys == nil {
		httpError(w, http.StatusNotImplemented, errors.New("access key management not supported"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpError(w, http.StatusBadRequest, errors.New("invalid access key id"))
		return
	}
	if err := h.cfg.AccessKeys.RevokeAccessKey(r.Context(), id); err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAccessKeyInfo(key store.AccessKey) AccessKeyInfo {
	info := AccessKeyInfo{ID: key.ID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt}
	if !key.RevokedAt.IsZero() {
		revoked := key.RevokedAt
		info.RevokedAt = &revoked
	}
	return info
}

func (h *handler) listClients(w http.ResponseWriter, r *http.Request) {
	users, err := h.cfg.Store.ListUsers(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]ClientInfo, 0, len(users))
	for _, u := range users {
		out = append(out, h.toClientInfo(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) createClient(w http.ResponseWriter, r *http.Request) {
	var req CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		httpError(w, http.StatusBadRequest, errors.New("name required"))
		return
	}
	kp, err := crypto.Generate()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	u, err := h.cfg.Store.CreateUser(r.Context(), kp.Public(), req.Name)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err)
			return
		}
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	boards := h.keylinkBoards(r.Context())
	link, err := keylink.Build(kp.Private(), h.cfg.ServerPublic, boards, req.Name)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateClientResponse{ID: u.ID, Name: u.Name, Keylink: link})
}

func (h *handler) getClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	u, err := h.cfg.Store.UserByID(r.Context(), id)
	if err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	writeJSON(w, http.StatusOK, h.toClientInfo(u))
}

// updateClient переименовывает и/или меняет статус клиента. Переход в
// disabled рвёт его живые сессии — тем же путём, что и DELETE.
func (h *handler) updateClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	var req UpdateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			httpError(w, http.StatusBadRequest, errors.New("name must not be empty"))
			return
		}
		if err := h.cfg.Store.SetUserName(r.Context(), id, *req.Name); err != nil {
			httpError(w, statusForStore(err), err)
			return
		}
	}
	if req.Status != nil {
		status := store.UserStatus(*req.Status)
		if status != store.UserActive && status != store.UserDisabled {
			httpError(w, http.StatusBadRequest, fmt.Errorf("invalid status %q", *req.Status))
			return
		}
		if err := h.cfg.Store.SetUserStatus(r.Context(), id, status); err != nil {
			httpError(w, statusForStore(err), err)
			return
		}
		if status == store.UserDisabled && h.cfg.Disconnector != nil {
			h.cfg.Disconnector.DisconnectUser(r.Context(), id)
		}
	}
	u, err := h.cfg.Store.UserByID(r.Context(), id)
	if err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	writeJSON(w, http.StatusOK, h.toClientInfo(u))
}

// removeClient безвозвратно удаляет клиента: сначала рвёт его живые сессии (если
// он на линии), затем удаляет запись из хранилища. Временное отключение без
// удаления — отдельное действие (PATCH status=disabled).
func (h *handler) removeClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	// Рвём живые сессии до удаления, чтобы клиент сразу потерял доступ.
	if h.cfg.Disconnector != nil {
		h.cfg.Disconnector.DisconnectUser(r.Context(), id)
	}
	if err := h.cfg.Store.DeleteUser(r.Context(), id); err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getClientConnections отдаёт снимок живых соединений клиента (пусто, если
// клиент сейчас не подключён, или Connections не настроен).
func (h *handler) getClientConnections(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	if _, err := h.cfg.Store.UserByID(r.Context(), id); err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	if h.cfg.Connections == nil {
		writeJSON(w, http.StatusOK, []ConnectionInfo{})
		return
	}
	conns := h.cfg.Connections.UserConnections(id)
	out := make([]ConnectionInfo, 0, len(conns))
	for _, c := range conns {
		out = append(out, toConnectionInfo(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) restart(w http.ResponseWriter, _ *http.Request) {
	if h.cfg.Restart == nil {
		httpError(w, http.StatusNotImplemented, errors.New("restart not supported"))
		return
	}
	w.WriteHeader(http.StatusAccepted)
	// Отвечаем до фактического перезапуска: даём ответу уйти клиенту, прежде чем
	// сокет закроется вместе с сервером.
	restart := h.cfg.Restart
	go func() {
		time.Sleep(200 * time.Millisecond)
		restart()
	}()
}

// getLogs отдаёт последние записи лога сервера. ?limit=N ограничивает выдачу
// (по умолчанию — весь буфер).
func (h *handler) getLogs(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Logs == nil {
		writeJSON(w, http.StatusOK, []LogEntry{})
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries := h.cfg.Logs(limit)
	if entries == nil {
		entries = []LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// getStats отдаёт агрегатную статистику сервера для дашборда.
func (h *handler) getStats(w http.ResponseWriter, _ *http.Request) {
	if h.cfg.Stats == nil {
		writeJSON(w, http.StatusOK, ServerStats{})
		return
	}
	writeJSON(w, http.StatusOK, h.cfg.Stats())
}

// exportBackup стримит консистентный снимок БД как файл на скачивание.
func (h *handler) exportBackup(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Backup == nil {
		httpError(w, http.StatusNotImplemented, errors.New("backup not supported"))
		return
	}
	rc, size, err := h.cfg.Backup(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	defer rc.Close()
	name := fmt.Sprintf("bproxy-backup-%s.db", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	_, _ = io.Copy(w, rc)
}

// sqliteMagic — заголовок файла БД SQLite (16 байт, включая нулевой терминатор).
// Проверяем его перед импортом, чтобы не подсунуть серверу мусор вместо дампа.
var sqliteMagic = []byte("SQLite format 3\x00")

// importBackup принимает загруженный дамп БД (multipart, поле "backup") и
// применяет его через Config.Restore. Обычно Restore инициирует плавный
// перезапуск сервера, поэтому отвечаем 202 Accepted.
func (h *handler) importBackup(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Restore == nil {
		httpError(w, http.StatusNotImplemented, errors.New("restore not supported"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUpload)
	file, _, err := r.FormFile("backup")
	if err != nil {
		httpError(w, http.StatusBadRequest, fmt.Errorf("backup file required (multipart field %q): %w", "backup", err))
		return
	}
	defer file.Close()

	// Проверяем магию SQLite по первым байтам, не считывая весь файл в память.
	head := make([]byte, len(sqliteMagic))
	if _, err := io.ReadFull(file, head); err != nil {
		httpError(w, http.StatusBadRequest, errors.New("uploaded file too short to be a SQLite database"))
		return
	}
	if !bytes.Equal(head, sqliteMagic) {
		httpError(w, http.StatusBadRequest, errors.New("uploaded file is not a SQLite database"))
		return
	}
	// Восстанавливаем полный поток: уже прочитанный заголовок + остаток файла.
	full := io.MultiReader(bytes.NewReader(head), file)
	if err := h.cfg.Restore(r.Context(), full); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// keylinkBoards — доски, которые вкладываем в keylink клиента: все активные хабы
// store (при мульти-хабе клиент сам выберет доступную). Если store пуст, но в
// Config задана обслуживаемая доска (board-less старт с явным -board), кладём её.
func (h *handler) keylinkBoards(ctx context.Context) []string {
	var boards []string
	if hubs, err := h.cfg.Store.ListHubs(ctx); err == nil {
		for _, hb := range hubs {
			if hb.Status == store.HubActive {
				boards = append(boards, hb.ID)
			}
		}
	}
	if len(boards) == 0 && h.cfg.Board != "" {
		boards = []string{h.cfg.Board}
	}
	return boards
}

func (h *handler) listBoards(w http.ResponseWriter, r *http.Request) {
	hubs, err := h.cfg.Store.ListHubs(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]BoardInfo, 0, len(hubs))
	for _, hb := range hubs {
		out = append(out, toBoardInfo(hb))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) createBoard(w http.ResponseWriter, r *http.Request) {
	var req CreateBoardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if req.ID == "" {
		httpError(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	if req.MaxLanes == 0 {
		req.MaxLanes = 8
	}
	if req.MaxLanes < 1 || req.MaxLanes > 32 {
		httpError(w, http.StatusBadRequest, errors.New("max_lanes must be between 1 and 32"))
		return
	}
	hb, err := h.cfg.Store.UpsertHub(r.Context(), req.ID, req.Name, "")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if err := h.cfg.Store.SetHubMaxLanes(r.Context(), req.ID, req.MaxLanes); err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	hb.MaxLanes = req.MaxLanes
	writeJSON(w, http.StatusCreated, toBoardInfo(hb))
}

// getBoard ищет хаб по id. Отдельного метода "по одному" у store нет
// (не нужен нигде, кроме этой ручки) — фильтруем ListHubs, как это уже
// делает keylinkBoard.
func (h *handler) getBoard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hubs, err := h.cfg.Store.ListHubs(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	for _, hb := range hubs {
		if hb.ID == id {
			writeJSON(w, http.StatusOK, toBoardInfo(hb))
			return
		}
	}
	httpError(w, http.StatusNotFound, store.ErrNotFound)
}

// updateBoard переименовывает и/или меняет статус хаба.
func (h *handler) updateBoard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req UpdateBoardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			httpError(w, http.StatusBadRequest, errors.New("name must not be empty"))
			return
		}
		if err := h.cfg.Store.SetHubName(r.Context(), id, *req.Name); err != nil {
			httpError(w, statusForStore(err), err)
			return
		}
	}
	if req.Status != nil {
		status := store.HubStatus(*req.Status)
		if status != store.HubActive && status != store.HubDisabled {
			httpError(w, http.StatusBadRequest, fmt.Errorf("invalid status %q", *req.Status))
			return
		}
		if err := h.cfg.Store.SetHubStatus(r.Context(), id, status); err != nil {
			httpError(w, statusForStore(err), err)
			return
		}
	}
	if req.MaxLanes != nil {
		if *req.MaxLanes < 1 || *req.MaxLanes > 32 {
			httpError(w, http.StatusBadRequest, errors.New("max_lanes must be between 1 and 32"))
			return
		}
		if err := h.cfg.Store.SetHubMaxLanes(r.Context(), id, *req.MaxLanes); err != nil {
			httpError(w, statusForStore(err), err)
			return
		}
	}
	h.getBoard(w, r)
}

// removeBoard безвозвратно удаляет доску из хранилища. Живой хаб (если доска
// сейчас обслуживается) остановится при следующем перезапуске сервера.
// Временное отключение без удаления — отдельное действие (PATCH status=disabled).
func (h *handler) removeBoard(w http.ResponseWriter, r *http.Request) {
	if err := h.cfg.Store.DeleteHub(r.Context(), r.PathValue("id")); err != nil {
		httpError(w, statusForStore(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// toClientInfo собирает представление клиента для API: трафик — персистентный
// (store, завершённые сессии) плюс, если клиент сейчас на линии, живой из его
// текущих соединений — иначе после долгой сессии счётчик выглядел бы нулевым
// до самого отключения.
func (h *handler) toClientInfo(u store.User) ClientInfo {
	c := ClientInfo{
		ID:        u.ID,
		Name:      u.Name,
		Status:    string(u.Status),
		PublicKey: base64.StdEncoding.EncodeToString(u.PublicKey),
		CreatedAt: u.CreatedAt,
		RxBytes:   u.RxBytes,
		TxBytes:   u.TxBytes,
	}
	if !u.LastSeen.IsZero() {
		ls := u.LastSeen
		c.LastSeen = &ls
	}
	if h.cfg.Connections != nil {
		for _, conn := range h.cfg.Connections.UserConnections(u.ID) {
			c.RxBytes += conn.Received
			c.TxBytes += conn.Written
		}
	}
	return c
}

func toConnectionInfo(c hub.ConnectionInfo) ConnectionInfo {
	streams := make([]StreamInfo, 0, len(c.Streams))
	for _, st := range c.Streams {
		streams = append(streams, StreamInfo{
			ID:        st.ID,
			Target:    st.Target,
			Written:   st.Written,
			Received:  st.Received,
			StartedAt: st.StartedAt,
		})
	}
	lanes := make([]LaneInfo, 0, len(c.Lanes))
	for _, lane := range c.Lanes {
		lanes = append(lanes, LaneInfo{
			ID: uint32(lane.ID), Page: lane.Page, RTTMillis: lane.RTT.Milliseconds(),
		})
	}
	return ConnectionInfo{
		BundleID:  c.BundleID,
		LaneID:    uint32(c.LaneID),
		Epoch:     uint32(c.Epoch),
		Page:      c.Page,
		Lanes:     lanes,
		Written:   c.Written,
		Received:  c.Received,
		RTTMillis: c.RTT.Milliseconds(),
		Streams:   streams,
	}
}

func toBoardInfo(h store.Hub) BoardInfo {
	return BoardInfo{
		ID:        h.ID,
		Name:      h.Name,
		HubSlide:  h.HubSlide,
		Status:    string(h.Status),
		MaxLanes:  h.MaxLanes,
		CreatedAt: h.CreatedAt,
	}
}

func statusForStore(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
