package mgmt

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
)

// Serve поднимает управляющий HTTP-API на unix-сокете socketPath и обслуживает
// его, пока ctx не отменён. Локальный сокет (права файловой системы) — граница
// доступа; публичный TCP-API с аутентификацией придёт отдельно (--api-port).
func Serve(ctx context.Context, socketPath string, h http.Handler) error {
	// Убираем возможный залежавшийся сокет от прошлого запуска.
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
