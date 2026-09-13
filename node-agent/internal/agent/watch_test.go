package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"bproxy-node-agent/internal/identity"
	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/grpc"
)

func TestApplyConfigStoresRevisionAndCheckpoint(t *testing.T) {
	service, store, core := newTestService()

	if err := service.applyConfig(context.Background(), document(7, "version = 1\n")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if service.state.Revision != 7 {
		t.Fatalf("revision=%d, want 7", service.state.Revision)
	}
	if len(core.applied) != 1 {
		t.Fatalf("core applies=%d, want 1", len(core.applied))
	}
	if _, ok := store.checkpoints[agentStateKey]; !ok {
		t.Fatal("applied state was not checkpointed: a restart would re-apply blindly")
	}
}

// The hash guards the transport: a truncated or tampered document must never
// reach core.
func TestApplyConfigRejectsHashMismatch(t *testing.T) {
	service, _, core := newTestService()
	bad := &nodev1.ConfigDocument{Revision: 2, ConfigSha256: "deadbeef", ConfigToml: []byte("version = 1\n")}

	if err := service.applyConfig(context.Background(), bad); err == nil {
		t.Fatal("expected a hash mismatch to be refused")
	}
	if len(core.applied) != 0 {
		t.Fatal("core must not see a document that failed its hash")
	}
}

// Re-announcing the applied revision is the steady state: it must be a no-op,
// not a repeated restart of core.
func TestApplyConfigIsIdempotentForTheSameRevision(t *testing.T) {
	service, _, core := newTestService()
	doc := document(3, "version = 1\n")

	if err := service.applyConfig(context.Background(), doc); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := service.applyConfig(context.Background(), doc); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if len(core.applied) != 1 {
		t.Fatalf("core applies=%d, want 1", len(core.applied))
	}
}

// Тот же номер с другим содержимым — признак пересозданной базы хаба, а не
// его внутренней порчи: FetchConfig отдаёт то, что лежит в Postgres сейчас.
func TestApplyConfigTakesTheHubVersionOfTheSameRevision(t *testing.T) {
	service, _, core := newTestService()
	if err := service.applyConfig(context.Background(), document(3, "version = 1\n")); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	if err := service.applyConfig(context.Background(), document(3, "other\n")); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(core.applied) != 2 {
		t.Fatalf("core applies=%d, want 2", len(core.applied))
	}
	if got := string(core.applied[1]); got != "other\n" {
		t.Fatalf("core applied %q, want the hub version", got)
	}
}

// Базу хаба пересоздали, нумерация пошла заново, а том ноды это пережил.
// Прежде здесь был отказ без выхода, и нода застревала навсегда.
func TestApplyConfigResynchronisesAfterHubRevisionReset(t *testing.T) {
	service, store, core := newTestService()
	if err := service.applyConfig(context.Background(), document(5, "version = 1\n")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if err := service.applyConfig(context.Background(), document(2, "fresh hub\n")); err != nil {
		t.Fatalf("apply after the hub reset: %v", err)
	}
	if service.state.Revision != 2 {
		t.Fatalf("revision=%d, want 2", service.state.Revision)
	}
	if got := string(core.applied[len(core.applied)-1]); got != "fresh hub\n" {
		t.Fatalf("core applied %q, want the fresh configuration", got)
	}

	// Отметка обязана переехать на новую последовательность, иначе следующий
	// цикл снова посчитает документ хаба младшим.
	raw := store.checkpoints[agentStateKey]
	if !strings.Contains(string(raw), `"applied_revision":2`) {
		t.Fatalf("checkpoint = %s, want revision 2", raw)
	}
}

// A failed apply must not advance the recorded state, otherwise the next
// reconcile would believe the node is already current.
func TestFailedApplyKeepsPreviousState(t *testing.T) {
	service, _, core := newTestService()
	core.err = errors.New("core rejected the config")

	if err := service.applyConfig(context.Background(), document(9, "version = 1\n")); err == nil {
		t.Fatal("expected the core failure to surface")
	}
	if service.state.Revision != 0 {
		t.Fatalf("revision=%d, want 0", service.state.Revision)
	}
}

// syncConfig обязан сказать, изменилось ли наблюдаемое хабом состояние.
//
// По этому признаку цикл решает, отчитываться ли немедленно. Раньше он ничего
// не возвращал, и о применённой ревизии хаб узнавал только со следующим тиком
// отчётов — до пятнадцати секунд, в течение которых панель показывала
// расхождение desired / applied, которого на ноде уже не было.
func TestSyncConfigReportsWhetherStateMoved(t *testing.T) {
	service, _, core := newTestService()
	client := &fakeControlClient{config: document(4, "version = 1\n")}

	if !service.syncConfig(context.Background(), client) {
		t.Fatal("первое применение обязано считаться изменением")
	}
	if service.syncConfig(context.Background(), client) {
		t.Fatal("повтор той же ревизии изменением не является: отчёт слать не о чем")
	}

	// Отказ применения — тоже новость для хаба: он показывает её как ошибку.
	core.err = errors.New("core rejected the config")
	client.config = document(5, "version = 2\n")
	if !service.syncConfig(context.Background(), client) {
		t.Fatal("появившаяся ошибка применения обязана считаться изменением")
	}
	if service.syncConfig(context.Background(), client) {
		t.Fatal("та же ошибка второй раз изменением не является")
	}

	// Ушедшая ошибка — тоже: иначе хаб продолжит показывать её после починки.
	core.err = nil
	if !service.syncConfig(context.Background(), client) {
		t.Fatal("исчезнувшая ошибка применения обязана считаться изменением")
	}
}

// Недоступный хаб изменением не является: отчёт всё равно не уйдёт.
func TestSyncConfigReportsNoChangeWhenFetchFails(t *testing.T) {
	service, _, _ := newTestService()

	if service.syncConfig(context.Background(), &fakeControlClient{err: errors.New("hub is down")}) {
		t.Fatal("несостоявшаяся выборка конфигурации изменением не является")
	}
}

// fakeControlClient отвечает на FetchConfig и ничего больше не умеет: остальные
// вызовы в этих тестах не звучат.
type fakeControlClient struct {
	nodev1.NodeControlServiceClient
	config *nodev1.ConfigDocument
	err    error
}

func (c *fakeControlClient) FetchConfig(
	context.Context, *nodev1.FetchConfigRequest, ...grpc.CallOption,
) (*nodev1.ConfigDocument, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.config, nil
}

func document(revision uint64, body string) *nodev1.ConfigDocument {
	digest := sha256.Sum256([]byte(body))
	return &nodev1.ConfigDocument{
		Revision:     revision,
		ConfigSha256: hex.EncodeToString(digest[:]),
		ConfigToml:   []byte(body),
	}
}

func newTestService() (*Service, *fakeStore, *fakeCore) {
	store := &fakeStore{checkpoints: map[string][]byte{}, changes: make(chan struct{})}
	core := &fakeCore{}
	service := &Service{
		version:  "test",
		identity: &identity.Identity{NodeID: "node-1", HubURL: "hub:8443"},
		store:    store,
		core:     core,
		bootID:   "boot-1",
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return service, store, core
}

type fakeStore struct {
	checkpoints map[string][]byte
	pending     []localstore.Pending
	acked       []string
	changes     chan struct{}
}

func (s *fakeStore) PutCheckpoint(key string, value []byte) error {
	s.checkpoints[key] = value
	return nil
}

func (s *fakeStore) Pending() ([]localstore.Pending, error) { return s.pending, nil }

func (s *fakeStore) Ack(batchID string) error {
	s.acked = append(s.acked, batchID)
	remaining := s.pending[:0]
	for _, item := range s.pending {
		if item.BatchID != batchID {
			remaining = append(remaining, item)
		}
	}
	s.pending = remaining
	return nil
}

func (s *fakeStore) Changes() <-chan struct{} { return s.changes }

type fakeCore struct {
	applied [][]byte
	err     error
}

func (c *fakeCore) Apply(_ context.Context, config []byte) (uint64, error) {
	if c.err != nil {
		return 0, c.err
	}
	c.applied = append(c.applied, config)
	return uint64(len(c.applied)), nil
}

func (c *fakeCore) Status(context.Context) (bool, bool, string) { return true, true, "" }
