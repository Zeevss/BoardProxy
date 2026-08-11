package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"bproxy-control-plane/internal/adapters/coreconfig"
	"bproxy-control-plane/internal/adapters/events"
	"bproxy-control-plane/internal/adapters/filesystem"
	"bproxy-control-plane/internal/domain"
)

func TestCatalogChangeCreatesRevisionAndApplyClearsDrift(t *testing.T) {
	ctx := context.Background()
	store, desired, catalogs := catalogFixture(t)
	catalog := applicationCatalog(t)
	created, err := catalogs.Create(ctx, catalog, "test-operator")
	if err != nil {
		t.Fatal(err)
	}
	if !created.ConfigChanged || created.Desired.Revision != 1 || created.Desired.CatalogVersion != 1 {
		t.Fatalf("create result=%+v", created)
	}

	session := NewNodeSession(store, store, store, "node-1", domain.AppliedState{})
	now := time.Unix(200, 0).UTC()
	if err := session.Connected(ctx, domain.NodeHello{BootID: "boot-1"}, now); err != nil {
		t.Fatal(err)
	}
	revision, send, err := session.PendingDesired(ctx, now)
	if err != nil || !send || revision.Revision != 1 {
		t.Fatalf("desired=%+v send=%t err=%v", revision, send, err)
	}
	if err := session.RecordApply(ctx, domain.ApplyResult{
		DesiredRevision: revision.Revision, RuntimeRevision: 1,
		ConfigSHA256: revision.ConfigSHA256, AppliedAt: now,
	}, now); err != nil {
		t.Fatal(err)
	}
	status, err := store.NodeStatus(ctx, "node-1")
	if err != nil || status.Drifted() {
		t.Fatalf("status=%+v err=%v", status, err)
	}

	board := catalog.Boards[0]
	board.Name = "Renamed board"
	updated, err := catalogs.ReplaceBoard(ctx, "node-1", board, 1, "test-operator")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ConfigChanged || updated.Desired.Revision != 2 || updated.Desired.PreviousRevision != 1 {
		t.Fatalf("update result=%+v", updated)
	}
	status, err = store.NodeStatus(ctx, "node-1")
	if err != nil || !status.Drifted() || status.DesiredRevision != 2 || status.AppliedRevision != 1 {
		t.Fatalf("status after new desired=%+v err=%v", status, err)
	}
	history, err := desired.History(ctx, "node-1")
	if err != nil || len(history) != 2 || string(history[0].ConfigTOML) == string(history[1].ConfigTOML) {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestConcurrentCatalogMutationRejectsOneStaleWriter(t *testing.T) {
	ctx := context.Background()
	_, _, catalogs := catalogFixture(t)
	catalog := applicationCatalog(t)
	if _, err := catalogs.Create(ctx, catalog, "creator"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for index := 0; index < 2; index++ {
		writers.Add(1)
		go func(index int) {
			defer writers.Done()
			<-start
			board := catalog.Boards[0]
			board.Name = fmt.Sprintf("writer-%d", index)
			_, err := catalogs.ReplaceBoard(ctx, "node-1", board, 1, fmt.Sprintf("writer-%d", index))
			results <- err
		}(index)
	}
	close(start)
	writers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestReconnectReconcilesDesiredAgainstAppliedRevision(t *testing.T) {
	ctx := context.Background()
	store, _, catalogs := catalogFixture(t)
	created, err := catalogs.Create(ctx, applicationCatalog(t), "creator")
	if err != nil {
		t.Fatal(err)
	}
	upToDate := NewNodeSession(store, store, store, "node-1", domain.AppliedState{
		Revision: created.Desired.Revision, SHA256: created.Desired.ConfigSHA256,
	})
	if _, send, err := upToDate.PendingDesired(ctx, time.Now()); err != nil || send {
		t.Fatalf("up-to-date reconnect send=%t err=%v", send, err)
	}
	stale := NewNodeSession(store, store, store, "node-1", domain.AppliedState{})
	if revision, send, err := stale.PendingDesired(ctx, time.Now()); err != nil || !send || revision.Revision != 1 {
		t.Fatalf("stale reconnect desired=%+v send=%t err=%v", revision, send, err)
	}
}

func TestDesiredPublishNotifiesConnectedNode(t *testing.T) {
	store, err := filesystem.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	desired := NewDesiredStates(store, bus)
	notification, unsubscribe := bus.Subscribe("node-1")
	defer unsubscribe()
	if _, err := desired.Publish(context.Background(), "node-1", 0, 1, []byte("version = 1"), "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notification:
	case <-time.After(time.Second):
		t.Fatal("desired notification was not delivered")
	}
}

func TestPeriodicReconcileRepairsCatalogWithoutDesiredRevision(t *testing.T) {
	ctx := context.Background()
	store, _, catalogs := catalogFixture(t)
	catalog := applicationCatalog(t)
	if err := store.SaveCatalog(ctx, catalog, 0); err != nil {
		t.Fatal(err)
	}
	if err := catalogs.ReconcileAll(ctx, "periodic.test"); err != nil {
		t.Fatal(err)
	}
	desired, err := store.Desired(ctx, "node-1")
	if err != nil || desired.Revision != 1 || desired.Cause != "periodic.test" {
		t.Fatalf("desired=%+v err=%v", desired, err)
	}
}

func catalogFixture(t *testing.T) (*filesystem.Store, *DesiredStates, *Catalogs) {
	t.Helper()
	store, err := filesystem.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	desired := NewDesiredStates(store, bus)
	return store, desired, NewCatalogs(store, coreconfig.Compiler{}, desired, store, store)
}

func applicationCatalog(t *testing.T) domain.Catalog {
	t.Helper()
	catalog, err := domain.NewCatalog(
		domain.Node{ID: "node-1", Name: "Node 1", State: domain.ResourceEnabled, Core: domain.DefaultCoreSettings(applicationKey(1))},
		[]domain.Board{{ID: "primary", Name: "Primary", Hash: "board-hash", State: domain.ResourceEnabled, MaxLanes: 2}},
		[]domain.User{{ID: "alice", Name: "Alice", PrivateKey: applicationKey(2), State: domain.ResourceEnabled, MaxSessions: 2, MaxLanes: 2}},
		domain.NodeAssignment{NodeID: "node-1", BoardIDs: []string{"primary"}, Users: []domain.AssignedUser{{UserID: "alice", BoardIDs: []string{"primary"}}}},
		time.Unix(100, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func applicationKey(value byte) string {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = value
	}
	return "base64:" + base64.StdEncoding.EncodeToString(raw)
}
