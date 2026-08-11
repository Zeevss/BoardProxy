package coreconfig

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"bproxy-control-plane/internal/domain"
)

func TestCompilerIsDeterministicAndSortsResources(t *testing.T) {
	catalog := compileCatalog(t)
	first, err := (Compiler{}).Compile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Boards[0], catalog.Boards[1] = catalog.Boards[1], catalog.Boards[0]
	catalog.Users[0], catalog.Users[1] = catalog.Users[1], catalog.Users[0]
	catalog.Assignment.BoardIDs[0], catalog.Assignment.BoardIDs[1] = catalog.Assignment.BoardIDs[1], catalog.Assignment.BoardIDs[0]
	catalog.Assignment.Users[0], catalog.Assignment.Users[1] = catalog.Assignment.Users[1], catalog.Assignment.Users[0]
	second, err := (Compiler{}).Compile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("compiler output depends on input ordering\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if string(first) != expectedTOML {
		t.Fatalf("compiled TOML changed\n--- got ---\n%s\n--- want ---\n%s", first, expectedTOML)
	}
}

func TestDisabledNodeDrainsCompiledResources(t *testing.T) {
	catalog := compileCatalog(t)
	catalog.Node.State = domain.ResourceDisabled
	compiled, err := (Compiler{}).Compile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(compiled), "enabled = false"); count != 4 {
		t.Fatalf("disabled resource count=%d, want 4\n%s", count, compiled)
	}
}

func TestRevokedUserSecretIsRemovedFromCompiledConfig(t *testing.T) {
	catalog := compileCatalog(t)
	catalog.Users[0].State = domain.ResourceRevoked
	compiled, err := (Compiler{}).Compile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(compiled), catalog.Users[0].PrivateKey) || strings.Contains(string(compiled), `tag = "zoe"`) {
		t.Fatalf("revoked user leaked into config:\n%s", compiled)
	}
}

func compileCatalog(t *testing.T) domain.Catalog {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	catalog, err := domain.NewCatalog(
		domain.Node{ID: "node-1", Name: "Node 1", State: domain.ResourceEnabled, Core: domain.DefaultCoreSettings(key(1))},
		[]domain.Board{
			{ID: "zeta", Name: "Zeta", Hash: "hash-z", State: domain.ResourceEnabled, MaxLanes: 3},
			{ID: "alpha", Name: "Alpha", Hash: "hash-a", APIBase: "https://example.test/api", State: domain.ResourceEnabled, MaxLanes: 2},
		},
		[]domain.User{
			{ID: "zoe", Name: "Zoe", PrivateKey: key(4), State: domain.ResourceEnabled, MaxSessions: 0, MaxLanes: 2},
			{ID: "alice", Name: "Alice", PrivateKey: key(3), State: domain.ResourceEnabled, MaxSessions: 2, MaxLanes: 2},
		},
		domain.NodeAssignment{
			NodeID: "node-1", BoardIDs: []string{"zeta", "alpha"},
			Users: []domain.AssignedUser{
				{UserID: "zoe", BoardIDs: []string{"zeta"}},
				{UserID: "alice", BoardIDs: []string{"zeta", "alpha"}},
			},
		},
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func key(value byte) string {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = value
	}
	return "base64:" + base64.StdEncoding.EncodeToString(raw)
}

const expectedTOML = `version = 1

[server]
  private_key = "base64:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
  idle_timeout = "1m30s"
  allow_private_egress = false

[transport]
  window = 0
  max_frame_payload = 4194304
  stream_window = 1048576
  max_stream_window = 33554432
  ack_timeout = "6s"
  coalesce_target = 0
  stream_idle_timeout = "0s"

[management]
  grpc_listen = "unix:///run/bproxy/control.sock"

[observability]
  enabled = true
  log_level = "info"

[[boards]]
  tag = "alpha"
  name = "Alpha"
  hash = "hash-a"
  api_base = "https://example.test/api"
  enabled = true
  max_lanes = 2

[[boards]]
  tag = "zeta"
  name = "Zeta"
  hash = "hash-z"
  enabled = true
  max_lanes = 3

[[users]]
  tag = "alice"
  name = "Alice"
  private_key = "base64:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="
  enabled = true
  boards = ["alpha", "zeta"]
  max_sessions = 2
  max_lanes = 2

[[users]]
  tag = "zoe"
  name = "Zoe"
  private_key = "base64:BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ="
  enabled = true
  boards = ["zeta"]
  max_sessions = 0
  max_lanes = 2
`
