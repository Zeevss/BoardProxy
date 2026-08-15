package app

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/serverconfig"
)

func runtimeTestConfig(t *testing.T) serverconfig.Config {
	t.Helper()
	server, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	user, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	enc := func(raw []byte) string { return "base64:" + base64.StdEncoding.EncodeToString(raw) }
	disabled := false
	cfg := serverconfig.Defaults()
	cfg.Server.PrivateKey = enc(server.Private())
	cfg.Management.GRPCListen = "unix://" + t.TempDir() + "/control.sock"
	cfg.Boards = []serverconfig.Board{{Tag: "main", Name: "Main", Hash: "hash", Enabled: &disabled, MaxLanes: 8}}
	cfg.Users = []serverconfig.User{{Tag: "alice", Name: "Alice", PrivateKey: enc(user.Private()), Boards: []string{"main"}, MaxSessions: 2, MaxLanes: 4}}
	return cfg
}

func TestRuntimeConfigMutationAndRevision(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Revision() != 1 {
		t.Fatalf("revision = %d", r.Revision())
	}
	u := cfg.Users[0]
	u.Name = "Renamed"
	if err := r.ReplaceUser(1, u); err != nil {
		t.Fatal(err)
	}
	if r.Revision() != 2 || r.Users()[0].Name != "Renamed" {
		t.Fatalf("mutation was not applied: %+v", r.Users())
	}
	if err := r.ReplaceUser(1, u); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}

func TestRuntimeAddsResourcesWithoutReplacingExistingTags(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddUser(1, cfg.Users[0]); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate user error = %v, want ErrAlreadyExists", err)
	}
	if err := r.AddBoard(1, cfg.Boards[0]); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate board error = %v, want ErrAlreadyExists", err)
	}

	board := cfg.Boards[0]
	board.Tag, board.Name, board.Hash = "backup", "Backup", "backup-hash"
	if err := r.AddBoard(1, board); err != nil {
		t.Fatal(err)
	}
	user := cfg.Users[0]
	user.Tag, user.Name, user.Boards = "bob", "Bob", []string{"backup"}
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	user.PrivateKey = "base64:" + base64.StdEncoding.EncodeToString(key.Private())
	if err := r.AddUser(2, user); err != nil {
		t.Fatal(err)
	}
	if r.Revision() != 3 || len(r.Config().Boards) != 2 || len(r.Config().Users) != 2 {
		t.Fatalf("reactive additions missing: revision=%d config=%+v", r.Revision(), r.Config())
	}
}

func TestRuntimeSnapshotMutationIsAtomic(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	invalidUsers := []serverconfig.User{cfg.Users[0], cfg.Users[0]}
	if err := r.ApplySnapshot(1, invalidUsers, cfg.Boards); err == nil {
		t.Fatal("duplicate-user snapshot was accepted")
	}
	if r.Revision() != 1 || len(r.Config().Users) != 1 {
		t.Fatalf("rejected snapshot partially mutated runtime: revision=%d config=%+v", r.Revision(), r.Config())
	}

	disabled := false
	users := append([]serverconfig.User(nil), cfg.Users...)
	users[0].Enabled = &disabled
	if err := r.ApplySnapshot(1, users, cfg.Boards); err != nil {
		t.Fatal(err)
	}
	if r.Revision() != 2 || r.Users()[0].Enabled {
		t.Fatalf("valid snapshot was not applied atomically: revision=%d users=%+v", r.Revision(), r.Users())
	}
}

func TestRuntimeApplyChangesCommitsOneRevisionAndPublishesResourceEvents(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	bootID, _ := r.EventPosition()

	board := cfg.Boards[0]
	board.Tag, board.Name, board.Hash = "backup", "Backup", "backup-hash"
	user := cfg.Users[0]
	user.Tag, user.Name, user.Boards = "bob", "Bob", []string{"backup"}
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	user.PrivateKey = "base64:" + base64.StdEncoding.EncodeToString(key.Private())
	if err := r.ApplyChanges(1, []serverconfig.Change{
		{ID: "board-add", Kind: serverconfig.UpsertBoard, Board: &board},
		{ID: "user-add", Kind: serverconfig.UpsertUser, User: &user},
	}); err != nil {
		t.Fatal(err)
	}
	if r.Revision() != 2 || len(r.Config().Boards) != 2 || len(r.Config().Users) != 2 {
		t.Fatalf("changes were not committed atomically: revision=%d config=%+v", r.Revision(), r.Config())
	}
	subscription := r.SubscribeEvents(bootID, 0)
	defer subscription.Close()
	if len(subscription.Replay) != 2 {
		t.Fatalf("events=%+v, want board and user events", subscription.Replay)
	}
	for _, event := range subscription.Replay {
		if event.RuntimeRevision != 2 || event.ResourceOperation != "added" {
			t.Fatalf("event=%+v", event)
		}
	}
}

func TestRuntimeApplyChangesRejectsWholeInvalidBatch(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.ApplyChanges(1, []serverconfig.Change{
		{ID: "remove-referenced-board", Kind: serverconfig.RemoveBoard, Tag: "main"},
		{ID: "disable-user", Kind: serverconfig.SetUserEnabled, Tag: "alice", Enabled: false},
	}); err == nil {
		t.Fatal("invalid batch was accepted")
	}
	if r.Revision() != 1 || len(r.Config().Boards) != 1 || !r.Config().Users[0].IsEnabled() {
		t.Fatalf("invalid batch partially changed runtime: revision=%d config=%+v", r.Revision(), r.Config())
	}
}

func TestRuntimeCannotReloadStdin(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Reload(0); err == nil {
		t.Fatal("stdin runtime unexpectedly reloaded")
	}
}

func TestRuntimeRejectsBoardRemovalWhileReferenced(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.RemoveBoard(1, "main"); err == nil {
		t.Fatal("referenced board was removed")
	}
}

func TestRuntimeStatsAreSinceStartOnly(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := r.Stats()
	if s.Revision != 1 || s.UsersConfigured != 1 || s.BoardsConfigured != 1 || s.BoardsRunning != 0 {
		t.Fatalf("unexpected stats: %+v", s)
	}
}

func TestRuntimeDoesNotBlockOnBoardStartup(t *testing.T) {
	cfg := runtimeTestConfig(t)
	enabled := true
	cfg.Boards[0].Enabled = &enabled
	cfg.Boards[0].APIBase = "http://127.0.0.1:1/api"
	started := time.Now()
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("runtime creation blocked on board dial for %v", elapsed)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		boards := r.Boards()
		if len(boards) == 1 && boards[0].State == string(BoardRetrying) && boards[0].Error != "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("board did not reach retrying state: %+v", r.Boards())
}

func TestBoardStartupRetriesAndShutdownInterruptsBackoff(t *testing.T) {
	cfg := runtimeTestConfig(t)
	r, err := NewServerRuntime(context.Background(), cfg, "stdin:", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	r.startBoard = func(context.Context, serverconfig.Config, serverconfig.Board) (*hub.Server, string, int, error) {
		attempts.Add(1)
		return nil, "", 0, errors.New("temporary outage")
	}
	if err := r.SetBoardEnabled(1, "main", true); err != nil {
		r.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for attempts.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := attempts.Load(); got < 2 {
		r.Close()
		t.Fatalf("board startup attempts = %d, want at least 2", got)
	}
	started := time.Now()
	r.Close()
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("shutdown did not interrupt board retry backoff: %v", elapsed)
	}
}
