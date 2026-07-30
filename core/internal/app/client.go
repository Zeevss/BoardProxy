package app

import (
	"context"
	"log/slog"

	"bproxy-core/internal/clientcore"
	"bproxy-core/internal/config"
	"bproxy-core/internal/mux"
	"bproxy-core/internal/proxy"
)

// DialClient присоединяется к доске, проходит rendezvous с рукопожатием и
// мигрирует на выданную страницу, возвращая готовую (уже зашифрованную)
// mux-сессию. Владение сессией переходит вызывающему: её Close каскадно закроет
// и link, и сессию доски. Отдельно от RunClient — чтобы наблюдаемый клиент
// (pkg/bproxy) мог собирать метрики с этой сессии, не дублируя поток подключения.
func DialClient(ctx context.Context, cfg config.Config, log *slog.Logger) (*mux.Session, error) {
	return clientcore.Dial(ctx, cfg, log)
}

// RunClient присоединяется к доске, проходит rendezvous, мигрирует на выданную
// страницу и обслуживает локальный SOCKS5/HTTP прокси, пока ctx не отменён.
func RunClient(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	m, err := DialClient(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer m.Close()
	return proxy.Serve(ctx, cfg.Client.Listen, m, log, proxy.Options{})
}
