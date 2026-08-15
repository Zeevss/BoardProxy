package hub

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/bond"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/handshake"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
)

// idleCheckInterval — как часто проверять простой клиентской страницы.
const idleCheckInterval = 30 * time.Second

const absoluteMaxBundleLanes = 32

const (
	maxConcurrentHello = 16
	hubCleanupTimeout  = 5 * time.Second
)

const (
	pageCleanupPasses  = 6
	pageCleanupDelay   = 150 * time.Millisecond
	pageCleanupBatch   = 128
	pageCleanupTimeout = 30 * time.Second
)

// Dialer создаёт новые гостевые сессии доски. Серверу нужна отдельная сессия
// (сокет) на каждую активную клиентскую страницу — по модели «одна сессия = один
// сокет на один слайд».
type Dialer interface {
	Join(ctx context.Context) (board.Session, error)
}

// User is authorization policy compiled from the current desired config.
type User struct {
	ID          string
	Name        string
	MaxSessions int
	MaxLanes    int
}

// UserDirectory is the narrow control-plane port required by the data plane.
// Its implementation is in-memory and session claims span every running hub.
type UserDirectory interface {
	Authorize(ctx context.Context, publicKey []byte, boardTag string) (User, error)
	AcquireSession(userID string) bool
	ReleaseSession(userID string, rx, tx uint64)
}

type SessionOpened struct {
	UserID   string
	BoardTag string
	BundleID string
}

type SessionClosed struct {
	UserID   string
	BoardTag string
	BundleID string
	RXBytes  uint64
	TXBytes  uint64
	Reason   string
}

type SessionObserver interface {
	SessionOpened(SessionOpened)
	SessionClosed(SessionClosed)
}

// ServerConfig конфигурирует хаб-сервер.
type ServerConfig struct {
	// HubSession — сессия observer'а на hub-слайде (уже присоединённая).
	HubSession board.Session
	// HubSlide — хэш hub-слайда.
	HubSlide string
	// Pool — хэши страниц для раздачи клиентам.
	Pool []string
	// Dialer создаёт серверные сессии для клиентских страниц.
	Dialer Dialer
	// ServerStatic — постоянная пара ключей сервера (клиенты знают её публичную
	// часть из keylink); ею сервер отвечает в рукопожатии Noise IK.
	ServerStatic crypto.Keypair
	// BoardTag is the stable config identifier used by user allowlists.
	BoardTag string
	// Users authorizes clients and enforces process-wide session limits.
	Users UserDirectory
	// Events receives low-volume lifecycle facts. Implementations must not
	// block the data plane; nil disables observation.
	Events SessionObserver
	Codec  codec.Codec
	Link   link.Options
	// MaxPayload, StreamWindow, CoalesceTarget и StreamIdleTimeout передаются
	// в серверные mux-сессии.
	MaxPayload      int
	StreamWindow    int
	MaxStreamWindow int
	// CoalesceTarget — 0 = полностью адаптивно (см. mux.Options.CoalesceTarget),
	// >0 = ручной потолок.
	CoalesceTarget    int
	StreamIdleTimeout time.Duration
	// IdleTimeout освобождает страницу клиента, от которого дольше этого времени
	// не было ни одного события. Живой клиент регулярно присылает link-heartbeat,
	// поэтому проверка отличает тихое соединение от оборванного даже при наличии
	// зависших открытых стримов. 0 отключает проверку.
	IdleTimeout time.Duration
	// MaxLanes limits pages in one logical bundle. Zero means eight.
	MaxLanes int
}

// Server — хаб: observer на hub-слайде, раздаёт страницы и поднимает серверную
// mux-сессию на каждую занятую страницу.
type Server struct {
	cfg      ServerConfig
	pool     *pagePool
	maxLanes atomic.Int64

	accept chan *mux.Session

	mu      sync.Mutex
	clients map[*mux.Session]*liveBundle
	// byUser — сессии, сгруппированные по id пользователя, чтобы «clients rm»
	// мог корректно оборвать живые соединения отключаемого клиента.
	byUser map[string]map[*mux.Session]bool
	// bundles is the v3 logical-connection index. Every bundle owns exactly one
	// mux session and one bond.Conn; its lanes are independent page sessions.
	bundles         map[bond.BundleID]*liveBundle
	bundleBySession map[*mux.Session]bond.BundleID
	bundleClaims    map[bond.BundleID]bool
	joinClaims      map[bond.BundleID]bool
	// processing — helloID, которые сейчас обрабатываются, для дедупликации
	// повторно доставленных доской HELLO-событий.
	processing map[string]bool
	helloSem   chan struct{}
	hubCleanup chan string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// connWG отдельно от wg считает горутины активных lanes и bundles. Close
	// дожидается их, чтобы runtime accounting был завершён до возврата.
	connWG sync.WaitGroup

	pageCleanupRuns        atomic.Uint64
	pageCleanupDeleted     atomic.Uint64
	pageCleanupFailures    atomic.Uint64
	pageCleanupQuarantined atomic.Uint64
}

type liveBundle struct {
	userID   string
	maxLanes int
	epoch    bond.Epoch
	token    bond.JoinToken
	bond     *bond.Conn
	mux      *mux.Session
	lanes    map[bond.LaneID]*liveLane
	nextID   bond.LaneID
}

type liveLane struct {
	id   bond.LaneID
	page string
	link *link.Link
}

// NewServer подписывает observer'а на hub-слайд и запускает rendezvous-цикл.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	snapshot, err := cfg.HubSession.Subscribe(ctx, cfg.HubSlide)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(ctx)
	pool := make([]string, 0, len(cfg.Pool))
	seenPages := make(map[string]bool, len(cfg.Pool))
	for _, page := range cfg.Pool {
		if page == "" || page == cfg.HubSlide || seenPages[page] {
			continue
		}
		seenPages[page] = true
		pool = append(pool, page)
	}
	maxLanes := cfg.MaxLanes
	if maxLanes <= 0 {
		maxLanes = 8
	}
	if maxLanes > absoluteMaxBundleLanes {
		maxLanes = absoluteMaxBundleLanes
	}
	s := &Server{
		cfg:             cfg,
		pool:            newPagePool(pool),
		accept:          make(chan *mux.Session, 16),
		clients:         make(map[*mux.Session]*liveBundle),
		byUser:          make(map[string]map[*mux.Session]bool),
		bundles:         make(map[bond.BundleID]*liveBundle),
		bundleBySession: make(map[*mux.Session]bond.BundleID),
		bundleClaims:    make(map[bond.BundleID]bool),
		joinClaims:      make(map[bond.BundleID]bool),
		processing:      make(map[string]bool),
		helloSem:        make(chan struct{}, maxConcurrentHello),
		hubCleanup:      make(chan string, 1024),
		ctx:             sctx,
		cancel:          cancel,
	}
	s.maxLanes.Store(int64(maxLanes))
	s.wg.Add(2)
	go s.rendezvousLoop()
	go s.hubCleanupLoop()
	// HELLO мог появиться, пока сервер был остановлен, но клиент ещё ждёт ответ.
	// Subscribe возвращает его только в snapshot, не в Events — обрабатываем оба
	// источника одним путём. Клиент удаляет HELLO при собственном timeout.
	for _, obj := range snapshot {
		s.processHubObject(obj)
	}
	return s, nil
}

// Accept возвращает следующую серверную mux-сессию (по одной на подключившегося
// клиента).
func (s *Server) Accept(ctx context.Context) (*mux.Session, error) {
	select {
	case m := <-s.accept:
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, link.ErrClosed
	}
}

// Close останавливает хаб, закрывает все клиентские сессии и hub-сессию.
// Закрытие идёт через mux.Session.Close (не link.Link.Close напрямую), чтобы
// каждый клиент получил GOAWAY и не ждал таймаута оборванного соединения.
func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	sessions := make([]*mux.Session, 0, len(s.clients))
	for m := range s.clients {
		sessions = append(sessions, m)
	}
	s.clients = nil
	s.mu.Unlock()
	rvLog(s.cfg.Link.Log).Info("hub: graceful shutdown started", "clients", len(sessions))
	for _, m := range sessions {
		_ = m.Close()
	}
	// Сначала дожидаемся rendezvousLoop и всех уже запущенных handleHello:
	// каждый handleHello ещё может создать watchClient и сделать connWG.Add.
	// Только после этого безопасно вызывать connWG.Wait (Add одновременно с
	// Wait при нулевом счётчике является гонкой и мог оставить страницу занятой).
	s.wg.Wait()
	// Complete lane cleanup and in-memory traffic accounting before returning.
	s.connWG.Wait()
	return s.cfg.HubSession.Close()
}

// ServerStats — агрегат нагрузки сервера для метрик.
type ServerStats struct {
	Clients                int    // активных клиентских сессий
	FreePages              int    // свободных страниц в пуле
	Written                uint64 // всего байт отправлено клиентам (client downloads)
	Received               uint64 // всего байт получено от клиентов (client uploads)
	PageCleanupRuns        uint64
	PageCleanupDeleted     uint64
	PageCleanupFailures    uint64
	PageCleanupQuarantined uint64
}

// Stats собирает агрегат нагрузки по всем активным клиентским сессиям.
func (s *Server) Stats() ServerStats {
	s.mu.Lock()
	sessions := make([]*mux.Session, 0, len(s.clients))
	for m := range s.clients {
		sessions = append(sessions, m)
	}
	s.mu.Unlock()

	st := ServerStats{
		Clients: len(sessions), FreePages: s.pool.available(),
		PageCleanupRuns:        s.pageCleanupRuns.Load(),
		PageCleanupDeleted:     s.pageCleanupDeleted.Load(),
		PageCleanupFailures:    s.pageCleanupFailures.Load(),
		PageCleanupQuarantined: s.pageCleanupQuarantined.Load(),
	}
	for _, m := range sessions {
		ms := m.Stats()
		st.Written += ms.Written
		st.Received += ms.Received
	}
	return st
}

// SetMaxLanes updates the cap for newly created bundles. Existing bundles keep
// the limit negotiated in their authenticated assignment.
func (s *Server) SetMaxLanes(max int) {
	if max < 1 {
		max = 1
	}
	if max > absoluteMaxBundleLanes {
		max = absoluteMaxBundleLanes
	}
	s.maxLanes.Store(int64(max))
}

// StreamInfo — снимок одного открытого стрима внутри соединения клиента.
type StreamInfo struct {
	ID        uint32
	Target    string // "host:port", запрошенный клиентом при открытии стрима
	Written   uint64 // байт отправлено клиенту (download этого стрима)
	Received  uint64 // байт получено от клиента (upload этого стрима)
	StartedAt time.Time
}

// ConnectionInfo — снимок одного живого соединения (mux-сессии) клиента: на
// какой странице оно поднято, суммарный трафик и открытые сейчас стримы.
// Обычно у клиента одно такое соединение; больше одного бывает кратко при
// реконнекте, пока прежняя страница ещё не освобождена по IdleTimeout.
type ConnectionInfo struct {
	BundleID string
	LaneID   bond.LaneID
	Epoch    bond.Epoch
	Page     string
	Lanes    []LaneInfo
	Written  uint64
	Received uint64
	RTT      time.Duration
	Streams  []StreamInfo
}

type LaneInfo struct {
	ID   bond.LaneID
	Page string
	RTT  time.Duration
}

// UserConnections возвращает снимок всех живых соединений пользователя userID
// (для управления — просмотр активных подключений клиента). Пустой список —
// клиент сейчас не подключён.
func (s *Server) UserConnections(userID string) []ConnectionInfo {
	s.mu.Lock()
	sessions := make([]*mux.Session, 0, len(s.byUser[userID]))
	bundleIDs := make(map[*mux.Session]bond.BundleID, len(s.byUser[userID]))
	epochs := make(map[*mux.Session]bond.Epoch, len(s.byUser[userID]))
	lanes := make(map[*mux.Session][]LaneInfo, len(s.byUser[userID]))
	for m := range s.byUser[userID] {
		sessions = append(sessions, m)
		bundleID := s.bundleBySession[m]
		bundleIDs[m] = bundleID
		if bundle := s.clients[m]; bundle != nil {
			epochs[m] = bundle.epoch
			for _, lane := range bundle.lanes {
				lanes[m] = append(lanes[m], LaneInfo{
					ID: lane.id, Page: lane.page, RTT: lane.link.RTT(),
				})
			}
			sort.Slice(lanes[m], func(i, j int) bool {
				return lanes[m][i].ID < lanes[m][j].ID
			})
		}
	}
	s.mu.Unlock()

	out := make([]ConnectionInfo, 0, len(sessions))
	for _, m := range sessions {
		ms := m.Stats()
		bundleID := ""
		if !bundleIDs[m].IsZero() {
			bundleID = bundleIDs[m].String()
		}
		var primary LaneInfo
		if len(lanes[m]) > 0 {
			primary = lanes[m][0]
		}
		ci := ConnectionInfo{
			BundleID: bundleID,
			LaneID:   primary.ID,
			Epoch:    epochs[m],
			Page:     primary.Page,
			Lanes:    lanes[m],
			Written:  ms.Written,
			Received: ms.Received,
			RTT:      ms.RTT,
			Streams:  make([]StreamInfo, 0, len(ms.PerStream)),
		}
		for _, st := range ms.PerStream {
			ci.Streams = append(ci.Streams, StreamInfo{
				ID:        st.ID,
				Target:    st.Target,
				Written:   st.Written,
				Received:  st.Received,
				StartedAt: st.StartedAt,
			})
		}
		out = append(out, ci)
	}
	return out
}

// claimHello помечает helloID как обрабатываемый; false — уже обрабатывается
// (дубликат события), обрабатывать повторно не нужно.
func (s *Server) claimHello(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processing[id] {
		return false
	}
	s.processing[id] = true
	return true
}

func (s *Server) releaseHello(id string) {
	s.mu.Lock()
	delete(s.processing, id)
	s.mu.Unlock()
}

func (s *Server) claimNewBundle(id bond.BundleID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bundles[id] != nil || s.bundleClaims[id] {
		return false
	}
	s.bundleClaims[id] = true
	return true
}

func (s *Server) releaseBundleClaim(id bond.BundleID) {
	s.mu.Lock()
	delete(s.bundleClaims, id)
	s.mu.Unlock()
}

func (s *Server) claimJoin(userID string, req bundleRequest) (*liveBundle, bond.LaneID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bundles[req.id]
	if b == nil || b.userID != userID || b.epoch != req.epoch ||
		!b.token.Equal(req.token) || s.joinClaims[req.id] ||
		len(b.lanes) >= b.maxLanes {
		return nil, 0, false
	}
	s.joinClaims[req.id] = true
	laneID := b.nextID
	if laneID < bond.FirstLane+1 {
		laneID = bond.FirstLane + 1
	}
	b.nextID = laneID + 1
	return b, laneID, true
}

func (s *Server) releaseJoinClaim(id bond.BundleID) {
	s.mu.Lock()
	delete(s.joinClaims, id)
	s.mu.Unlock()
}

func (s *Server) rendezvousLoop() {
	defer s.wg.Done()
	events := s.cfg.HubSession.Events()
	for {
		select {
		case <-s.ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				rvLog(s.cfg.Link.Log).Error(
					"hub: control session closed unexpectedly; stopping hub",
					"slide", s.cfg.HubSlide,
				)
				s.cancel()
				return
			}
			if ev.Kind != board.Created {
				continue
			}
			s.processHubObject(ev.Object)
		}
	}
}

func (s *Server) processHubObject(obj board.Object) {
	if obj.CreatorHash == s.cfg.HubSession.Participant() {
		return
	}
	frame, err := s.cfg.Codec.Decode(obj.Value)
	if err != nil {
		s.enqueueHubCleanup(obj.ID)
		return
	}
	m, ok := decodeRV(frame)
	if !ok || m.kind != rvHello {
		s.enqueueHubCleanup(obj.ID)
		return
	}
	// Дедупликация: snapshot и live events, а также сама доска, могут доставить
	// один HELLO повторно. Параллельная обработка заняла бы несколько страниц.
	if !s.claimHello(obj.ID) {
		return
	}
	select {
	case s.helloSem <- struct{}{}:
	case <-s.ctx.Done():
		s.releaseHello(obj.ID)
		return
	default:
		// Keep the rendezvous reader bounded under a HELLO flood. The client owns
		// timeout/retry and removes its HELLO; a later event/reconnect can retry.
		s.releaseHello(obj.ID)
		s.enqueueHubCleanup(obj.ID)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.helloSem }()
		s.handleHello(m.nonce, m.body, obj.ID)
	}()
}

func (s *Server) enqueueHubCleanup(id string) {
	if id == "" {
		return
	}
	select {
	case s.hubCleanup <- id:
	default:
		rvLog(s.cfg.Link.Log).Warn("hub: cleanup queue full", "object", id)
	}
}

func (s *Server) hubCleanupLoop() {
	defer s.wg.Done()
	for {
		select {
		case id := <-s.hubCleanup:
			s.deleteHubObject(id)
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Server) deleteHubObject(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), hubCleanupTimeout)
	defer cancel()
	if err := s.cfg.HubSession.Delete(ctx, id); err != nil && s.ctx.Err() == nil {
		rvLog(s.cfg.Link.Log).Warn("hub: failed to remove rendezvous object", "object", id, "err", err)
	}
}

// scrubPage removes every object before a page changes owner. Two consecutive
// empty snapshots are required so a late write from the previous participant
// cannot slip between cleanup and returning the page to the allocator.
func scrubPage(ctx context.Context, sess board.Session, page string, initial []board.Object) (int, error) {
	deleted := 0
	emptyPasses := 0
	snapshot := initial
	for pass := 0; pass < pageCleanupPasses; pass++ {
		if pass > 0 {
			t := time.NewTimer(pageCleanupDelay)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return deleted, ctx.Err()
			}
			var err error
			snapshot, err = sess.Subscribe(ctx, page)
			if err != nil {
				return deleted, err
			}
		}
		if len(snapshot) == 0 {
			emptyPasses++
			if emptyPasses == 2 {
				return deleted, nil
			}
			continue
		}
		emptyPasses = 0
		for start := 0; start < len(snapshot); start += pageCleanupBatch {
			end := min(start+pageCleanupBatch, len(snapshot))
			ids := make([]string, 0, end-start)
			for _, obj := range snapshot[start:end] {
				if obj.ID != "" {
					ids = append(ids, obj.ID)
				}
			}
			if len(ids) > 0 {
				if err := sess.Delete(ctx, ids...); err != nil {
					return deleted, err
				}
				deleted += len(ids)
			}
		}
	}
	return deleted, fmt.Errorf("page remained non-empty after %d cleanup passes", pageCleanupPasses)
}

func (s *Server) recordPageCleanup(deleted int, err error) {
	s.pageCleanupRuns.Add(1)
	s.pageCleanupDeleted.Add(uint64(deleted))
	if err != nil {
		s.pageCleanupFailures.Add(1)
	}
}

// releasePage closes the ownership gap with a fresh board participant. A page
// is returned to the allocator only after it remains empty; a failed cleanup
// quarantines it in the busy set instead of exposing the next user to stale
// ciphertext or a still-writing previous client.
func (s *Server) releasePage(page string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), pageCleanupTimeout)
	defer cancel()
	sess, err := s.cfg.Dialer.Join(ctx)
	if err != nil {
		s.recordPageCleanup(0, err)
		s.pageCleanupQuarantined.Add(1)
		rvLog(s.cfg.Link.Log).Error("hub: page cleanup join failed; page quarantined",
			"page", page, "err", err)
		return false
	}
	defer sess.Close()
	snapshot, err := sess.Subscribe(ctx, page)
	deleted := 0
	if err == nil {
		deleted, err = scrubPage(ctx, sess, page, snapshot)
	}
	s.recordPageCleanup(deleted, err)
	if err != nil {
		s.pageCleanupQuarantined.Add(1)
		rvLog(s.cfg.Link.Log).Error("hub: page cleanup failed; page quarantined",
			"page", page, "deleted_objects", deleted, "err", err)
		return false
	}
	s.pool.release(page)
	rvLog(s.cfg.Link.Log).Info("hub: page cleaned and released",
		"page", page, "deleted_objects", deleted, "free_pages", s.pool.available())
	return true
}

// handleHello проводит рукопожатие с клиентом, авторизует его и, если всё
// сошлось, поднимает серверную сессию на выданной странице. При любом отказе
// отвечает единым DENIED (истинная причина — в логе), чтобы не давать оракул.
func (s *Server) handleHello(nonce [nonceLen]byte, msg1 []byte, helloID string) {
	// HELLO потребляем в любом случае; метку обработки снимаем последней (после
	// удаления объекта), чтобы между снятием метки и удалением не проскочил
	// повторно доставленный дубль.
	defer s.releaseHello(helloID)
	defer s.deleteHubObject(helloID)
	log := rvLog(s.cfg.Link.Log)
	hello, ok := decodeHello(msg1)
	if !ok {
		log.Warn("hub: protocol version mismatch")
		s.putRV(encodeDenied(nonce))
		return
	}
	version, ok := negotiateVersion(hello)
	if !ok || (hello.legacy && version != 2) {
		log.Warn("hub: no compatible protocol version",
			"client_min", hello.minVersion,
			"client_max", hello.maxVersion,
		)
		s.putRV(encodeDenied(nonce))
		return
	}

	// Обрабатываем msg1 и узнаём личность клиента (криптографически
	// подтверждённую его статическим ключом).
	resp, err := handshake.Respond(s.cfg.ServerStatic, hello.msg1)
	if err != nil {
		log.Warn("hub: рукопожатие отклонено", "err", err)
		s.putRV(encodeDenied(nonce))
		return
	}
	user, err := s.cfg.Users.Authorize(s.ctx, resp.PeerStatic(), s.cfg.BoardTag)
	if err != nil {
		log.Warn("hub: клиент не авторизован", "err", err)
		s.putRV(encodeDenied(nonce))
		return
	}

	var (
		bundleID    bond.BundleID
		bundleToken bond.JoinToken
		bundleLane  = bond.FirstLane
		bundleEpoch = bond.FirstEpoch
		existing    *liveBundle
		isJoin      bool
	)
	if version >= 3 {
		req, valid := decodeBundleRequest(resp.Payload())
		if !valid {
			log.Warn("hub: invalid v3 bundle request", "user", user.Name)
			s.putRV(encodeDenied(nonce))
			return
		}
		bundleID = req.id
		switch req.kind {
		case bundleRequestNew:
			if !s.claimNewBundle(bundleID) {
				log.Warn("hub: duplicate bundle id", "user", user.Name, "bundle", bundleID.String())
				s.putRV(encodeDenied(nonce))
				return
			}
			defer s.releaseBundleClaim(bundleID)
			bundleToken, err = bond.NewJoinToken()
			if err != nil {
				log.Warn("hub: failed to create bundle token", "err", err)
				s.putRV(encodeDenied(nonce))
				return
			}
		case bundleRequestJoin:
			existing, bundleLane, ok = s.claimJoin(user.ID, req)
			if !ok {
				log.Warn("hub: bundle join rejected", "user", user.Name, "bundle", bundleID.String())
				s.putRV(encodeDenied(nonce))
				return
			}
			defer s.releaseJoinClaim(bundleID)
			isJoin = true
			bundleToken = existing.token
			bundleEpoch = existing.epoch
		default:
			log.Warn("hub: unsupported bundle request", "user", user.Name)
			s.putRV(encodeDenied(nonce))
			return
		}
	}
	sessionAcquired := false
	if !isJoin {
		if !s.cfg.Users.AcquireSession(user.ID) {
			log.Warn("hub: per-user session limit reached", "user", user.Name,
				"limit", user.MaxSessions)
			s.putRV(encodeDenied(nonce))
			return
		}
		sessionAcquired = true
		defer func() {
			if sessionAcquired {
				s.cfg.Users.ReleaseSession(user.ID, 0, 0)
			}
		}()
	}
	bundleMaxLanes := int(s.maxLanes.Load())
	if user.MaxLanes > 0 && user.MaxLanes < bundleMaxLanes {
		bundleMaxLanes = user.MaxLanes
	}
	if isJoin {
		bundleMaxLanes = existing.maxLanes
	}

	page, ok := s.pool.acquire()
	if !ok {
		log.Warn("hub: пул страниц исчерпан", "user", user.Name)
		s.putRV(encodeDenied(nonce))
		return
	}

	// Завершаем рукопожатие: v2 несёт только страницу, v3 — аутентифицированные
	// bundle/lane credentials плюс страницу. Секрет join token никогда не
	// выходит из зашифрованного Noise payload.
	responsePayload := []byte(page)
	if version >= 3 {
		var valid bool
		responsePayload, valid = encodeBundleAssignment(bundleAssignment{
			id:       bundleID,
			lane:     bundleLane,
			epoch:    bundleEpoch,
			token:    bundleToken,
			maxLanes: uint8(bundleMaxLanes),
			page:     page,
		}, version)
		if !valid {
			log.Warn("hub: failed to encode bundle assignment", "bundle", bundleID.String())
			s.releasePage(page)
			s.putRV(encodeDenied(nonce))
			return
		}
	}
	keys, msg2, err := resp.Accept(responsePayload)
	if err != nil {
		log.Warn("hub: не удалось завершить рукопожатие", "err", err)
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	sealed, err := crypto.NewSealed(s.cfg.Codec, keys.Send, keys.Recv)
	if err != nil {
		log.Warn("hub: не удалось собрать sealed-кодек", "err", err)
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}

	sess, err := s.cfg.Dialer.Join(s.ctx)
	if err != nil {
		log.Warn("hub: не удалось поднять серверную сессию доски", "err", err, "user", user.Name)
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	log.Info("hub: server session for client", "user", user.Name, "participant", sess.Participant(), "page", page)
	snapshot, err := sess.Subscribe(s.ctx, page)
	if err != nil {
		log.Warn("hub: не удалось подписаться на страницу", "err", err, "page", page)
		_ = sess.Close()
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	deleted, err := scrubPage(s.ctx, sess, page, snapshot)
	s.recordPageCleanup(deleted, err)
	if err != nil {
		log.Error("hub: page cleanup before assignment failed", "err", err,
			"page", page, "deleted_objects", deleted)
		_ = sess.Close()
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	if deleted > 0 {
		log.Info("hub: stale page objects removed before assignment",
			"page", page, "deleted_objects", deleted)
	}
	// Ownership starts from an intentionally empty page. Reconcile remains in
	// the setup path for reconnect snapshots, but there is no prior-user state
	// to replay into this new encrypted link.
	snapshot = nil

	l := link.New(sess, sealed, laneLinkOptions(s.cfg.Link, bundleID, bundleLane))
	var (
		m          *mux.Session
		bundleConn *bond.Conn
		bundle     *liveBundle
	)
	if version >= 3 {
		if isJoin {
			bundle = existing
			bundleConn = existing.bond
		} else {
			bundleConn = bond.New(bond.Options{Ordered: version < 4})
		}
		if err := bundleConn.AddLane(bundleLane, l); err != nil {
			log.Warn("hub: failed to attach bundle lane", "err", err,
				"bundle", bundleID.String(), "lane", bundleLane)
			_ = l.Close()
			s.releasePage(page)
			s.putRV(encodeDenied(nonce))
			return
		}
		if isJoin {
			m = existing.mux
		} else {
			m = mux.New(bundleConn, mux.Options{
				Version:           int(version),
				MaxPayload:        s.cfg.MaxPayload,
				StreamWindow:      s.cfg.StreamWindow,
				MaxStreamWindow:   s.cfg.MaxStreamWindow,
				CoalesceTarget:    s.cfg.CoalesceTarget,
				StreamIdleTimeout: s.cfg.StreamIdleTimeout,
			})
			bundle = &liveBundle{
				userID:   user.ID,
				maxLanes: bundleMaxLanes,
				epoch:    bundleEpoch,
				token:    bundleToken,
				bond:     bundleConn,
				mux:      m,
				lanes:    make(map[bond.LaneID]*liveLane),
				nextID:   bond.FirstLane + 1,
			}
		}
	} else {
		m = mux.New(l, mux.Options{
			Version:           int(version),
			MaxPayload:        s.cfg.MaxPayload,
			StreamWindow:      s.cfg.StreamWindow,
			MaxStreamWindow:   s.cfg.MaxStreamWindow,
			CoalesceTarget:    s.cfg.CoalesceTarget,
			StreamIdleTimeout: s.cfg.StreamIdleTimeout,
		})
		bundle = &liveBundle{
			userID:   user.ID,
			maxLanes: 1,
			mux:      m,
			lanes:    make(map[bond.LaneID]*liveLane),
			nextID:   bond.FirstLane + 1,
		}
	}
	// Страницы пула переиспользуются между клиентами; если предыдущий сеанс на
	// этой странице завершился не идеально чисто (например, обрыв до того, как
	// его объекты были подтверждены/удалены), в снапшоте окажутся чужие
	// объекты — они не придут через Events(), нужен Reconcile. Важен порядок:
	// mux.New — до Reconcile, иначе он проиграет снапшот на горутине run() и
	// может заблокироваться, отправляя payload'ы в recvCh, который пока некому
	// разбирать (это и делает reader() мультиплексора).
	if err := l.Reconcile(s.ctx, snapshot); err != nil {
		log.Warn("hub: reconcile страницы не удался", "err", err, "page", page)
		if isJoin {
			bundleConn.RemoveLane(bundleLane)
		} else {
			_ = m.Close()
		}
		s.releasePage(page)
		s.putRV(encodeDenied(nonce))
		return
	}

	lane := &liveLane{id: bundleLane, page: page, link: l}
	// Publish ASSIGN before committing the lane to the live indexes. A failed
	// hub write must roll back immediately instead of reserving a page for a
	// client that never received its assignment.
	if err := s.putRV(encodeAssign(nonce, version, hello.legacy, msg2)); err != nil {
		log.Warn("hub: failed to publish assignment", "user", user.Name, "page", page, "err", err)
		if isJoin {
			bundleConn.RemoveLane(bundleLane)
		} else {
			_ = m.Close()
		}
		s.releasePage(page)
		return
	}
	s.mu.Lock()
	if s.clients == nil { // сервер уже закрывается
		s.mu.Unlock()
		if isJoin {
			bundleConn.RemoveLane(bundleLane)
		} else {
			_ = m.Close()
		}
		s.releasePage(page)
		return
	}
	if isJoin {
		if s.bundles[bundleID] != bundle || s.clients[m] != bundle {
			s.mu.Unlock()
			bundleConn.RemoveLane(bundleLane)
			s.releasePage(page)
			s.putRV(encodeDenied(nonce))
			return
		}
		bundle.lanes[bundleLane] = lane
	} else {
		bundle.lanes[bundleLane] = lane
		s.clients[m] = bundle
		if s.byUser[user.ID] == nil {
			s.byUser[user.ID] = make(map[*mux.Session]bool)
		}
		s.byUser[user.ID][m] = true
		if version >= 3 {
			s.bundles[bundleID] = bundle
			s.bundleBySession[m] = bundleID
		}
	}
	s.mu.Unlock()
	if !isJoin {
		// The bundle watcher now owns the process-wide session claim.
		sessionAcquired = false
		if s.cfg.Events != nil {
			s.cfg.Events.SessionOpened(SessionOpened{
				UserID: user.ID, BoardTag: s.cfg.BoardTag, BundleID: bundleID.String(),
			})
		}
	}

	// Every lane owns and releases its own page. Losing one lane only removes it
	// from the bond; the mux remains alive while another lane exists.
	s.connWG.Add(1)
	go s.watchLane(bundle, lane)

	if !isJoin {
		s.connWG.Add(1)
		go s.watchBundle(bundleID, bundle)
		select {
		case s.accept <- m:
		case <-s.ctx.Done():
		}
	}
}

// watchLane releases exactly one page. A failed or idle lane is detached from
// the bond without touching the mux while another lane remains.
func (s *Server) watchLane(bundle *liveBundle, lane *liveLane) {
	defer s.connWG.Done()
	reason := "lane_closed"
	if s.cfg.IdleTimeout > 0 {
		tick := s.cfg.IdleTimeout / 3
		if tick < 20*time.Millisecond {
			tick = 20 * time.Millisecond
		}
		if tick > idleCheckInterval {
			tick = idleCheckInterval
		}
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
	loop:
		for {
			select {
			case <-lane.link.Done():
				break loop
			case <-bundle.mux.Done():
				reason = "bundle_closed"
				break loop
			case <-s.ctx.Done():
				reason = "server_shutdown"
				// Server.Close cancels the rendezvous context before flushing
				// GOAWAY. Do not detach the lane underneath mux.Close: wait
				// until the logical session has completed its flush.
				<-bundle.mux.Done()
				break loop
			case <-ticker.C:
				// Открытые стримы не доказывают, что клиент жив: при аварийном
				// завершении процесса они как раз остаются в mux до RESET. Живость
				// подтверждает link-heartbeat, который обновляет LastActivity.
				if time.Since(lane.link.LastActivity()) > s.cfg.IdleTimeout {
					reason = "peer_timeout"
					break loop
				}
			}
		}
	} else {
		select {
		case <-lane.link.Done():
		case <-bundle.mux.Done():
			reason = "bundle_closed"
		case <-s.ctx.Done():
			reason = "server_shutdown"
			<-bundle.mux.Done()
		}
	}

	s.mu.Lock()
	if bundle.lanes[lane.id] != lane {
		s.mu.Unlock()
		return
	}
	delete(bundle.lanes, lane.id)
	last := len(bundle.lanes) == 0
	s.mu.Unlock()

	if bundle.bond != nil {
		bundle.bond.RemoveLane(lane.id)
	} else {
		_ = lane.link.Close()
	}
	released := s.releasePage(lane.page)
	rvLog(s.cfg.Link.Log).Info("hub: client lane released",
		"user", bundle.userID, "page", lane.page, "lane", lane.id,
		"reason", reason, "remaining_lanes", bundleLaneCount(s, bundle),
		"released", released, "free_pages", s.pool.available())
	if last {
		_ = bundle.mux.Close()
	}
}

func bundleLaneCount(s *Server, bundle *liveBundle) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(bundle.lanes)
}

// watchBundle owns logical-session bookkeeping and reports since-start traffic
// exactly once, independently of how many lane pages were attached.
func (s *Server) watchBundle(bundleID bond.BundleID, bundle *liveBundle) {
	defer s.connWG.Done()
	reason := "session_closed"
	select {
	case <-bundle.mux.Done():
	case <-s.ctx.Done():
		reason = "server_shutdown"
		_ = bundle.mux.Close()
		<-bundle.mux.Done()
	}

	final := bundle.mux.Stats()
	s.mu.Lock()
	if s.clients != nil {
		delete(s.clients, bundle.mux)
	}
	if sessions := s.byUser[bundle.userID]; sessions != nil {
		delete(sessions, bundle.mux)
		if len(sessions) == 0 {
			delete(s.byUser, bundle.userID)
		}
	}
	delete(s.bundleBySession, bundle.mux)
	if !bundleID.IsZero() && s.bundles[bundleID] == bundle {
		delete(s.bundles, bundleID)
	}
	s.mu.Unlock()

	s.cfg.Users.ReleaseSession(bundle.userID, final.Received, final.Written)
	if s.cfg.Events != nil {
		s.cfg.Events.SessionClosed(SessionClosed{
			UserID: bundle.userID, BoardTag: s.cfg.BoardTag, BundleID: bundleID.String(),
			RXBytes: final.Received, TXBytes: final.Written, Reason: reason,
		})
	}
}

// DisconnectUser корректно закрывает все живые сессии пользователя (каждая шлёт
// GOAWAY) — используется при отключении клиента через управление. Возвращает
// число закрытых сессий. Освобождение страниц и чистку трекинга доделает
// watchClient каждой сессии, проснувшись на её закрытии.
func (s *Server) DisconnectUser(_ context.Context, userID string) int {
	s.mu.Lock()
	var sessions []*mux.Session
	for m := range s.byUser[userID] {
		sessions = append(sessions, m)
	}
	s.mu.Unlock()
	for _, m := range sessions {
		go m.Close()
	}
	return len(sessions)
}

// putRV кладёт rendezvous-объект на hub-страницу.
func (s *Server) putRV(body []byte) error {
	value, err := s.cfg.Codec.Encode(body)
	if err != nil {
		return err
	}
	return s.cfg.HubSession.Put(s.ctx, board.Object{ID: board.NewID(), Value: value})
}
