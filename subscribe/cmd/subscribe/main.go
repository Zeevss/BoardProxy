package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/config"
	"github.com/Zeevss/BoardProxy/subscribe/internal/controlplane"
	recoveryworker "github.com/Zeevss/BoardProxy/subscribe/internal/recovery"
	"github.com/Zeevss/BoardProxy/subscribe/internal/web"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	"github.com/Zeevss/BoardProxy/subscribe/recovery"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to the subscribe TOML configuration")
	printURL := flag.Bool("print-url", false, "read issued subscription JSON from stdin, print its URL, and exit")
	printPublicKey := flag.Bool("print-public-key", false, "print the recovery server public key and exit")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load(*configPath)
	if err != nil {
		logger.Error("cannot load configuration", "error", err)
		os.Exit(1)
	}
	privateKey, err := config.DecodePrivateKey(settings.Recovery.PrivateKey)
	if err != nil {
		logger.Error("cannot decode recovery key", "error", err)
		os.Exit(1)
	}
	publicKey, err := recovery.PublicKey(privateKey)
	if err != nil {
		logger.Error("cannot derive recovery public key", "error", err)
		os.Exit(1)
	}
	if *printPublicKey {
		fmt.Println(base64.RawURLEncoding.EncodeToString(publicKey))
		return
	}
	if *printURL {
		var issued struct {
			Token                    string `json:"token"`
			RecoveryClientPrivateKey string `json:"recoveryClientPrivateKey"`
		}
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10)).Decode(&issued); err != nil {
			logger.Error("cannot read issued subscription JSON from stdin", "error", err)
			os.Exit(2)
		}
		if issued.Token == "" || issued.RecoveryClientPrivateKey == "" {
			logger.Error("issued subscription JSON lacks token or recoveryClientPrivateKey")
			os.Exit(2)
		}
		completeURL, err := protocol.BuildURL(settings.Server.PublicURL, issued.Token, protocol.Capsule{
			Version: 1, YandexURL: settings.Yandex.EditorURL, RecoveryKeyID: settings.Recovery.KeyID,
			ClientPrivateKey:     issued.RecoveryClientPrivateKey,
			RecoveryServerPublic: protocol.EncodeKey(publicKey),
		})
		if err != nil {
			logger.Error("cannot build subscription URL", "error", err)
			os.Exit(2)
		}
		fmt.Println(completeURL)
		return
	}

	httpClient := &http.Client{Timeout: settings.ControlPlane.Timeout}
	control := controlplane.New(settings.ControlPlane.URL, settings.ControlPlane.Token, httpClient)
	worker := recoveryworker.New(
		settings.Yandex.EditorURL, settings.Recovery.KeyID, privateKey, control, logger,
	)
	handler := web.New(control, settings.Apps, worker.Ready)
	server := &http.Server{
		Addr:              settings.Server.Listen,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Run(ctx) }()
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.ListenAndServe() }()

	logger.Info(
		"subscribe service started",
		"listen", settings.Server.Listen,
		"public_url", settings.Server.PublicURL,
		"recovery_key_id", settings.Recovery.KeyID,
		"recovery_public_key", base64.RawURLEncoding.EncodeToString(publicKey),
	)

	select {
	case err := <-serverResult:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
		}
		stop()
	case err := <-workerResult:
		if !errors.Is(err, context.Canceled) {
			logger.Error("recovery worker stopped", "error", err)
		}
		stop()
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("cannot gracefully stop HTTP server", "error", err)
	}
}
