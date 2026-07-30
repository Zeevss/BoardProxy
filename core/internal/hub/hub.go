package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/bond"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/handshake"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/store"
)

// idleCheckInterval — как часто проверять простой клиентской страницы.
const idleCheckInterval = 30 * time.Second

const absoluteMaxBundleLanes = 16

// Dialer создаёт новые гостевые сессии доски. Серверу нужна отдельная сессия
// (сокет) на каждую активную клиентскую страницу — по модели «одна сессия = один
// сокет на один слайд».
type Dialer interface {
	Join(ctx context.Context) (board.Session, error)
}

// UserStore — то, что hub'у нужно от хранилища: поиск заранее предоставленного
// пользователя по его публичному ключу для авторизации (ErrNotFound здесь и
// есть отказ в доступе) и накопление трафика при закрытии его сессии. Узкий
// интерфейс намеренно — не полный store.Store.
type UserStore interface {
	UserByPublicKey(ctx context.Context, pubKey []byte) (store.User, error)
	// AddUserTraffic добавляет rx/tx к накопленному трафику пользователя,
	// вызывается при закрытии его сессии с её финальными байтами.
	AddUserTraffic(ctx context.Context, userID int64, rx, tx uint64) error
	// TouchUser отмечает last_seen пользователя, вызывается при успешной
	// авторизации подключившегося клиента (best-effort).
	TouchUser(ctx context.Context, userID int64) error
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
	// Users авторизует подключающихся клиентов по их публичному ключу.
	Users UserStore
	Codec codec.Codec
	Link  link.Options
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
	// MaxLanes limits pages in one logical bundle. Zero means four.
	MaxLanes int
}

// Server — хаб: observer на hub-слайде, раздаёт страницы и поднимает серверную
// mux-сессию на каждую занятую страницу.
type Server struct {
	cfg      ServerConfig
	pool     *pagePool
	maxLanes int

	accept chan *mux.Session

	mu      sync.Mutex
	clients map[*mux.Session]*liveBundle
	// byUser — сессии, сгруппированные по id пользователя, чтобы «clients rm»
	// мог корректно оборвать живые соединения отключаемого клиента.
	byUser map[int64]map[*mux.Session]bool
	// bundles is the v3 logical-connection index. Every bundle owns exactly one
	// mux session and one bond.Conn; its lanes are independent page sessions.
	bundles         map[bond.BundleID]*liveBundle
	bundleBySession map[*mux.Session]bond.BundleID
	bundleClaims    map[bond.BundleID]bool
	joinClaims      map[bond.BundleID]bool
	// processing — helloID, которые сейчас обрабатываются, для дедупликации
	// повторно доставленных доской HELLO-событий.
	processing map[string]bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// connWG отдельно от wg считает горутины watchClient (по одной на клиентскую
	// сессию): Close должен дождаться их завершения, чтобы персист трафика
	// (AddUserTraffic) успел записаться в store до того, как вызывающий его
	// закроет (см. app.RunServer, где store.Close() идёт после srv.Close()).
	connWG sync.WaitGroup
}

type liveBundle struct {
	userID int64
	epoch  bond.Epoch
	token  bond.JoinToken
	bond   *bond.Conn
	mux    *mux.Session
	lanes  map[bond.LaneID]*liveLane
	nextID bond.LaneID
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
	maxLanes := cfg.MaxLanes
	if maxLanes <= 0 {
		maxLanes = 4
	}
	if maxLanes > absoluteMaxBundleLanes {
		maxLanes = absoluteMaxBundleLanes
	}
	s := &Server{
		cfg:             cfg,
		pool:            newPagePool(cfg.Pool),
		maxLanes:        maxLanes,
		accept:          make(chan *mux.Session, 16),
		clients:         make(map[*mux.Session]*liveBundle),
		byUser:          make(map[int64]map[*mux.Session]bool),
		bundles:         make(map[bond.BundleID]*liveBundle),
		bundleBySession: make(map[*mux.Session]bond.BundleID),
		bundleClaims:    make(map[bond.BundleID]bool),
		joinClaims:      make(map[bond.BundleID]bool),
		processing:      make(map[string]bool),
		ctx:             sctx,
		cancel:          cancel,
	}
	s.wg.Add(1)
	go s.rendezvousLoop()
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
	// connWG — прежде чем закрыть store (это делает вызывающий сразу после
	// Close): дожидаемся, чтобы персист трафика каждой сессии (watchClient)
	// успел записаться, а не оборвался на закрытой БД.
	s.connWG.Wait()
	return s.cfg.HubSession.Close()
}

// ServerStats — агрегат нагрузки сервера для метрик.
type ServerStats struct {
	Clients   int    // активных клиентских сессий
	FreePages int    // свободных страниц в пуле
	Written   uint64 // всего байт отправлено клиентам (client downloads)
	Received  uint64 // всего байт получено от клиентов (client uploads)
}

// Stats собирает агрегат нагрузки по всем активным клиентским сессиям.
func (s *Server) Stats() ServerStats {
	s.mu.Lock()
	sessions := make([]*mux.Session, 0, len(s.clients))
	for m := range s.clients {
		sessions = append(sessions, m)
	}
	s.mu.Unlock()

	st := ServerStats{Clients: len(sessions), FreePages: s.pool.available()}
	for _, m := range sessions {
		ms := m.Stats()
		st.Written += ms.Written
		st.Received += ms.Received
	}
	return st
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
func (s *Server) UserConnections(userID int64) []ConnectionInfo {
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

func (s *Server) claimJoin(userID int64, req bundleRequest) (*liveBundle, bond.LaneID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bundles[req.id]
	if b == nil || b.userID != userID || b.epoch != req.epoch ||
		!b.token.Equal(req.token) || s.joinClaims[req.id] ||
		len(b.lanes) >= s.maxLanes {
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
		return
	}
	m, ok := decodeRV(frame)
	if !ok || m.kind != rvHello {
		return
	}
	// Дедупликация: snapshot и live events, а также сама доска, могут доставить
	// один HELLO повторно. Параллельная обработка заняла бы несколько страниц.
	if !s.claimHello(obj.ID) {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleHello(m.nonce, m.body, obj.ID)
	}()
}

// handleHello проводит рукопожатие с клиентом, авторизует его и, если всё
// сошлось, поднимает серверную сессию на выданной странице. При любом отказе
// отвечает единым DENIED (истинная причина — в логе), чтобы не давать оракул.
func (s *Server) handleHello(nonce [nonceLen]byte, msg1 []byte, helloID string) {
	// HELLO потребляем в любом случае; метку обработки снимаем последней (после
	// удаления объекта), чтобы между снятием метки и удалением не проскочил
	// повторно доставленный дубль.
	defer s.releaseHello(helloID)
	defer s.cfg.HubSession.Delete(s.ctx, helloID)
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
	user, err := s.cfg.Users.UserByPublicKey(s.ctx, resp.PeerStatic())
	if err != nil || user.Status != store.UserActive {
		log.Warn("hub: клиент не авторизован", "err", err, "status", user.Status)
		s.putRV(encodeDenied(nonce))
		return
	}
	// Отмечаем «был в сети» — best-effort, вне критического пути рукопожатия.
	if err := s.cfg.Users.TouchUser(s.ctx, user.ID); err != nil {
		log.Warn("hub: не удалось отметить last_seen", "user", user.ID, "err", err)
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
			id:    bundleID,
			lane:  bundleLane,
			epoch: bundleEpoch,
			token: bundleToken,
			page:  page,
		})
		if !valid {
			log.Warn("hub: failed to encode bundle assignment", "bundle", bundleID.String())
			s.pool.release(page)
			s.putRV(encodeDenied(nonce))
			return
		}
	}
	keys, msg2, err := resp.Accept(responsePayload)
	if err != nil {
		log.Warn("hub: не удалось завершить рукопожатие", "err", err)
		s.pool.release(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	sealed, err := crypto.NewSealed(s.cfg.Codec, keys.Send, keys.Recv)
	if err != nil {
		log.Warn("hub: не удалось собрать sealed-кодек", "err", err)
		s.pool.release(page)
		s.putRV(encodeDenied(nonce))
		return
	}

	sess, err := s.cfg.Dialer.Join(s.ctx)
	if err != nil {
		log.Warn("hub: не удалось поднять серверную сессию доски", "err", err, "user", user.Name)
		s.pool.release(page)
		s.putRV(encodeDenied(nonce))
		return
	}
	log.Info("hub: server session for client", "user", user.Name, "participant", sess.Participant(), "page", page)
	snapshot, err := sess.Subscribe(s.ctx, page)
	if err != nil {
		log.Warn("hub: не удалось подписаться на страницу", "err", err, "page", page)
		_ = sess.Close()
		s.pool.release(page)
		s.putRV(encodeDenied(nonce))
		return
	}

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
			s.pool.release(page)
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
				userID: user.ID,
				epoch:  bundleEpoch,
				token:  bundleToken,
				bond:   bundleConn,
				mux:    m,
				lanes:  make(map[bond.LaneID]*liveLane),
				nextID: bond.FirstLane + 1,
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
			userID: user.ID,
			mux:    m,
			lanes:  make(map[bond.LaneID]*liveLane),
			nextID: bond.FirstLane + 1,
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
		s.pool.release(page)
		s.putRV(encodeDenied(nonce))
		return
	}

	lane := &liveLane{id: bundleLane, page: page, link: l}
	s.mu.Lock()
	if s.clients == nil { // сервер уже закрывается
		s.mu.Unlock()
		if isJoin {
			bundleConn.RemoveLane(bundleLane)
		} else {
			_ = m.Close()
		}
		s.pool.release(page)
		return
	}
	if isJoin {
		if s.bundles[bundleID] != bundle || s.clients[m] != bundle {
			s.mu.Unlock()
			bundleConn.RemoveLane(bundleLane)
			s.pool.release(page)
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

	// Серверная страница готова — объявляем назначение (msg2 с ключами и
	// зашифрованным id страницы внутри).
	s.putRV(encodeAssign(nonce, version, hello.legacy, msg2))

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
	s.pool.release(lane.page)
	rvLog(s.cfg.Link.Log).Info("hub: client lane released",
		"user", bundle.userID, "page", lane.page, "lane", lane.id,
		"reason", reason, "remaining_lanes", bundleLaneCount(s, bundle),
		"free_pages", s.pool.available())
	if last {
		_ = bundle.mux.Close()
	}
}

func bundleLaneCount(s *Server, bundle *liveBundle) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(bundle.lanes)
}

// watchBundle owns logical-session bookkeeping and persists traffic exactly
// once, independently of how many lane pages were attached.
func (s *Server) watchBundle(bundleID bond.BundleID, bundle *liveBundle) {
	defer s.connWG.Done()
	select {
	case <-bundle.mux.Done():
	case <-s.ctx.Done():
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

	// Персист трафика — отдельным контекстом: s.ctx уже может быть отменён
	// (штатное закрытие сервера), а Close дожидается connWG именно затем, чтобы
	// эта запись успела дойти до store, прежде чем вызывающий его закроет.
	tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.cfg.Users.AddUserTraffic(tctx, bundle.userID, final.Received, final.Written); err != nil {
		rvLog(s.cfg.Link.Log).Warn("hub: не удалось сохранить трафик клиента",
			"user", bundle.userID, "err", err)
	}
}

// DisconnectUser корректно закрывает все живые сессии пользователя (каждая шлёт
// GOAWAY) — используется при отключении клиента через управление. Возвращает
// число закрытых сессий. Освобождение страниц и чистку трекинга доделает
// watchClient каждой сессии, проснувшись на её закрытии.
func (s *Server) DisconnectUser(_ context.Context, userID int64) int {
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
func (s *Server) putRV(body []byte) {
	value, err := s.cfg.Codec.Encode(body)
	if err != nil {
		return
	}
	_ = s.cfg.HubSession.Put(s.ctx, board.Object{ID: board.NewID(), Value: value})
}
