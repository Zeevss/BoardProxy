// Пакет clientconfig читает клиентский конфиг BoardProxy в формате TOML и
// собирает из него bproxy.Config. Конфиг задаёт всё, что нужно клиенту:
// адрес прослушивания, уровень логов, local-dns/system-proxy, список bypass и
// учётные данные — либо готовой строкой keylink, либо явными ключами и списком
// досок (тогда keylink собирается из них).
package clientconfig

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"bproxy-core/internal/keylink"
	"bproxy-core/pkg/bproxy"

	"github.com/BurntSushi/toml"
)

// file — TOML-схема клиентского конфига.
type file struct {
	Listen       string   `toml:"listen"`
	Log          string   `toml:"log"`
	LocalDNS     bool     `toml:"local_dns"`
	SystemProxy  bool     `toml:"system_proxy"`
	EnableUDP    bool     `toml:"enable_udp"`
	RetryInitial bool     `toml:"retry_initial_connection"`
	MaxLanes     int      `toml:"max_lanes"`
	Bypass       []string `toml:"bypass"`
	Keylink      string   `toml:"keylink"`
	Board        string   `toml:"board"`    // переопределяет доску из keylink
	APIBase      string   `toml:"api_base"` // REST-точка доски (пусто — дефолт)
	HubPage      string   `toml:"hub_page"` // общий hub-слайд; обычно не задаётся
	Keys         *keys    `toml:"keys"`     // альтернатива keylink
}

// keys — явные учётные данные (альтернатива готовой строке keylink). Ключи —
// base64 (стандартный алфавит) сырых 32 байт X25519.
type keys struct {
	ClientPrivate string   `toml:"client_private"`
	ServerPublic  string   `toml:"server_public"`
	Boards        []string `toml:"boards"`
	Label         string   `toml:"label"`
}

// Load читает TOML-конфиг по пути path и собирает bproxy.Config. Требует ровно
// один источник учётных данных: либо keylink, либо секцию [keys].
func Load(path string) (bproxy.Config, error) {
	var f file
	metadata, err := toml.DecodeFile(path, &f)
	if err != nil {
		return bproxy.Config{}, fmt.Errorf("clientconfig: %w", err)
	}
	if err := rejectUnknown(metadata); err != nil {
		return bproxy.Config{}, err
	}
	return f.toConfig()
}

func (f file) toConfig() (bproxy.Config, error) {
	if f.Log != "" {
		switch strings.ToLower(strings.TrimSpace(f.Log)) {
		case "debug", "info", "warn", "warning", "error":
		default:
			return bproxy.Config{}, fmt.Errorf("clientconfig: unsupported log level %q", f.Log)
		}
	}
	if f.MaxLanes < 0 || f.MaxLanes > 32 {
		return bproxy.Config{}, fmt.Errorf("clientconfig: max_lanes must be between 1 and 32, or 0 for default")
	}
	link, err := f.resolveKeylink()
	if err != nil {
		return bproxy.Config{}, err
	}
	return bproxy.Config{
		Keylink:      link,
		Listen:       f.Listen,
		Board:        f.Board,
		APIBase:      f.APIBase,
		HubPage:      f.HubPage,
		LogLevel:     f.Log,
		BypassList:   f.Bypass,
		LocalDNS:     f.LocalDNS,
		SystemProxy:  f.SystemProxy,
		EnableUDP:    f.EnableUDP,
		RetryInitial: f.RetryInitial,
		MaxLanes:     f.MaxLanes,
	}, nil
}

// resolveKeylink возвращает keylink: либо прямо заданный, либо собранный из
// секции [keys]. Оба источника одновременно — ошибка (неоднозначность).
func (f file) resolveKeylink() (string, error) {
	switch {
	case f.Keylink != "" && f.Keys != nil:
		return "", fmt.Errorf("clientconfig: заданы и keylink, и [keys] — оставьте что-то одно")
	case f.Keylink != "":
		return f.Keylink, nil
	case f.Keys != nil:
		return f.Keys.build()
	default:
		return "", fmt.Errorf("clientconfig: нет учётных данных — задайте keylink или секцию [keys]")
	}
}

func (k keys) build() (string, error) {
	priv, err := base64.StdEncoding.DecodeString(k.ClientPrivate)
	if err != nil {
		return "", fmt.Errorf("clientconfig: keys.client_private не base64: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(k.ServerPublic)
	if err != nil {
		return "", fmt.Errorf("clientconfig: keys.server_public не base64: %w", err)
	}
	// keylink.Build проверяет длины ключей и требует непустой список досок.
	link, err := keylink.Build(priv, pub, k.Boards, k.Label)
	if err != nil {
		return "", fmt.Errorf("clientconfig: [keys]: %w", err)
	}
	return link, nil
}

// ReadBypass перечитывает только список bypass из конфига — для реактивного
// обновления на лету без разбора остального (учётные данные не трогаем).
func ReadBypass(path string) ([]string, error) {
	var f file
	metadata, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("clientconfig: %w", err)
	}
	if err := rejectUnknown(metadata); err != nil {
		return nil, err
	}
	return f.Bypass, nil
}

func rejectUnknown(metadata toml.MetaData) error {
	undecoded := metadata.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	fields := make([]string, 0, len(undecoded))
	for _, key := range undecoded {
		fields = append(fields, key.String())
	}
	sort.Strings(fields)
	return fmt.Errorf("clientconfig: unknown fields: %s", strings.Join(fields, ", "))
}
