package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"bproxy-control-plane/internal/adapters/filesystem"
	"bproxy-control-plane/internal/domain"
)

func TestCatalogCLISeedsAndMutatesManagedDesiredState(t *testing.T) {
	directory := t.TempDir()
	seed := catalogSeed{
		Node:   domain.Node{ID: "node-1", Name: "Node", State: domain.ResourceEnabled, Core: domain.DefaultCoreSettings(cliKey(1))},
		Boards: []domain.Board{{ID: "primary", Name: "Primary", Hash: "hash", State: domain.ResourceEnabled, MaxLanes: 2}},
		Users:  []domain.User{{ID: "alice", Name: "Alice", PrivateKey: cliKey(2), State: domain.ResourceEnabled, MaxLanes: 2}},
		Assignment: domain.NodeAssignment{
			NodeID: "node-1", BoardIDs: []string{"primary"},
			Users: []domain.AssignedUser{{UserID: "alice", BoardIDs: []string{"primary"}}},
		},
	}
	seedJSON, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := App{Stdin: bytes.NewReader(seedJSON), Stdout: &output, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := app.Run([]string{"catalog", "seed", "--data", directory, "--file", "-", "--actor", "tester"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "desired_revision 1") {
		t.Fatalf("seed output=%q", output.String())
	}

	board := seed.Boards[0]
	board.Name = "Renamed"
	boardJSON, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	app.Stdin = bytes.NewReader(boardJSON)
	if err := app.Run([]string{
		"catalog", "board", "--data", directory, "--node", "node-1", "--file", "-",
		"--expected-version", "1", "--actor", "tester",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "desired_revision 2") {
		t.Fatalf("mutation output=%q", output.String())
	}

	store, err := filesystem.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	history, err := store.DesiredHistory(context.Background(), "node-1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	catalog, err := store.Catalog(context.Background(), "node-1")
	if err != nil || catalog.Version != 2 || catalog.Boards[0].Version != 2 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
}

func TestCatalogJSONRejectsUnknownFields(t *testing.T) {
	app := App{Stdin: strings.NewReader(`{"node":{},"unexpected":true}`), Stdout: io.Discard, Logger: slog.Default()}
	err := app.Run([]string{"catalog", "seed", "--data", t.TempDir(), "--file", "-"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error=%v", err)
	}
}

func TestDurationJSONUsesHumanReadableStrings(t *testing.T) {
	raw, err := json.Marshal(domain.DefaultCoreSettings(cliKey(1)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"idle_timeout":"1m30s"`)) || !bytes.Contains(raw, []byte(`"ack_timeout":"6s"`)) {
		t.Fatalf("settings JSON=%s", raw)
	}
	var decoded domain.CoreSettings
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Server.IdleTimeout != domain.Duration(90*time.Second) {
		t.Fatalf("idle timeout=%s", decoded.Server.IdleTimeout)
	}
}

func cliKey(value byte) string {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = value
	}
	return "base64:" + base64.StdEncoding.EncodeToString(raw)
}
