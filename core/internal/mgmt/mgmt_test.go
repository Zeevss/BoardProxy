package mgmt

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/store"
)

// fakeStore — минимальная реализация mgmt.Store для тестов хендлера.
type fakeStore struct {
	mu    sync.Mutex
	next  int64
	users map[int64]store.User
	hubs  map[string]store.Hub
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]store.User{}, hubs: map[string]store.Hub{}}
}

func (f *fakeStore) CreateUser(_ context.Context, pub []byte, name string) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	u := store.User{ID: f.next, PublicKey: pub, Name: name, Status: store.UserActive, CreatedAt: time.Now()}
	f.users[u.ID] = u
	return u, nil
}
func (f *fakeStore) UserByID(_ context.Context, id int64) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}
func (f *fakeStore) ListUsers(context.Context) ([]store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}
func (f *fakeStore) SetUserStatus(_ context.Context, id int64, s store.UserStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return store.ErrNotFound
	}
	u.Status = s
	f.users[id] = u
	return nil
}
func (f *fakeStore) SetUserName(_ context.Context, id int64, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return store.ErrNotFound
	}
	u.Name = name
	f.users[id] = u
	return nil
}
func (f *fakeStore) DeleteUser(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.users[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.users, id)
	return nil
}
func (f *fakeStore) UpsertHub(_ context.Context, id, name, slide string) (store.Hub, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := store.Hub{ID: id, Name: name, HubSlide: slide, Status: store.HubActive, CreatedAt: time.Now()}
	f.hubs[id] = h
	return h, nil
}
func (f *fakeStore) ListHubs(context.Context) ([]store.Hub, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.Hub, 0, len(f.hubs))
	for _, h := range f.hubs {
		out = append(out, h)
	}
	return out, nil
}
func (f *fakeStore) SetHubStatus(_ context.Context, id string, s store.HubStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.hubs[id]
	if !ok {
		return store.ErrNotFound
	}
	h.Status = s
	f.hubs[id] = h
	return nil
}
func (f *fakeStore) SetHubName(_ context.Context, id string, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.hubs[id]
	if !ok {
		return store.ErrNotFound
	}
	h.Name = name
	f.hubs[id] = h
	return nil
}
func (f *fakeStore) DeleteHub(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.hubs[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.hubs, id)
	return nil
}

// fakeConnections — минимальная реализация ConnectionsProvider для тестов.
type fakeConnections struct {
	mu    sync.Mutex
	byUID map[int64][]hub.ConnectionInfo
}

func newFakeConnections() *fakeConnections {
	return &fakeConnections{byUID: make(map[int64][]hub.ConnectionInfo)}
}

func (f *fakeConnections) set(userID int64, conns []hub.ConnectionInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byUID[userID] = conns
}

func (f *fakeConnections) UserConnections(userID int64) []hub.ConnectionInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byUID[userID]
}

func strPtr(s string) *string { return &s }

// fakeDisconnector фиксирует вызовы DisconnectUser.
type fakeDisconnector struct {
	mu    sync.Mutex
	calls []int64
}

func (d *fakeDisconnector) DisconnectUser(_ context.Context, id int64) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, id)
	return 1
}

func (d *fakeDisconnector) called() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int64(nil), d.calls...)
}

// startServerCfg поднимает mgmt.Serve с произвольным Config на временном сокете
// и возвращает клиента, дождавшись готовности. Тестирует реальную связку
// Serve + Handler + Client.
func startServerCfg(t *testing.T, cfg Config) *Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "mgmt.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = Serve(ctx, sock, Handler(cfg)) }()

	c := NewClient(sock)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cctx, ccancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := c.ListClients(cctx)
		ccancel()
		if err == nil {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("mgmt-сервер не поднялся на сокете")
	return nil
}

func startServer(t *testing.T, fs *fakeStore, serverPub []byte) *Client {
	return startServerCfg(t, Config{Store: fs, ServerPublic: serverPub, Board: "board-hash"})
}

func TestCreateClientReturnsUsableKeylink(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	c := startServer(t, fs, serverKP.Public())

	resp, err := c.AddClient(context.Background(), "alice")
	if err != nil {
		t.Fatalf("AddClient: %v", err)
	}
	if resp.ID == 0 || resp.Name != "alice" {
		t.Fatalf("resp = %+v", resp)
	}
	creds, err := keylink.Parse(resp.Keylink)
	if err != nil {
		t.Fatalf("keylink.Parse: %v", err)
	}
	if string(creds.ServerPublic) != string(serverKP.Public()) {
		t.Fatal("keylink несёт не тот публичный ключ сервера")
	}
	if len(creds.Boards) != 1 || creds.Boards[0] != "board-hash" {
		t.Fatalf("keylink boards = %v", creds.Boards)
	}
	// Приватный ключ клиента реально из keylink восстанавливается в пару.
	if _, err := creds.ClientKeypair(); err != nil {
		t.Fatalf("ClientKeypair: %v", err)
	}
}

func TestClientLifecycle(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	c := startServer(t, fs, serverKP.Public())
	ctx := context.Background()

	resp, err := c.AddClient(ctx, "bob")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	list, err := c.ListClients(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != resp.ID {
		t.Fatalf("list = %+v", list)
	}
	got, err := c.GetClient(ctx, resp.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != string(store.UserActive) {
		t.Fatalf("status = %q", got.Status)
	}
	if err := c.RemoveClient(ctx, resp.ID); err != nil {
		t.Fatalf("rm: %v", err)
	}
	// Удаление теперь жёсткое — записи в store больше нет.
	if _, ok := fs.users[resp.ID]; ok {
		t.Fatalf("rm должен удалять пользователя, а не отключать")
	}
	if _, err := c.GetClient(ctx, resp.ID); err == nil {
		t.Fatal("GetClient(удалённый) должен вернуть ошибку")
	}
	if _, err := c.GetClient(ctx, 9999); err == nil {
		t.Fatal("GetClient(неизвестный) должен вернуть ошибку")
	}
}

func TestRemoveClientDisconnectsLiveSession(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	disc := &fakeDisconnector{}
	c := startServerCfg(t, Config{Store: fs, ServerPublic: serverKP.Public(), Board: "b", Disconnector: disc})
	ctx := context.Background()

	resp, err := c.AddClient(ctx, "on-the-line")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := c.RemoveClient(ctx, resp.ID); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if got := disc.called(); len(got) != 1 || got[0] != resp.ID {
		t.Fatalf("DisconnectUser вызван с %v, хочу [%d]", got, resp.ID)
	}
}

func TestRestartTriggersCallback(t *testing.T) {
	serverKP, _ := crypto.Generate()
	fired := make(chan struct{}, 1)
	c := startServerCfg(t, Config{
		Store:        newFakeStore(),
		ServerPublic: serverKP.Public(),
		Restart:      func() { fired <- struct{}{} },
	})
	if err := c.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("Restart-колбэк не сработал")
	}
}

func TestCreateClientBoardlessUsesFirstActiveHub(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	// Board в конфиге пуст (board-less), но в store есть активный хаб.
	if _, err := fs.UpsertHub(context.Background(), "hub-from-db", "", "slide"); err != nil {
		t.Fatal(err)
	}
	c := startServerCfg(t, Config{Store: fs, ServerPublic: serverKP.Public(), Board: ""})

	resp, err := c.AddClient(context.Background(), "x")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	creds, err := keylink.Parse(resp.Keylink)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(creds.Boards) != 1 || creds.Boards[0] != "hub-from-db" {
		t.Fatalf("keylink boards = %v, хочу [hub-from-db]", creds.Boards)
	}
}

func TestBoardLifecycle(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	c := startServer(t, fs, serverKP.Public())
	ctx := context.Background()

	if _, err := c.AddBoard(ctx, "b1", "First"); err != nil {
		t.Fatalf("add: %v", err)
	}
	boards, err := c.ListBoards(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(boards) != 1 || boards[0].ID != "b1" || boards[0].Name != "First" {
		t.Fatalf("boards = %+v", boards)
	}
	if err := c.RemoveBoard(ctx, "b1"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := c.RemoveBoard(ctx, "nope"); err == nil {
		t.Fatal("RemoveBoard(неизвестный) должен вернуть ошибку")
	}
}

func TestUpdateClientRenameAndDisable(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	disc := &fakeDisconnector{}
	c := startServerCfg(t, Config{Store: fs, ServerPublic: serverKP.Public(), Board: "b", Disconnector: disc})
	ctx := context.Background()

	resp, err := c.AddClient(ctx, "carol")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := c.UpdateClient(ctx, resp.ID, UpdateClientRequest{Name: strPtr("carol-renamed")})
	if err != nil {
		t.Fatalf("update(name): %v", err)
	}
	if got.Name != "carol-renamed" || got.Status != string(store.UserActive) {
		t.Fatalf("после переименования = %+v", got)
	}

	got, err = c.UpdateClient(ctx, resp.ID, UpdateClientRequest{Status: strPtr(string(store.UserDisabled))})
	if err != nil {
		t.Fatalf("update(status): %v", err)
	}
	if got.Status != string(store.UserDisabled) {
		t.Fatalf("статус после update = %q", got.Status)
	}
	if calls := disc.called(); len(calls) != 1 || calls[0] != resp.ID {
		t.Fatalf("PATCH-отключение должно рвать живые сессии, calls=%v", calls)
	}

	if _, err := c.UpdateClient(ctx, resp.ID, UpdateClientRequest{Status: strPtr("bogus")}); err == nil {
		t.Fatal("невалидный статус должен быть ошибкой")
	}
	if _, err := c.UpdateClient(ctx, resp.ID, UpdateClientRequest{Name: strPtr("")}); err == nil {
		t.Fatal("пустое имя должно быть ошибкой")
	}
	if _, err := c.UpdateClient(ctx, 999999, UpdateClientRequest{Name: strPtr("x")}); err == nil {
		t.Fatal("UpdateClient(неизвестный) должен вернуть ошибку")
	}
}

func TestUpdateBoardRenameAndStatusPlusGetBoard(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	c := startServer(t, fs, serverKP.Public())
	ctx := context.Background()

	if _, err := c.AddBoard(ctx, "b1", "First"); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := c.GetBoard(ctx, "b1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "First" {
		t.Fatalf("get = %+v", got)
	}
	if _, err := c.GetBoard(ctx, "nope"); err == nil {
		t.Fatal("GetBoard(неизвестный) должен вернуть ошибку")
	}

	got, err = c.UpdateBoard(ctx, "b1", UpdateBoardRequest{Name: strPtr("Renamed"), Status: strPtr(string(store.HubDisabled))})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Name != "Renamed" || got.Status != string(store.HubDisabled) {
		t.Fatalf("после update = %+v", got)
	}
	if _, err := c.UpdateBoard(ctx, "b1", UpdateBoardRequest{Status: strPtr("bogus")}); err == nil {
		t.Fatal("невалидный статус должен быть ошибкой")
	}
}

func TestClientConnectionsAndLiveTraffic(t *testing.T) {
	fs := newFakeStore()
	serverKP, _ := crypto.Generate()
	conns := newFakeConnections()
	c := startServerCfg(t, Config{Store: fs, ServerPublic: serverKP.Public(), Board: "b", Connections: conns})
	ctx := context.Background()

	resp, err := c.AddClient(ctx, "dave")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Пока клиент не на линии — пустой список, но не ошибка.
	empty, err := c.GetClientConnections(ctx, resp.ID)
	if err != nil {
		t.Fatalf("connections(offline): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("connections(offline) = %+v, хочу пусто", empty)
	}

	conns.set(resp.ID, []hub.ConnectionInfo{{
		BundleID: "00112233445566778899aabbccddeeff",
		LaneID:   1,
		Epoch:    1,
		Page:     "p1",
		Lanes: []hub.LaneInfo{
			{ID: 1, Page: "p1", RTT: 12 * time.Millisecond},
			{ID: 2, Page: "p2", RTT: 18 * time.Millisecond},
		},
		Written:  100,
		Received: 200,
		RTT:      42 * time.Millisecond,
		Streams: []hub.StreamInfo{
			{ID: 1, Target: "example.com:443", Written: 100, Received: 200, StartedAt: time.Now()},
		},
	}})

	live, err := c.GetClientConnections(ctx, resp.ID)
	if err != nil {
		t.Fatalf("connections(online): %v", err)
	}
	if len(live) != 1 || live[0].Page != "p1" || live[0].Written != 100 || live[0].Received != 200 {
		t.Fatalf("connections(online) = %+v", live)
	}
	if live[0].BundleID != "00112233445566778899aabbccddeeff" ||
		live[0].LaneID != 1 || live[0].Epoch != 1 {
		t.Fatalf("bundle identity = %+v", live[0])
	}
	if len(live[0].Lanes) != 2 || live[0].Lanes[1].Page != "p2" ||
		live[0].Lanes[1].RTTMillis != 18 {
		t.Fatalf("lanes = %+v", live[0].Lanes)
	}
	if live[0].RTTMillis != 42 {
		t.Fatalf("rtt_ms = %d, хочу 42", live[0].RTTMillis)
	}
	if len(live[0].Streams) != 1 || live[0].Streams[0].Target != "example.com:443" {
		t.Fatalf("streams = %+v", live[0].Streams)
	}

	// GetClient должен суммировать live-трафик поверх персистентного (здесь 0).
	info, err := c.GetClient(ctx, resp.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.RxBytes != 200 || info.TxBytes != 100 {
		t.Fatalf("live-трафик не попал в ClientInfo: %+v", info)
	}

	if _, err := c.GetClientConnections(ctx, 999999); err == nil {
		t.Fatal("connections(неизвестный клиент) должен вернуть ошибку")
	}
}
