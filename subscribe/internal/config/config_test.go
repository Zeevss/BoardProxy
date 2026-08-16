package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadExampleShape(t *testing.T) {
	raw, err := os.ReadFile("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	validKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	raw = []byte(strings.Replace(string(raw), "replace-with-32-byte-base64url-key", validKey, 1))
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ControlPlane.Timeout != 10*time.Second || loaded.Server.Listen != ":8090" {
		t.Fatalf("unexpected decoded defaults: %+v", loaded)
	}
}

func TestLoadFromEnvironmentWithoutFile(t *testing.T) {
	t.Setenv("SUBSCRIBE_LISTEN", ":9090")
	t.Setenv("SUBSCRIBE_PUBLIC_URL", "https://subscribe.example.com")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_URL", "http://hub:8080")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_TOKEN", "subscriber-secret")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_TIMEOUT", "7s")
	t.Setenv("SUBSCRIBE_YANDEX_EDITOR_URL", "https://disk.yandex.ru/i/example")
	t.Setenv("SUBSCRIBE_RECOVERY_KEY_ID", "recovery-v1")
	t.Setenv("SUBSCRIBE_RECOVERY_PRIVATE_KEY", "base64:"+base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	t.Setenv("SUBSCRIBE_APPS_JSON", `[{"name":"BoardProxy Android","url":"https://example.com/android"}]`)

	loaded, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Listen != ":9090" || loaded.ControlPlane.URL != "http://hub:8080" {
		t.Fatalf("environment was not applied: %+v", loaded)
	}
	if loaded.ControlPlane.Timeout != 7*time.Second || len(loaded.Apps) != 1 {
		t.Fatalf("typed environment was not applied: %+v", loaded)
	}
}
