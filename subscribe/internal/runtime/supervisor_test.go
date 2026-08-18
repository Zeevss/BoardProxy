package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
)

type stubPoller struct {
	responses []*controlplane.Settings
	errs      []error
	reports   []controlplane.Report
}

func (s *stubPoller) Poll(_ context.Context, report controlplane.Report) (*controlplane.Settings, error) {
	s.reports = append(s.reports, report)
	index := len(s.reports) - 1
	if index < len(s.errs) && s.errs[index] != nil {
		return nil, s.errs[index]
	}
	if index < len(s.responses) {
		return s.responses[index], nil
	}
	return nil, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func settings(revision int64) *controlplane.Settings {
	return &controlplane.Settings{
		Revision: revision, Enabled: true, PublicURL: "https://subscribe.example.com",
		YandexEditorURL: "https://disk.yandex.ru/i/sheet", RecoveryKeyID: "recovery-1",
		RecoveryPrivateKey: "AAAA",
	}
}

func TestWaitForSettingsAppliesFirstConfig(t *testing.T) {
	control := &stubPoller{responses: []*controlplane.Settings{settings(4)}}
	applied := 0
	supervisor := New(control, quiet(), "1.0.0", func(controlplane.Settings) error { applied++; return nil })

	if err := supervisor.WaitForSettings(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected settings to be applied once, got %d", applied)
	}
	if got := supervisor.Settings(); got == nil || got.Revision != 4 {
		t.Fatalf("expected revision 4, got %#v", got)
	}
}

func TestReportCarriesAppliedRevision(t *testing.T) {
	control := &stubPoller{responses: []*controlplane.Settings{settings(7), nil}}
	supervisor := New(control, quiet(), "1.2.3", func(controlplane.Settings) error { return nil })

	if err := supervisor.WaitForSettings(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := supervisor.once(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Первый запрос уходит без ревизии, второй — уже с применённой.
	if control.reports[0].Revision != nil {
		t.Fatalf("expected no revision on the first report")
	}
	if control.reports[1].Revision == nil || *control.reports[1].Revision != 7 {
		t.Fatalf("expected applied revision 7 on the second report")
	}
	if control.reports[1].ServiceVersion != "1.2.3" {
		t.Fatalf("expected the service version to be reported")
	}
}

func TestRestartRequestStopsTheService(t *testing.T) {
	restarting := settings(9)
	restarting.RestartRequested = true
	control := &stubPoller{responses: []*controlplane.Settings{restarting}}
	supervisor := New(control, quiet(), "1.0.0", func(controlplane.Settings) error { return nil })

	err := supervisor.once(context.Background())

	if !errors.Is(err, ErrRestartRequested) {
		t.Fatalf("expected a restart request, got %v", err)
	}
	// Настройки всё равно применяются: перезапуск не должен терять свежую ревизию.
	if got := supervisor.Settings(); got == nil || got.Revision != 9 {
		t.Fatalf("expected settings to be applied before restarting, got %#v", got)
	}
}

func TestFreshStartAfterRestartDoesNotLoop(t *testing.T) {
	// Control-plane отдаёт перезапуск ровно один раз, поэтому после старта
	// сервис получает обычную конфигурацию и продолжает работать.
	control := &stubPoller{responses: []*controlplane.Settings{settings(9)}}
	supervisor := New(control, quiet(), "1.0.0", func(controlplane.Settings) error { return nil })

	if err := supervisor.WaitForSettings(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := supervisor.once(context.Background()); err != nil {
		t.Fatalf("expected the service to keep running, got %v", err)
	}
}

func TestPollErrorDoesNotDropKnownSettings(t *testing.T) {
	control := &stubPoller{
		responses: []*controlplane.Settings{settings(3), nil},
		errs:      []error{nil, errors.New("control-plane unreachable")},
	}
	supervisor := New(control, quiet(), "1.0.0", func(controlplane.Settings) error { return nil })
	if err := supervisor.WaitForSettings(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := supervisor.once(context.Background()); err == nil {
		t.Fatalf("expected the poll error to surface")
	}
	// Недоступный хаб не должен обнулять конфигурацию работающего сервиса.
	if got := supervisor.Settings(); got == nil || got.Revision != 3 {
		t.Fatalf("expected settings to survive a failed poll, got %#v", got)
	}
}

func TestApplyFailureKeepsPreviousSettings(t *testing.T) {
	control := &stubPoller{responses: []*controlplane.Settings{settings(2), settings(3)}}
	failing := false
	supervisor := New(control, quiet(), "1.0.0", func(controlplane.Settings) error {
		if failing {
			return errors.New("cannot restart recovery worker")
		}
		return nil
	})
	if err := supervisor.WaitForSettings(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failing = true
	if err := supervisor.once(context.Background()); err == nil {
		t.Fatalf("expected the apply error to surface")
	}
	// Неприменённая ревизия не должна отражаться в отчёте как применённая.
	if got := supervisor.Settings(); got == nil || got.Revision != 2 {
		t.Fatalf("expected revision 2 to remain applied, got %#v", got)
	}
}
