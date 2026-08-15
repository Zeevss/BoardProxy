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
