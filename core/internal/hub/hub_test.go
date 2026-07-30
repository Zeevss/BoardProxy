package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/bond"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/handshake"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/proto"
	"bproxy-core/internal/store"
)

type memDialer struct{ b *memory.Board }

func (d memDialer) Join(context.Context) (board.Session, error) {
	return d.b.NewSession(board.NewID()), nil
}

type recordingDialer struct {
	b  *memory.Board
	mu sync.Mutex
	ss []*memory.Session
}

func (d *recordingDialer) Join(context.Context) (board.Session, error) {
	sess := d.b.NewSession(board.NewID())
	d.mu.Lock()
	d.ss = append(d.ss, sess)
	d.mu.Unlock()
	return sess, nil
}

func (d *recordingDialer) session(i int) *memory.Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ss[i]
}

// fakeUsers — минимальная реализация UserStore для тестов: только поиск по
// публичному ключу заранее «предоставленных» пользователей.
type fakeUsers struct {
	mu      sync.Mutex
	next    int64
	byKey   map[string]store.User
	traffic map[int64][2]uint64 // id -> [rx, tx], для проверки AddUserTraffic
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byKey: make(map[string]store.User), traffic: make(map[int64][2]uint64)}
}

func (f *fakeUsers) provision(pub []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.byKey[string(pub)] = store.User{ID: f.next, PublicKey: pub, Name: "test", Status: store.UserActive}
}

func (f *fakeUsers) idOf(pub []byte) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byKey[string(pub)].ID
}

func (f *fakeUsers) UserByPublicKey(_ context.Context, pub []byte) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byKey[string(pub)]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) AddUserTraffic(_ context.Context, id int64, rx, tx uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.traffic[id]
	t[0] += rx
	t[1] += tx
	f.traffic[id] = t
	return nil
}

func (f *fakeUsers) TouchUser(_ context.Context, _ int64) error { return nil }

func (f *fakeUsers) trafficOf(id int64) (rx, tx uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.traffic[id]
	return t[0], t[1]
}

// testHub — hub-сервер на in-memory доске с серверным ключом и фейковым
// каталогом пользователей.
type testHub struct {
	b        *memory.Board
	srv      *Server
	serverKP crypto.Keypair
	users    *fakeUsers
}

func newTestHub(t *testing.T, pool []string) *testHub { return newTestHubIdle(t, pool, 0) }

func newTestHubIdle(t *testing.T, pool []string, idle time.Duration) *testHub {
	t.Helper()
	b := memory.NewBoard()
	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	users := newFakeUsers()
	srv, err := NewServer(context.Background(), ServerConfig{
		HubSession:   b.NewSession("hub-observer"),
		HubSlide:     "hub",
		Pool:         pool,
		Dialer:       memDialer{b},
		ServerStatic: serverKP,
		Users:        users,
		Codec:        codec.Base64Codec{},
		IdleTimeout:  idle,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return &testHub{b: b, srv: srv, serverKP: serverKP, users: users}
}

// dial провизионирует свежий ключ клиента и подключается (авторизованный путь).
func (h *testHub) dial(t *testing.T) (*mux.Session, error) {
	t.Helper()
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	return h.dialWith(t, client)
}

// dialWith подключается ключом client независимо от того, провизионирован он или
// нет.
func (h *testHub) dialWith(t *testing.T, client crypto.Keypair) (*mux.Session, error) {
	t.Helper()
	return Dial(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: h.serverKP.Public(),
	})
}

// serveEcho accepts server-side mux sessions and echoes every stream.
func serveEcho(srv *Server) {
	go func() {
		for {
			m, err := srv.Accept(context.Background())
			if err != nil {
				return
			}
			go func(m *mux.Session) {
				for {
					st, err := m.AcceptStream(context.Background())
					if err != nil {
						return
					}
					go func(st *mux.Stream) {
						data, _ := io.ReadAll(st)
						_, _ = st.Write(data)
						_ = st.CloseWrite()
					}(st)
				}
			}(m)
		}
	}()
}

func TestHubMultipleClientsDistinctPages(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2", "p3"})
	serveEcho(h.srv)

	const clients = 3
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := h.dial(t)
			if err != nil {
				errs <- fmt.Errorf("client %d dial: %w", i, err)
				return
			}
			defer m.Close()
			st, err := m.OpenStream(fmt.Sprintf("target-%d:80", i))
			if err != nil {
				errs <- err
				return
			}
			msg := fmt.Sprintf("hello from client %d", i)
			_, _ = io.WriteString(st, msg)
			_ = st.CloseWrite()
			got := readAll(t, st, 5*time.Second)
			if got != msg {
				errs <- fmt.Errorf("client %d: got %q want %q", i, got, msg)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestHubPoolExhaustion(t *testing.T) {
	h := newTestHub(t, []string{"only-one"})
	serveEcho(h.srv)

	// First client takes the single page and stays connected.
	m1, err := h.dial(t)
	if err != nil {
		t.Fatalf("first client should succeed: %v", err)
	}
	defer m1.Close()

	// Second client must be denied (pool exhausted, reported generically).
	if _, err := h.dial(t); err != ErrRendezvousDenied {
		t.Fatalf("second client err = %v, want ErrRendezvousDenied", err)
	}
}

func TestHubUnauthorizedClientDenied(t *testing.T) {
	h := newTestHub(t, []string{"p1"})
	serveEcho(h.srv)

	// Клиент с валидным ключом, но не заведённый в каталоге, должен получить
	// тот же generic-отказ, что и при исчерпании пула.
	unknown, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.dialWith(t, unknown); err != ErrRendezvousDenied {
		t.Fatalf("unauthorized client err = %v, want ErrRendezvousDenied", err)
	}

	// Пул не должен быть занят отклонённым клиентом.
	if got := h.srv.pool.available(); got != 1 {
		t.Fatalf("pool available = %d, want 1 (denied client must not hold a page)", got)
	}
}

func TestHubRejectsWrongServerKey(t *testing.T) {
	h := newTestHub(t, []string{"p1"})
	serveEcho(h.srv)

	// Клиент провизионирован, но пинит НЕ тот публичный ключ сервера — IK на
	// стороне сервера не сойдётся, клиент получит отказ.
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	impostor, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Dial(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: impostor.Public(),
	})
	if err == nil {
		t.Fatal("dial с чужим ключом сервера прошёл, хочу ошибку")
	}
}

func TestServerProcessesHelloFromInitialSnapshot(t *testing.T) {
	b := memory.NewBoard()
	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	clientKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	users := newFakeUsers()
	users.provision(clientKP.Public())
	clientSess := b.NewSession("waiting-client")
	if _, err := clientSess.Subscribe(context.Background(), "hub"); err != nil {
		t.Fatal(err)
	}
	init, err := handshake.Initiate(clientKP, serverKP.Public())
	if err != nil {
		t.Fatal(err)
	}
	var nonce [nonceLen]byte
	nonce[0] = 42
	value, err := codec.Base64Codec{}.Encode(encodeLegacyHello(nonce, 2, init.Message()))
	if err != nil {
		t.Fatal(err)
	}
	if err := clientSess.Put(context.Background(), board.Object{ID: "before-server", Value: value}); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(context.Background(), ServerConfig{
		HubSession: b.NewSession("hub-observer"), HubSlide: "hub", Pool: []string{"p1"},
		Dialer: memDialer{b}, ServerStatic: serverKP, Users: users, Codec: codec.Base64Codec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := waitAssign(waitCtx, clientSess, codec.Base64Codec{}, nonce); err != nil {
		t.Fatalf("snapshot HELLO was not answered: %v", err)
	}
}

func TestHelloCarriesAndValidatesProtocolVersion(t *testing.T) {
	var nonce [nonceLen]byte
	frame := encodeHello(nonce, []byte("noise-message"))
	decoded, ok := decodeRV(frame)
	if !ok {
		t.Fatal("encoded HELLO did not decode")
	}
	hello, ok := decodeHello(decoded.body)
	if !ok || hello.legacy || hello.minVersion != proto.Version || hello.maxVersion != proto.Version ||
		string(hello.msg1) != "noise-message" {
		t.Fatalf("decodeHello = %+v, %v", hello, ok)
	}
	if version, ok := negotiateVersion(hello); !ok || version != proto.Version {
		t.Fatalf("negotiateVersion = %d, %v", version, ok)
	}

	legacyFrame := encodeLegacyHello(nonce, 2, []byte("legacy-noise"))
	legacyRV, ok := decodeRV(legacyFrame)
	if !ok {
		t.Fatal("legacy HELLO did not decode")
	}
	legacy, ok := decodeHello(legacyRV.body)
	if !ok || !legacy.legacy || legacy.minVersion != 2 || string(legacy.msg1) != "legacy-noise" {
		t.Fatalf("legacy HELLO = %+v, %v", legacy, ok)
	}
	if version, ok := negotiateVersion(legacy); !ok || version != 2 {
		t.Fatalf("legacy negotiateVersion = %d, %v", version, ok)
	}
	previousBonded := helloEnvelope{minVersion: 3, maxVersion: 3, msg1: []byte("noise")}
	if version, ok := negotiateVersion(previousBonded); !ok || version != 3 {
		t.Fatalf("v3 negotiateVersion = %d, %v", version, ok)
	}

	incompatible := helloEnvelope{
		minVersion: byte(proto.Version + 1),
		maxVersion: byte(proto.Version + 1),
		msg1:       []byte("noise"),
	}
	if _, ok := negotiateVersion(incompatible); ok {
		t.Fatal("incompatible protocol range was accepted")
	}
}

func TestClaimJoinValidatesOwnerTokenAndLaneLimit(t *testing.T) {
	id, err := bond.NewBundleID()
	if err != nil {
		t.Fatal(err)
	}
	token, err := bond.NewJoinToken()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		bundles: map[bond.BundleID]*liveBundle{
			id: {
				userID: 7,
				epoch:  bond.FirstEpoch,
				token:  token,
				lanes:  map[bond.LaneID]*liveLane{1: &liveLane{}},
				nextID: 2,
			},
		},
		joinClaims: make(map[bond.BundleID]bool),
		maxLanes:   4,
	}
	req := bundleRequest{id: id, epoch: bond.FirstEpoch, token: token}
	if _, laneID, ok := s.claimJoin(7, req); !ok || laneID != 2 {
		t.Fatalf("valid join = lane %d, ok %v", laneID, ok)
	}
	s.releaseJoinClaim(id)

	bad := req
	bad.token[0] ^= 0xff
	if _, _, ok := s.claimJoin(7, bad); ok {
		t.Fatal("join with wrong token accepted")
	}
	if _, _, ok := s.claimJoin(8, req); ok {
		t.Fatal("join from another user accepted")
	}
	for laneID := bond.LaneID(2); laneID <= bond.LaneID(s.maxLanes); laneID++ {
		s.bundles[id].lanes[laneID] = &liveLane{}
	}
	if _, _, ok := s.claimJoin(7, req); ok {
		t.Fatal("lane exceeded adaptive bundle limit")
	}
}

func TestDialBundleReturnsAuthenticatedIdentity(t *testing.T) {
	h := newTestHub(t, []string{"p1"})
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())

	result, err := DialBundle(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: h.serverKP.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle.ID.IsZero() || result.Bundle.LaneID != 1 ||
		result.Bundle.Epoch != 1 || result.Bundle.Page != "p1" ||
		result.Bundle.JoinToken.IsZero() {
		t.Fatalf("bundle info = %+v", result.Bundle)
	}

	h.srv.mu.Lock()
	bundle := h.srv.bundles[result.Bundle.ID]
	h.srv.mu.Unlock()
	if bundle == nil || bundle.userID == 0 || bundle.epoch != result.Bundle.Epoch ||
		!bundle.token.Equal(result.Bundle.JoinToken) || bundle.lanes[result.Bundle.LaneID] == nil {
		t.Fatalf("server bundle state = %+v", bundle)
	}

	_ = result.Session.Close()
	if err := eventuallyHub(func() bool {
		h.srv.mu.Lock()
		defer h.srv.mu.Unlock()
		return h.srv.bundles[result.Bundle.ID] == nil
	}); err != nil {
		t.Fatalf("bundle was not released with its last lane: %v", err)
	}
}

func TestDialBundleJoinsSecondLaneIntoOneMux(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2"})
	serveEcho(h.srv)
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	dialer := &recordingDialer{b: h.b}
	result, err := DialBundle(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		Dialer:       dialer,
		TargetLanes:  2,
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: h.serverKP.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Session.Close()
	if len(result.Lanes) != 2 || result.Lanes[0].ID != result.Lanes[1].ID ||
		result.Lanes[0].LaneID != 1 || result.Lanes[1].LaneID != 2 ||
		result.Lanes[0].Page == result.Lanes[1].Page {
		t.Fatalf("joined lanes = %+v", result.Lanes)
	}
	if got := h.srv.pool.available(); got != 0 {
		t.Fatalf("free pages = %d, want 0", got)
	}

	for i := 0; i < 8; i++ {
		st, err := result.Session.OpenStream(fmt.Sprintf("lane-test-%d:80", i))
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("payload-%d", i)
		_, _ = io.WriteString(st, want)
		_ = st.CloseWrite()
		if got := readAll(t, st, 3*time.Second); got != want {
			t.Fatalf("echo = %q, want %q", got, want)
		}
	}

	h.srv.mu.Lock()
	serverBundle := h.srv.bundles[result.Bundle.ID]
	laneCount := len(serverBundle.lanes)
	h.srv.mu.Unlock()
	if laneCount != 2 {
		t.Fatalf("server lane count = %d, want 2", laneCount)
	}

	_ = result.Session.Close()
	if err := waitPool(h.srv, 2, 3*time.Second); err != nil {
		t.Fatalf("bundle close did not release both pages: %v", err)
	}
}

func TestDialBundleDegradesWhenSecondPageUnavailable(t *testing.T) {
	h := newTestHub(t, []string{"only"})
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	result, err := DialBundle(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		Dialer:       &recordingDialer{b: h.b},
		TargetLanes:  2,
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: h.serverKP.Public(),
	})
	if err != nil {
		t.Fatalf("initial lane must survive failed JOIN_LANE: %v", err)
	}
	defer result.Session.Close()
	if len(result.Lanes) != 1 || result.Lanes[0].Page != "only" {
		t.Fatalf("degraded lanes = %+v, want one initial lane", result.Lanes)
	}
	select {
	case <-result.Session.Done():
		t.Fatalf("degraded mux unexpectedly closed: %v", result.Session.Err())
	default:
	}
}

func TestSecondLaneLossKeepsBundleAlive(t *testing.T) {
	h := newTestHub(t, []string{"p1", "p2"})
	serveEcho(h.srv)
	client, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h.users.provision(client.Public())
	dialer := &recordingDialer{b: h.b}
	result, err := DialBundle(context.Background(), ClientConfig{
		Session:      h.b.NewSession(board.NewID()),
		Dialer:       dialer,
		TargetLanes:  2,
		HubSlide:     "hub",
		Codec:        codec.Base64Codec{},
		ClientStatic: client,
		ServerPublic: h.serverKP.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Session.Close()
	if len(result.Lanes) != 2 {
		t.Fatalf("lanes = %d, want 2", len(result.Lanes))
	}
	h.srv.mu.Lock()
	serverLane := h.srv.bundles[result.Bundle.ID].lanes[2].link
	h.srv.mu.Unlock()
	// The in-memory board has no websocket-disconnect broadcast, so explicitly
	// close both observed endpoints of lane 2 to model a transport loss.
	_ = dialer.session(0).Close()
	_ = serverLane.Close()

	deadline := time.Now().Add(3 * time.Second)
	for h.srv.pool.available() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.srv.pool.available(); got != 1 {
		t.Fatalf("failed lane page not released; free pages = %d", got)
	}
	select {
	case <-result.Session.Done():
		t.Fatalf("bundle closed after one lane loss: %v", result.Session.Err())
	default:
	}
	st, err := result.Session.OpenStream("remaining-lane:80")
	if err != nil {
		t.Fatalf("OpenStream after lane loss: %v", err)
	}
	_, _ = io.WriteString(st, "still-alive")
	_ = st.CloseWrite()
	if got := readAll(t, st, 2*time.Second); got != "still-alive" {
		t.Fatalf("echo after lane loss = %q", got)
	}
}

func eventuallyHub(cond func() bool) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("condition was not met before timeout")
}

func TestDialCleansHelloWhenContextExpires(t *testing.T) {
	b := memory.NewBoard()
	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	clientKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = Dial(ctx, ClientConfig{
		Session: b.NewSession("client"), HubSlide: "hub", Codec: codec.Base64Codec{},
		ClientStatic: clientKP, ServerPublic: serverKP.Public(),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Dial error = %v, want context deadline", err)
	}
	observer := b.NewSession("observer")
	snapshot, err := observer.Subscribe(context.Background(), "hub")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 0 {
		t.Fatalf("timed-out Dial left %d HELLO objects on hub", len(snapshot))
	}
}

func TestHubPageReleasedOnDisconnect(t *testing.T) {
	h := newTestHub(t, []string{"solo"})
	serveEcho(h.srv)

	m1, err := h.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise then disconnect.
	st, _ := m1.OpenStream("t:80")
	_, _ = io.WriteString(st, "hi")
	_ = st.CloseWrite()
	_ = readAll(t, st, 5*time.Second)
	m1.Close()

	// The page should return to the pool, letting a new client connect.
	if err := waitPool(h.srv, 1, 3*time.Second); err != nil {
		t.Fatalf("page not released: %v", err)
	}
	m2, err := h.dial(t)
	if err != nil {
		t.Fatalf("client after release should succeed: %v", err)
	}
	m2.Close()
}

func TestHubIdleTimeoutReleasesPage(t *testing.T) {
	// Short idle timeout; the client connects but opens no streams and stays
	// silent (keepalive is far longer), so the server should reclaim its page.
	h := newTestHubIdle(t, []string{"solo"}, 300*time.Millisecond)
	serveEcho(h.srv)

	m, err := h.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := waitPool(h.srv, 1, 5*time.Second); err != nil {
		t.Fatalf("idle page not reclaimed: %v", err)
	}
}

// TestHubIdleTimeoutReleasesPageWithOpenStream фиксирует аварийный сценарий:
// клиент исчез, не успев закрыть открытый стрим. Запись в mux не должна вечно
// удерживать страницу — живость определяется heartbeat'ами link, а не числом
// локально учтённых стримов.
func TestHubIdleTimeoutReleasesPageWithOpenStream(t *testing.T) {
	h := newTestHubIdle(t, []string{"solo"}, 300*time.Millisecond)
	serveEcho(h.srv)

	m, err := h.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.OpenStream("silent:80"); err != nil {
		t.Fatal(err)
	}

	if err := waitPool(h.srv, 1, 5*time.Second); err != nil {
		t.Fatalf("page with stale open stream not reclaimed: %v", err)
	}
}

// TestServerCloseClosesClientSessions регрессионный тест на то, что Server.Close
// закрывает клиентские сессии через mux.Session.Close (который шлёт GOAWAY), а
// не через link.Link.Close напрямую в обход mux — раньше это оставляло клиента
// без явного сигнала о завершении. В in-memory board события распространяются
// синхронно, поэтому здесь проверяем наблюдаемое здесь и сейчас: клиентская
// сессия и её открытый стрим завершаются быстро, без ожидания отдельного
// таймаута. Разницу в задержке обнаружения на реальной доске (до keepalive,
// 30с) этот тест не покрывает — см. ручную проверку в плане.
func TestServerCloseClosesClientSessions(t *testing.T) {
	h := newTestHub(t, []string{"solo"})
	serveEcho(h.srv)

	m, err := h.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.OpenStream("t:80")
	if err != nil {
		t.Fatal(err)
	}

	h.srv.Close()

	select {
	case <-m.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client mux session did not close after server shutdown")
	}
	if !errors.Is(m.Err(), mux.ErrPeerGoAway) {
		t.Fatalf("client close reason = %v, want ErrPeerGoAway", m.Err())
	}

	buf := make([]byte, 1)
	if _, err := st.Read(buf); err == nil {
		t.Fatal("expected stream Read to fail after server shutdown")
	}
}

func waitPool(srv *Server, want int, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if srv.pool.available() == want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("pool available = %d, want %d", srv.pool.available(), want)
}

func readAll(t *testing.T, r io.Reader, d time.Duration) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		ch <- string(data)
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(d):
		t.Fatal("read timed out")
		return ""
	}
}
