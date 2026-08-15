package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server       Server       `toml:"server"`
	ControlPlane ControlPlane `toml:"control_plane"`
	Yandex       Yandex       `toml:"yandex"`
	Recovery     Recovery     `toml:"recovery"`
	Apps         []App        `toml:"apps"`
}

type Server struct {
	Listen    string `toml:"listen"`
	PublicURL string `toml:"public_url"`
}

type ControlPlane struct {
	URL     string        `toml:"url"`
	Token   string        `toml:"token"`
	Timeout time.Duration `toml:"timeout"`
}

type Yandex struct {
	EditorURL string `toml:"editor_url"`
}

type Recovery struct {
	KeyID      string `toml:"key_id"`
	PrivateKey string `toml:"private_key"`
}

type App struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

func Load(path string) (Config, error) {
	var result Config
	if _, err := toml.DecodeFile(path, &result); err != nil {
		return Config{}, fmt.Errorf("load subscribe config: %w", err)
	}
	if result.Server.Listen == "" {
		result.Server.Listen = ":8090"
	}
	if result.ControlPlane.Timeout == 0 {
		result.ControlPlane.Timeout = 10 * time.Second
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (c Config) Validate() error {
	for name, value := range map[string]string{
		"server.public_url":    c.Server.PublicURL,
		"control_plane.url":    c.ControlPlane.URL,
		"control_plane.token":  c.ControlPlane.Token,
		"yandex.editor_url":    c.Yandex.EditorURL,
		"recovery.key_id":      c.Recovery.KeyID,
		"recovery.private_key": c.Recovery.PrivateKey,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, raw := range map[string]string{
		"server.public_url": c.Server.PublicURL,
		"control_plane.url": c.ControlPlane.URL,
		"yandex.editor_url": c.Yandex.EditorURL,
	} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("%s must be an absolute URL", name)
		}
	}
	yandexURL, _ := url.Parse(c.Yandex.EditorURL)
	if yandexURL.Scheme != "https" || (yandexURL.Hostname() != "disk.yandex.ru" && yandexURL.Hostname() != "docs.yandex.ru" && yandexURL.Hostname() != "disk.yandex.com" && yandexURL.Hostname() != "docs.yandex.com") {
		return errors.New("yandex.editor_url must use a trusted disk.yandex or docs.yandex HTTPS host")
	}
	if c.ControlPlane.Timeout <= 0 || c.ControlPlane.Timeout > time.Minute {
		return errors.New("control_plane.timeout must be positive and at most one minute")
	}
	if _, err := DecodePrivateKey(c.Recovery.PrivateKey); err != nil {
		return err
	}
	for _, app := range c.Apps {
		if strings.TrimSpace(app.Name) == "" {
			return errors.New("app name is required")
		}
		parsed, err := url.Parse(app.URL)
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("app %q has invalid URL", app.Name)
		}
	}
	return nil
}

func DecodePrivateKey(encoded string) ([]byte, error) {
	encoded = strings.TrimPrefix(encoded, "base64:")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(raw) != 32 || !containsNonZero(raw) {
		return nil, errors.New("recovery.private_key must contain a non-zero 32-byte base64 key")
	}
	return raw, nil
}

func containsNonZero(raw []byte) bool {
	for _, value := range raw {
		if value != 0 {
			return true
		}
	}
	return false
}
