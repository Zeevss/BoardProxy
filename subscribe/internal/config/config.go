package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config — всё, что сервис знает о себе сам. Публичный URL, ссылка на Яндекс
// Таблицу, recovery-ключ и ссылки на клиенты сюда намеренно не входят: ими
// владеет control-plane, и сервис забирает их по своему токену.
type Config struct {
	Server       Server       `toml:"server"`
	ControlPlane ControlPlane `toml:"control_plane"`
}

type Server struct {
	Listen string `toml:"listen"`
}

type ControlPlane struct {
	URL     string        `toml:"url"`
	Token   string        `toml:"token"`
	Timeout time.Duration `toml:"timeout"`
}

func Load(path string) (Config, error) {
	var result Config
	if _, err := toml.DecodeFile(path, &result); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Существующий, но битый файл — ошибка. Отсутствующий — нормальный режим
		// Compose, где всё приходит окружением; чего именно не хватает, скажет
		// Validate, а не отказ прочитать файл.
		return Config{}, fmt.Errorf("load subscribe config: %w", err)
	}
	if err := applyEnvironment(&result); err != nil {
		return Config{}, err
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

func applyEnvironment(result *Config) error {
	overrides := map[string]*string{
		"SUBSCRIBE_LISTEN":              &result.Server.Listen,
		"SUBSCRIBE_CONTROL_PLANE_URL":   &result.ControlPlane.URL,
		"SUBSCRIBE_CONTROL_PLANE_TOKEN": &result.ControlPlane.Token,
	}
	for name, target := range overrides {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	if value, ok := os.LookupEnv("SUBSCRIBE_CONTROL_PLANE_TIMEOUT"); ok {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("SUBSCRIBE_CONTROL_PLANE_TIMEOUT must be a Go duration: %w", err)
		}
		result.ControlPlane.Timeout = parsed
	}
	return nil
}

func (c Config) Validate() error {
	for name, value := range map[string]string{
		"control_plane.url":   c.ControlPlane.URL,
		"control_plane.token": c.ControlPlane.Token,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	parsed, err := url.Parse(c.ControlPlane.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("control_plane.url must be an absolute URL")
	}
	if c.ControlPlane.Timeout <= 0 || c.ControlPlane.Timeout > time.Minute {
		return errors.New("control_plane.timeout must be positive and at most one minute")
	}
	return nil
}
