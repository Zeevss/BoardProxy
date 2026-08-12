package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"
)

type fakeCore struct {
	revision uint64
	applied  []byte
	err      error
}

func (f *fakeCore) Apply(_ context.Context, config []byte) (uint64, error) {
	f.applied = append([]byte(nil), config...)
	return f.revision, f.err
}

func (*fakeCore) Status(context.Context) (bool, bool, string) { return true, true, "" }

type fakeStore struct {
	checkpoint []byte
	err        error
}

func (f *fakeStore) PutCheckpoint(_ string, value []byte) error {
	f.checkpoint = append([]byte(nil), value...)
	return f.err
}
func (*fakeStore) Pending() ([]localstore.Pending, error) { return nil, nil }
func (*fakeStore) Ack(string) error                       { return nil }
func (*fakeStore) Changes() <-chan struct{}               { return nil }

func TestActivateDesiredPersistsStateAfterCoreApply(t *testing.T) {
	config := []byte("version = 1")
	digest := sha256.Sum256(config)
	desired := &nodev1.DesiredState{Revision: 2, ConfigToml: config, ConfigSha256: hex.EncodeToString(digest[:])}
	core := &fakeCore{revision: 7}
	store := &fakeStore{}
	service := &Service{core: core, store: store, state: appliedState{Revision: 1, SHA256: "old"}}
	revision, failure := service.activateDesired(context.Background(), desired)
	if failure != "" || revision != 7 {
		t.Fatalf("revision=%d failure=%q", revision, failure)
	}
	if string(core.applied) != string(config) || len(store.checkpoint) == 0 || service.state.Revision != 2 {
		t.Fatalf("desired state was not committed: core=%q checkpoint=%q state=%+v", core.applied, store.checkpoint, service.state)
	}
}

func TestActivateDesiredDoesNotAdvanceStateWhenCheckpointFails(t *testing.T) {
	config := []byte("version = 1")
	digest := sha256.Sum256(config)
	service := &Service{
		core: &fakeCore{revision: 7}, store: &fakeStore{err: errors.New("disk full")},
		state: appliedState{Revision: 1, SHA256: "old"},
	}
	_, failure := service.activateDesired(context.Background(), &nodev1.DesiredState{
		Revision: 2, ConfigToml: config, ConfigSha256: hex.EncodeToString(digest[:]),
	})
	if failure == "" || service.state.Revision != 1 {
		t.Fatalf("failure=%q state=%+v", failure, service.state)
	}
}
