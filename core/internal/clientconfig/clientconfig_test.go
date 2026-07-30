package clientconfig

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"bproxy-core/internal/crypto"
	"bproxy-core/internal/keylink"
)

func writeTOML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadKeylinkForm(t *testing.T) {
	path := writeTOML(t, `
listen = "127.0.0.1:9999"
log = "debug"
local_dns = true
system_proxy = true
enable_udp = true
max_lanes = 6
bypass = ["\\.local$", "^10\\."]
keylink = "bproxy://abc#label"
board = "boardhash"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Keylink != "bproxy://abc#label" {
		t.Errorf("keylink = %q", cfg.Keylink)
	}
	if cfg.Listen != "127.0.0.1:9999" || cfg.LogLevel != "debug" {
		t.Errorf("listen/log не смапились: %+v", cfg)
	}
	if !cfg.LocalDNS || !cfg.SystemProxy || !cfg.EnableUDP {
		t.Errorf("local_dns/system_proxy/enable_udp не смапились: %+v", cfg)
	}
	if cfg.MaxLanes != 6 {
		t.Errorf("max_lanes = %d", cfg.MaxLanes)
	}
	if len(cfg.BypassList) != 2 || cfg.Board != "boardhash" {
		t.Errorf("bypass/board не смапились: %+v", cfg)
	}
}

func TestLoadKeysFormBuildsKeylink(t *testing.T) {
	clientKP, _ := crypto.Generate()
	serverKP, _ := crypto.Generate()
	priv := base64.StdEncoding.EncodeToString(clientKP.Private())
	pub := base64.StdEncoding.EncodeToString(serverKP.Public())

	path := writeTOML(t, `
listen = "127.0.0.1:1080"
bypass = ["x"]

[keys]
client_private = "`+priv+`"
server_public = "`+pub+`"
boards = ["hash1", "hash2"]
label = "me"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// keylink должен разбираться обратно в те же ключи/доски.
	creds, err := keylink.Parse(cfg.Keylink)
	if err != nil {
		t.Fatalf("собранный keylink не парсится: %v", err)
	}
	if string(creds.ClientPrivate) != string(clientKP.Private()) {
		t.Error("client_private не совпал после round-trip")
	}
	if string(creds.ServerPublic) != string(serverKP.Public()) {
		t.Error("server_public не совпал после round-trip")
	}
	if len(creds.Boards) != 2 || creds.Boards[0] != "hash1" {
		t.Errorf("boards не совпали: %v", creds.Boards)
	}
}

func TestLoadRejectsBothSources(t *testing.T) {
	path := writeTOML(t, `
keylink = "bproxy://abc"
[keys]
client_private = "AA"
server_public = "AA"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("keylink + [keys] одновременно должны быть ошибкой")
	}
}

func TestLoadRejectsNoCredentials(t *testing.T) {
	path := writeTOML(t, `listen = "127.0.0.1:1080"`)
	if _, err := Load(path); err == nil {
		t.Fatal("отсутствие учётных данных должно быть ошибкой")
	}
}

func TestReadBypass(t *testing.T) {
	path := writeTOML(t, `
keylink = "bproxy://abc"
bypass = ["a", "b", "c"]
`)
	got, err := ReadBypass(path)
	if err != nil {
		t.Fatalf("ReadBypass: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadBypass = %v, хочу 3 паттерна", got)
	}
}
