package runtime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
)

// PollInterval — как часто сервис ходит за конфигурацией. Тот же запрос служит
// heartbeat-ом, поэтому пауза заодно определяет точность индикатора в панели.
const PollInterval = 15 * time.Second

// ErrRestartRequested возвращается, когда оператор запросил перезапуск.
var ErrRestartRequested = errors.New("restart requested by control-plane")

type poller interface {
	Poll(ctx context.Context, report controlplane.Report) (*controlplane.Settings, error)
}

// Supervisor держит актуальные настройки и переподнимает recovery-worker,
// когда меняются параметры резервного канала.
type Supervisor struct {
	control   poller
	logger    *slog.Logger
	version   string
	startedAt time.Time
	onApply   func(controlplane.Settings) error

	mu       sync.RWMutex
	settings *controlplane.Settings
	ready    func() bool
}

func New(control poller, logger *slog.Logger, version string, onApply func(controlplane.Settings) error) *Supervisor {
	return &Supervisor{
		control: control, logger: logger, version: version,
		startedAt: time.Now().UTC(), onApply: onApply,
		ready: func() bool { return false },
	}
}

// Settings возвращает последнюю применённую конфигурацию; nil до первого успеха.
func (s *Supervisor) Settings() *controlplane.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SetReadiness подключает признак готовности резервного канала к отчётам.
func (s *Supervisor) SetReadiness(ready func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = ready
}

// WaitForSettings блокируется до первой успешной конфигурации: обслуживать
// подписки без recovery-ключа и публичного URL всё равно нельзя.
func (s *Supervisor) WaitForSettings(ctx context.Context) error {
	for {
		if err := s.once(ctx); err != nil {
			if errors.Is(err, ErrRestartRequested) || ctx.Err() != nil {
				return err
			}
			s.logger.Warn("cannot load settings from control-plane yet", "error", err)
		}
		if s.Settings() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// Run опрашивает control-plane до отмены контекста или запроса перезапуска.
func (s *Supervisor) Run(ctx context.Context) error {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.once(ctx); err != nil {
				if errors.Is(err, ErrRestartRequested) {
					return err
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				s.logger.Warn("settings poll failed", "error", err)
			}
		}
	}
}

func (s *Supervisor) once(ctx context.Context) error {
	settings, err := s.control.Poll(ctx, s.report())
	if err != nil {
		return err
	}
	if settings == nil {
		return nil // Ревизия совпала: применять нечего.
	}
	if err := s.onApply(*settings); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
	s.logger.Info(
		"applied settings from control-plane",
		"revision", settings.Revision, "enabled", settings.Enabled, "public_url", settings.PublicURL,
	)
	if settings.RestartRequested {
		return ErrRestartRequested
	}
	return nil
}

func (s *Supervisor) report() controlplane.Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := controlplane.Report{ServiceVersion: s.version, StartedAt: &s.startedAt}
	if s.settings != nil {
		revision := s.settings.Revision
		report.Revision = &revision
	}
	if s.ready != nil {
		ready := s.ready()
		report.RecoveryWatcherReady = &ready
	}
	return report
}
