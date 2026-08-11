package agent

import (
	"context"
	"log/slog"
	"time"

	"bproxy-node-agent/internal/coremgr"
	"bproxy-node-agent/internal/localstore"
	statscollector "bproxy-node-agent/internal/stats"
)

const coreSupervisionInterval = 5 * time.Second

func collectTraffic(ctx context.Context, store *localstore.Store, collector *statscollector.Collector, interval time.Duration, log *slog.Logger) {
	collect := func() {
		checkpoints, events, err := collector.Collect(ctx, time.Now().UTC())
		if err != nil {
			log.Warn("collect traffic", "err", err)
		}
		if len(checkpoints) == 0 && len(events) == 0 {
			return
		}
		if err := store.CommitCollection(checkpoints, events); err != nil {
			log.Error("persist traffic collection", "err", err)
		}
	}
	collect()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

func superviseCore(ctx context.Context, core *coremgr.Manager, log *slog.Logger) {
	ticker := time.NewTicker(coreSupervisionInterval)
	defer ticker.Stop()
	for {
		ensureContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := core.Ensure(ensureContext)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Warn("ensure core", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
