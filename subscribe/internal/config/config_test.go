package config

import (
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
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_URL", "http://hub:8080")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_TOKEN", "subscriber-secret")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_TIMEOUT", "7s")

	loaded, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Listen != ":9090" || loaded.ControlPlane.URL != "http://hub:8080" {
		t.Fatalf("environment was not applied: %+v", loaded)
	}
	if loaded.ControlPlane.Timeout != 7*time.Second {
		t.Fatalf("typed environment was not applied: %+v", loaded)
	}
}

// Публичный URL, ссылка на таблицу и recovery-ключ больше не читаются из
// окружения: ими владеет control-plane, и случайно оставшиеся переменные
// не должны создавать иллюзию локальной настройки.
func TestControlPlaneOwnedSettingsAreNotReadFromEnvironment(t *testing.T) {
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_URL", "http://hub:8080")
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_TOKEN", "subscriber-secret")
	t.Setenv("SUBSCRIBE_PUBLIC_URL", "https://stale.example.com")
	t.Setenv("SUBSCRIBE_YANDEX_EDITOR_URL", "https://disk.yandex.ru/i/stale")
	t.Setenv("SUBSCRIBE_RECOVERY_PRIVATE_KEY", "base64:AAAA")

	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml")); err != nil {
		t.Fatalf("stale environment must be ignored, not rejected: %v", err)
	}
}

// Отсутствие файла не должно быть фатальным само по себе: сообщение обязано
// называть недостающее значение, а не путь к необязательному файлу.
func TestMissingFileReportsMissingValueNotMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))

	if err == nil {
		t.Fatal("expected a configuration error")
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Fatalf("error must name the missing setting, got %v", err)
	}
	if !strings.Contains(err.Error(), "control_plane.url") {
		t.Fatalf("expected the missing setting to be named, got %v", err)
	}
}

func TestBrokenFileIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("this is not = valid = toml"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected a broken configuration file to be rejected")
	}
}

func TestTokenIsRequired(t *testing.T) {
	t.Setenv("SUBSCRIBE_CONTROL_PLANE_URL", "http://hub:8080")

	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatalf("expected the control-plane token to be required")
	}
}
