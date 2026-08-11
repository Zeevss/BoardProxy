package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bproxy-control-plane/internal/domain"
)

func TestTokenIsBoundAndOneTime(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateEnrollmentToken(context.Background(), "node-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeEnrollmentToken(context.Background(), "other", token); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("wrong node error = %v", err)
	}
	if err := store.ConsumeEnrollmentToken(context.Background(), "node-1", token); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeEnrollmentToken(context.Background(), "node-1", token); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("reuse error = %v", err)
	}
}

func TestDesiredRevisionAndTrafficIdempotency(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := store.AppendDesired(context.Background(), "node-1", 0, 1, []byte("first"), "test")
	second, _ := store.AppendDesired(context.Background(), "node-1", 1, 2, []byte("second"), "test")
	if first.Revision != 1 || second.Revision != 2 {
		t.Fatalf("revisions = %d, %d", first.Revision, second.Revision)
	}
	if second.PreviousRevision != 1 || second.CatalogVersion != 2 {
		t.Fatalf("second revision metadata=%+v", second)
	}
	history, err := store.DesiredHistory(context.Background(), "node-1")
	if err != nil || len(history) != 2 || string(history[0].ConfigTOML) != "first" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if err := store.StoreTraffic(context.Background(), "node-1", domain.InterfaceTraffic, "batch-1", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreTraffic(context.Background(), "node-1", domain.InterfaceTraffic, "batch-1", []byte("duplicate")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "traffic", "interface", "node-1", "batch-1.pb"))
	if err != nil || string(raw) != "first" {
		t.Fatalf("payload=%q err=%v", raw, err)
	}
}

func TestCatalogAndNodeStatusUseOptimisticVersions(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := filesystemCatalog(t)
	if err := store.SaveCatalog(context.Background(), catalog, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCatalog(context.Background(), catalog, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale catalog save error=%v", err)
	}
	status := domain.NodeStatus{NodeID: "node-1", Connected: true, Version: 1}
	if err := store.SaveNodeStatus(context.Background(), status, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNodeStatus(context.Background(), status, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale status save error=%v", err)
	}
}

func filesystemCatalog(t *testing.T) domain.Catalog {
	t.Helper()
	key := "base64:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	userKey := "base64:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	catalog, err := domain.NewCatalog(
		domain.Node{ID: "node-1", Name: "Node", State: domain.ResourceEnabled, Core: domain.DefaultCoreSettings(key)},
		[]domain.Board{{ID: "board", Name: "Board", Hash: "hash", State: domain.ResourceEnabled, MaxLanes: 1}},
		[]domain.User{{ID: "user", Name: "User", PrivateKey: userKey, State: domain.ResourceEnabled, MaxLanes: 1}},
		domain.NodeAssignment{NodeID: "node-1", BoardIDs: []string{"board"}, Users: []domain.AssignedUser{{UserID: "user", BoardIDs: []string{"board"}}}},
		time.Unix(1, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
