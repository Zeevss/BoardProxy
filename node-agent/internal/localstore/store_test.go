package localstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/protobuf/proto"
)

func TestCommitCollectionAndAck(t *testing.T) {
	store := openTestStore(t)
	event := interfaceEvent("batch-1")
	if err := store.CommitCollection(
		map[string][]byte{"interface": []byte("checkpoint")},
		map[string]*nodev1.ReportRequest{"batch-1": event},
	); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.Checkpoint("interface")
	if err != nil || string(checkpoint) != "checkpoint" {
		t.Fatalf("checkpoint=%q err=%v", checkpoint, err)
	}
	pending, err := store.Pending()
	if err != nil || len(pending) != 1 || pending[0].BatchID != "batch-1" || !proto.Equal(pending[0].Event, event) {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if err := store.Ack("batch-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack("batch-1"); err != nil {
		t.Fatalf("ack must be idempotent: %v", err)
	}
	pending, err = store.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after ack=%+v err=%v", pending, err)
	}
}

func TestStoreSurvivesReopen(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutCheckpoint("agent", []byte("revision-7")); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitCollection(nil, map[string]*nodev1.ReportRequest{"batch-1": interfaceEvent("batch-1")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	checkpoint, err := reopened.Checkpoint("agent")
	if err != nil || string(checkpoint) != "revision-7" {
		t.Fatalf("checkpoint=%q err=%v", checkpoint, err)
	}
	pending, err := reopened.Pending()
	if err != nil || len(pending) != 1 || pending[0].BatchID != "batch-1" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestPendingPreservesInsertionOrderWhenTimestampsCollide(t *testing.T) {
	store := openTestStore(t)
	// Reverse lexical order catches the old created_at,batch_id ordering. Force
	// the same timestamp to make the test deterministic.
	for _, batchID := range []string{"batch-z", "batch-a"} {
		raw, err := proto.Marshal(interfaceEvent(batchID))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO outbox (batch_id, event, created_at) VALUES (?, ?, 1)`, batchID, raw); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := store.Pending()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		ids = append(ids, item.BatchID)
	}
	if len(pending) != 2 || pending[0].BatchID != "batch-z" || pending[1].BatchID != "batch-a" {
		t.Fatalf("pending order=%v", ids)
	}
}

func TestCommitCollectionRollsBackOnBatchConflict(t *testing.T) {
	store := openTestStore(t)
	if err := store.CommitCollection(
		map[string][]byte{"interface": []byte("before")},
		map[string]*nodev1.ReportRequest{"batch-1": interfaceEvent("batch-1")},
	); err != nil {
		t.Fatal(err)
	}
	conflicting := &nodev1.ReportRequest{
		BatchId:     "batch-1",
		UserTraffic: []*nodev1.UserTrafficDelta{{UserId: "user-1"}},
	}
	err := store.CommitCollection(
		map[string][]byte{"interface": []byte("must-rollback")},
		map[string]*nodev1.ReportRequest{"batch-1": conflicting},
	)
	if !errors.Is(err, ErrBatchConflict) {
		t.Fatalf("error=%v, want ErrBatchConflict", err)
	}
	checkpoint, err := store.Checkpoint("interface")
	if err != nil || string(checkpoint) != "before" {
		t.Fatalf("checkpoint=%q err=%v, transaction was not rolled back", checkpoint, err)
	}
}

func TestCommitCollectionRejectsMismatchedBatchIDBeforeWrite(t *testing.T) {
	store := openTestStore(t)
	err := store.CommitCollection(
		map[string][]byte{"interface": []byte("must-not-commit")},
		map[string]*nodev1.ReportRequest{"map-key": interfaceEvent("event-key")},
	)
	if err == nil {
		t.Fatal("expected batch id validation error")
	}
	checkpoint, readErr := store.Checkpoint("interface")
	if readErr != nil || checkpoint != nil {
		t.Fatalf("checkpoint=%q err=%v, validation must happen before transaction", checkpoint, readErr)
	}
}

func TestOutboxLimitRollsBackCollectorCheckpoint(t *testing.T) {
	store, err := OpenWithOutboxLimit(t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.CommitCollection(
		map[string][]byte{"interface": []byte("must-not-advance")},
		map[string]*nodev1.ReportRequest{"batch-1": interfaceEvent("batch-1")},
	)
	if !errors.Is(err, ErrOutboxFull) {
		t.Fatalf("error=%v, want ErrOutboxFull", err)
	}
	checkpoint, readErr := store.Checkpoint("interface")
	if readErr != nil || checkpoint != nil {
		t.Fatalf("checkpoint=%q err=%v", checkpoint, readErr)
	}
}

func TestOpenCreatesPrivateSQLiteDatabase(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(filepath.Join(directory, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("database permissions=%#o, want 0600", mode)
	}
	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode=%q, want wal", journalMode)
	}
	var synchronous int
	if err := store.db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 2 { // SQLITE_SYNC_FULL
		t.Fatalf("synchronous=%d, want FULL(2)", synchronous)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (999, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(directory); err == nil {
		t.Fatal("expected newer schema error")
	}
}

func TestOpenRejectsLegacyDatabaseWithoutDeletingIt(t *testing.T) {
	directory := t.TempDir()
	legacyPath := filepath.Join(directory, legacyDatabaseName)
	legacy := []byte("legacy telemetry must be preserved")
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(directory); !errors.Is(err, ErrLegacyDatabase) {
		t.Fatalf("error=%v, want ErrLegacyDatabase", err)
	}
	got, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(legacy) {
		t.Fatalf("legacy database changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(directory, databaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sqlite database must not be created, stat error=%v", err)
	}
}

func TestConcurrentWritersAreSerialized(t *testing.T) {
	store := openTestStore(t)
	const collections = 32
	failures := make(chan error, collections*2)
	start := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		<-start
		for index := 0; index < collections; index++ {
			batchID := fmt.Sprintf("batch-%02d", index)
			if err := store.CommitCollection(
				map[string][]byte{"interface": []byte(fmt.Sprintf("sample-%02d", index))},
				map[string]*nodev1.ReportRequest{batchID: interfaceEvent(batchID)},
			); err != nil {
				failures <- err
			}
		}
	}()
	go func() {
		defer writers.Done()
		<-start
		for index := 0; index < collections; index++ {
			if err := store.PutCheckpoint("agent", []byte(fmt.Sprintf("revision-%02d", index))); err != nil {
				failures <- err
			}
		}
	}()
	close(start)
	writers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	pending, err := store.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != collections {
		t.Fatalf("pending=%d, want %d", len(pending), collections)
	}
	checkpoint, err := store.Checkpoint("agent")
	if err != nil || len(checkpoint) == 0 {
		t.Fatalf("agent checkpoint=%q err=%v", checkpoint, err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func interfaceEvent(batchID string) *nodev1.ReportRequest {
	return &nodev1.ReportRequest{
		BatchId:          batchID,
		InterfaceTraffic: []*nodev1.InterfaceTrafficDelta{{Interface: "eth0"}},
	}
}
