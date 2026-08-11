package cli

import (
	"context"
	"log/slog"
	"time"

	"bproxy-control-plane/internal/application"
)

const catalogReconcileInterval = 10 * time.Second

func reconcileCatalogs(ctx context.Context, catalogs *application.Catalogs, log *slog.Logger) {
	reconcile := func(cause string) {
		if err := catalogs.ReconcileAll(ctx, cause); err != nil && ctx.Err() == nil {
			log.Warn("catalog reconciliation failed", "err", err)
		}
	}
	reconcile("startup.reconcile")
	ticker := time.NewTicker(catalogReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile("periodic.reconcile")
		}
	}
}
