// Пакет helperipc описывает протокол общения GUI (непривилегированного) с
// привилегированным helper-процессом, который поднимает TUN. GUI слушает
// loopback-сокет и запускает helper с повышением прав (pkexec/osascript/UAC)
// один раз за сессию; helper подключается обратно, аутентифицируется токеном и
// живёт как демон: GUI шлёт команды start/stop/shutdown, helper — события
// (hello/status/metrics/log). Секреты (keylink) не попадают в argv: bootstrap с
// токеном идёт через файл 0600, а keylink — по loopback-сокету в команде start.
package helperipc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"bproxy-core/pkg/bproxy"
)

// Bootstrap — минимальный конфиг запуска helper'а: пишется GUI в файл 0600,
// путь передаётся аргументом. Содержит только адрес для обратного подключения и
// токен аутентификации (не секрет доступа к доске).
type Bootstrap struct {
	EventAddr string `json:"eventAddr"`
	Token     string `json:"token"`
}

// SessionConfig — параметры одного подключения; передаётся в команде start уже
// по защищённому loopback-сокету (не через файл/argv).
type SessionConfig struct {
	Keylink  string   `json:"keylink"`
	Listen   string   `json:"listen"`
	Bypass   []string `json:"bypass"`
	MaxLanes int      `json:"maxLanes"`

	TunAddr string `json:"tunAddr"`
	Gateway string `json:"gateway"`
	MTU     int    `json:"mtu"`
}

// Типы событий (helper → GUI).
const (
	EventHello   = "hello"
	EventStatus  = "status"
	EventMetrics = "metrics"
	EventLog     = "log"
)

// Типы команд (GUI → helper).
const (
	CmdStart    = "start"
	CmdStop     = "stop"
	CmdShutdown = "shutdown"
	CmdBypass   = "bypass"
)

// Event — сообщение helper'а к GUI (по одному JSON на строку).
type Event struct {
	Type    string          `json:"type"`
	Token   string          `json:"token,omitempty"` // только в hello
	Status  string          `json:"status,omitempty"`
	Error   string          `json:"error,omitempty"`
	Level   string          `json:"level,omitempty"`
	Msg     string          `json:"msg,omitempty"`
	Metrics json.RawMessage `json:"metrics,omitempty"` // сериализованный MetricsDTO
}

// Command — сообщение GUI к helper'у (по одному JSON на строку).
type Command struct {
	Type   string         `json:"type"`
	Config *SessionConfig `json:"config,omitempty"` // для start
	Bypass []string       `json:"bypass,omitempty"` // для bypass
}

// WriteBootstrapFile сохраняет bootstrap во временный файл 0600 и возвращает путь.
func WriteBootstrapFile(b Bootstrap) (string, error) {
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "bproxy-helper-*.json")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// ReadBootstrapFile читает и удаляет bootstrap-файл.
func ReadBootstrapFile(path string) (Bootstrap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bootstrap{}, err
	}
	os.Remove(path)
	var b Bootstrap
	if err := json.Unmarshal(data, &b); err != nil {
		return Bootstrap{}, fmt.Errorf("helperipc: bad bootstrap: %w", err)
	}
	return b, nil
}

// --- DTO метрик (та же форма, что во фронтенде) ---

type StreamDTO struct {
	ID        uint32 `json:"id"`
	Target    string `json:"target"`
	Host      string `json:"host"` // домен из DNS-кэша, если известен
	StartedAt int64  `json:"startedAt"`
	TotalUp   uint64 `json:"totalUp"`
	TotalDown uint64 `json:"totalDown"`
	RateUp    uint64 `json:"rateUp"`
	RateDown  uint64 `json:"rateDown"`
}

type LaneDTO struct {
	ID               uint32 `json:"id"`
	CongestionWindow int    `json:"congestionWindow"`
	Inflight         int    `json:"inflight"`
	PeerWindow       int    `json:"peerWindow"`
	EffectiveWindow  int    `json:"effectiveWindow"`
	TargetPayload    int    `json:"targetPayload"`
	RTTms            int64  `json:"rttMs"`
	BaseRTTms        int64  `json:"baseRttMs"`
	ConfirmedBytes   uint64 `json:"confirmedBytes"`
	Draining         bool   `json:"draining"`
}

type MetricsDTO struct {
	Status          string      `json:"status"`
	RTTms           int64       `json:"rttMs"`
	TotalUp         uint64      `json:"totalUp"`
	TotalDown       uint64      `json:"totalDown"`
	RateUp          uint64      `json:"rateUp"`
	RateDown        uint64      `json:"rateDown"`
	RateConfirmedTx uint64      `json:"rateConfirmedTx"`
	BacklogFrames   int         `json:"backlogFrames"`
	BacklogBytes    int         `json:"backlogBytes"`
	BlockedWriters  int         `json:"blockedWriters"`
	Lanes           []LaneDTO   `json:"lanes"`
	Streams         []StreamDTO `json:"streams"`
}

// NameResolver отдаёт домен по IP (из DNS-кэша helper'а). nil — без имён.
type NameResolver interface {
	HostForIP(ip string) string
}

// MetricsToDTO переводит снимок метрик core в DTO, обогащая потоки именами из
// resolver (если задан).
func MetricsToDTO(m bproxy.Metrics, resolver NameResolver) MetricsDTO {
	dto := MetricsDTO{
		Status:          string(m.Status),
		RTTms:           m.RTT.Milliseconds(),
		TotalUp:         m.TotalTx,
		TotalDown:       m.TotalRx,
		RateUp:          m.RateTx,
		RateDown:        m.RateRx,
		RateConfirmedTx: m.RateConfirmedTx,
		BacklogFrames:   m.BacklogFrames,
		BacklogBytes:    m.BacklogBytes,
		BlockedWriters:  m.BlockedWriters,
		Lanes:           make([]LaneDTO, 0, len(m.Lanes)),
		Streams:         make([]StreamDTO, 0, len(m.Details)),
	}
	for _, lane := range m.Lanes {
		dto.Lanes = append(dto.Lanes, LaneDTO{
			ID:               lane.ID,
			CongestionWindow: lane.CongestionWindow,
			Inflight:         lane.Inflight,
			PeerWindow:       lane.PeerWindow,
			EffectiveWindow:  lane.EffectiveWindow,
			TargetPayload:    lane.TargetPayload,
			RTTms:            lane.RTT.Milliseconds(),
			BaseRTTms:        lane.BaseRTT.Milliseconds(),
			ConfirmedBytes:   lane.ConfirmedBytes,
			Draining:         lane.Draining,
		})
	}
	for _, s := range m.Details {
		host := ""
		if resolver != nil {
			host = resolver.HostForIP(hostOnly(s.Target))
		}
		dto.Streams = append(dto.Streams, StreamDTO{
			ID:        s.ID,
			Target:    s.Target,
			Host:      host,
			StartedAt: s.StartedAt.UnixMilli(),
			TotalUp:   s.Tx,
			TotalDown: s.Rx,
		})
	}
	return dto
}

// hostOnly отбрасывает порт у "ip:port"/"host:port".
func hostOnly(target string) string {
	for i := len(target) - 1; i >= 0; i-- {
		if target[i] == ':' {
			return target[:i]
		}
	}
	return target
}

// MetricsJSON сериализует метрики в JSON для поля Event.Metrics.
func MetricsJSON(m bproxy.Metrics, resolver NameResolver) json.RawMessage {
	data, err := json.Marshal(MetricsToDTO(m, resolver))
	if err != nil {
		return nil
	}
	return data
}

// DialTimeout — таймаут подключения helper'а к GUI.
const DialTimeout = 20 * time.Second
