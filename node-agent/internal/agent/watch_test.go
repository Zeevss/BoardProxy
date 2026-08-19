package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"testing"

	"bproxy-node-agent/internal/identity"
	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"
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

func TestApplyConfigRefusesSameRevisionWithDifferentHash(t *testing.T) {
	service, _, _ := newTestService()
	if err := service.applyConfig(context.Background(), document(3, "version = 1\n")); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	conflicting := &nodev1.ConfigDocument{Revision: 3, ConfigSha256: "0", ConfigToml: []byte("other")}
	if err := service.applyConfig(context.Background(), conflicting); err == nil {
		t.Fatal("the same revision must not carry two different configurations")
	}
}

func TestApplyConfigRefusesStaleRevision(t *testing.T) {
	service, _, _ := newTestService()
	if err := service.applyConfig(context.Background(), document(5, "version = 1\n")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if err := service.applyConfig(context.Background(), document(4, "older\n")); err == nil {
		t.Fatal("a revision going backwards must be refused")
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
