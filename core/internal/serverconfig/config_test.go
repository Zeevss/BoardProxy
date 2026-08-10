package serverconfig

import (
	"encoding/base64"
	"strings"
	"testing"

	"bproxy-core/internal/crypto"
)

func validConfig(t *testing.T) string {
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
	return `
version = 1
[server]
private_key = "` + enc(server.Private()) + `"
[management]
grpc_listen = "unix:///tmp/bproxy-test.sock"
[[boards]]
tag = "main"
name = "Main"
hash = "board-hash"
max_lanes = 8
[[users]]
tag = "alice"
name = "Alice"
private_key = "` + enc(user.Private()) + `"
boards = ["main"]
max_sessions = 4
max_lanes = 4
`
}

func TestDecodeAppliesDefaults(t *testing.T) {
	cfg, err := Decode(strings.NewReader(validConfig(t)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport.MaxFramePayload != 4<<20 || cfg.Server.IdleTimeout.Duration() == 0 {
		t.Fatalf("defaults were not applied: %+v", cfg)
	}
	if !cfg.Boards[0].IsEnabled() || !cfg.Users[0].IsEnabled() {
		t.Fatal("enabled must default to true")
	}
}

func TestLoadAcceptsStdinSource(t *testing.T) {
	cfg, err := Load("stdin:", strings.NewReader(validConfig(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Boards) != 1 || cfg.Boards[0].Tag != "main" {
		t.Fatalf("unexpected stdin config: %+v", cfg)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	raw := validConfig(t) + "\n[observability]\nunknown = true\n"
	if _, err := Decode(strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestDecodeRejectsUnknownBoardReference(t *testing.T) {
	raw := strings.Replace(validConfig(t), `boards = ["main"]`, `boards = ["missing"]`, 1)
	if _, err := Decode(strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "unknown board") {
		t.Fatalf("expected board reference error, got %v", err)
	}
}

func TestDecodeRejectsInvalidBoardAPIBase(t *testing.T) {
	raw := strings.Replace(validConfig(t), `hash = "board-hash"`, "hash = \"board-hash\"\napi_base = \"://invalid\"", 1)
	if _, err := Decode(strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "api_base") {
		t.Fatalf("expected api_base error, got %v", err)
	}
}

func TestPublicKeyMigrationIdentity(t *testing.T) {
	kp, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	u := User{PublicKey: "base64:" + base64.StdEncoding.EncodeToString(kp.Public())}
	id, err := u.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if len(id.Private) != 0 || string(id.Public) != string(kp.Public()) {
		t.Fatalf("unexpected identity: %+v", id)
	}
}
