// Package mgmt is a deliberately read-only HTTP observability adapter. Desired
// state mutations belong to the gRPC control API; this package cannot make the
// core stateful again.
package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"bproxy-core/internal/logging"
	"bproxy-core/internal/telemetry"
)

type StatsProvider interface {
	Stats() telemetry.Stats
}

func Handler(stats StatsProvider, logs *logging.Buffer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		snapshot := stats.Stats()
		status := http.StatusOK
		state := "ready"
		if snapshot.BoardsEnabled == 0 || snapshot.BoardsRunning == 0 {
			status = http.StatusServiceUnavailable
			state = "not_ready"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		writeJSON(w, map[string]any{
			"status": state, "boards_enabled": snapshot.BoardsEnabled, "boards_running": snapshot.BoardsRunning,
		})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, stats.Stats())
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		limit := 500
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 1000 {
				limit = parsed
			}
		}
		if logs == nil {
			writeJSON(w, []logging.Entry{})
			return
		}
		writeJSON(w, logs.Entries(limit))
	})
	return mux
}

func Serve(ctx context.Context, address string, handler http.Handler) error {
	srv := &http.Server{
		Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
