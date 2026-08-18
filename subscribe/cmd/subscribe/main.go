package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/config"
	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
	recoveryworker "github.com/Zeevss/BoardProxy/subscribe/internal/recovery"
	"github.com/Zeevss/BoardProxy/subscribe/internal/runtime"
	"github.com/Zeevss/BoardProxy/subscribe/internal/web"
	"github.com/Zeevss/BoardProxy/subscribe/recovery"
)

// version сообщается control-plane и видна в панели.
var version = "dev"

// platformLabels переводит платформу в подпись кнопки на странице подписки.
var platformLabels = map[string]string{
	"ios": "iOS", "android": "Android", "windows": "Windows", "macos": "macOS", "linux": "Linux",
}

func main() {
	configPath := flag.String("config", "config.toml", "path to the subscribe TOML configuration")
	healthcheck := flag.String("healthcheck", "", "check an HTTP health endpoint and exit")
	showVersion := flag.Bool("version", false, "print the service version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *healthcheck != "" {
		if err := checkHealth(*healthcheck); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load(*configPath)
	if err != nil {
		logger.Error("cannot load configuration", "error", err)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: settings.ControlPlane.Timeout}
	control := controlplane.New(settings.ControlPlane.URL, settings.ControlPlane.Token, httpClient)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := &application{logger: logger, control: control}
	supervisor := runtime.New(control, logger, version, app.apply)
	app.supervisor = supervisor
	supervisor.SetReadiness(app.recoveryReady)

	// HTTP поднимается сразу: /healthz должен отвечать, пока сервис ждёт настройки.
	handler := web.New(control, app.apps, app.recoveryReady)
	server := &http.Server{
		Addr:              settings.Server.Listen,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.ListenAndServe() }()

	logger.Info(
		"subscribe service started",
		"listen", settings.Server.Listen, "control_plane", settings.ControlPlane.URL, "version", version,
	)

	exitCode := 0
	if err := supervisor.WaitForSettings(ctx); err != nil {
		if errors.Is(err, runtime.ErrRestartRequested) {
			logger.Info("restart requested by control-plane")
		} else if ctx.Err() == nil {
			logger.Error("cannot obtain settings", "error", err)
			exitCode = 1
		}
	} else {
		supervisorResult := make(chan error, 1)
		go func() { supervisorResult <- supervisor.Run(ctx) }()
		select {
		case err := <-serverResult:
			if !errors.Is(err, http.ErrServerClosed) {
				logger.Error("HTTP server stopped", "error", err)
				exitCode = 1
			}
			stop()
		case err := <-supervisorResult:
			if errors.Is(err, runtime.ErrRestartRequested) {
				// Выход с нулём: перезапуск делает supervisor контейнера.
				logger.Info("restart requested by control-plane")
			} else if ctx.Err() == nil {
				logger.Error("settings supervisor stopped", "error", err)
				exitCode = 1
			}
			stop()
		case <-ctx.Done():
		}
	}

	app.stopWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("cannot gracefully stop HTTP server", "error", err)
	}
	os.Exit(exitCode)
}

// application владеет живым состоянием сервиса: списком клиентов и
// recovery-worker-ом, которые переезжают вслед за настройками.
type application struct {
	logger     *slog.Logger
	control    *controlplane.Client
	supervisor *runtime.Supervisor

	mu       sync.RWMutex
	apps_    []web.App
	worker   *recoveryworker.Worker
	stop     context.CancelFunc
	recovery recoveryParams
}

// recoveryParams — то, что определяет резервный канал. Их смена требует
// пересоздания worker-а: он держит открытую сессию к конкретной таблице.
type recoveryParams struct {
	yandexURL  string
	keyID      string
	privateKey string
}

func (a *application) apply(settings controlplane.Settings) error {
	apps := make([]web.App, 0, len(settings.Apps))
	for _, app := range settings.Apps {
		label := platformLabels[strings.ToLower(app.Platform)]
		if label == "" {
			label = app.Platform
		}
		apps = append(apps, web.App{Name: label, URL: app.URL})
	}

	a.mu.Lock()
	a.apps_ = apps
	current := a.recovery
	a.mu.Unlock()

	next := recoveryParams{
		yandexURL:  settings.YandexEditorURL,
		keyID:      settings.RecoveryKeyID,
		privateKey: settings.RecoveryPrivateKey,
	}
	if next == current {
		return nil
	}
	return a.restartWorker(next)
}

func (a *application) restartWorker(params recoveryParams) error {
	privateKey, err := recovery.DecodeKey(params.privateKey)
	if err != nil {
		return fmt.Errorf("control-plane returned an unusable recovery key: %w", err)
	}
	a.stopWorker()

	worker := recoveryworker.New(params.yandexURL, params.keyID, privateKey, a.control, a.logger)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Error("recovery worker stopped", "error", err)
		}
	}()

	a.mu.Lock()
	a.worker = worker
	a.stop = cancel
	a.recovery = params
	a.mu.Unlock()
	a.logger.Info("recovery worker reconfigured", "recovery_key_id", params.keyID)
	return nil
}

func (a *application) stopWorker() {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.worker = nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (a *application) apps() []web.App {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.apps_
}

func (a *application) recoveryReady() bool {
	a.mu.RLock()
	worker := a.worker
	a.mu.RUnlock()
	return worker != nil && worker.Ready()
}

func checkHealth(endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("healthcheck request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("healthcheck returned HTTP %d", response.StatusCode)
	}
	return nil
}
