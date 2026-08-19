package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"bproxy-node-agent/internal/coremgr"
	"bproxy-node-agent/internal/identity"
	"bproxy-node-agent/internal/localstore"
	"bproxy-node-agent/internal/nodeconfig"
	statscollector "bproxy-node-agent/internal/stats"
)

const (
	initialReconnectDelay = time.Second
	maximumReconnectDelay = 30 * time.Second
)

var errCertificateRenewal = errors.New("node certificate renewal is due")

type Service struct {
	config   nodeconfig.Config
	version  string
	identity *identity.Identity
	store    stateStore
	core     coreRuntime
	state    appliedState
	bootID   string
	// Monotonic within bootID: lets the hub order reports without trusting clocks.
	seq        uint64
	applyError string
	startedAt  time.Time
	restart    func()
	log        *slog.Logger
}

// requestRestart stops the agent process. The hub tracks delivery, so a command
// acted upon once is never offered again.
func (s *Service) requestRestart() {
	if s.restart != nil {
		s.restart()
	}
}

func (s *Service) uptimeSeconds() int64 {
	if s.startedAt.IsZero() {
		return 0
	}
	return int64(time.Since(s.startedAt).Seconds())
}

func (s *Service) coreVersion(ready bool) string {
	if !ready {
		return ""
	}
	return s.version
}

type stateStore interface {
	PutCheckpoint(string, []byte) error
	Pending() ([]localstore.Pending, error)
	Ack(string) error
	Changes() <-chan struct{}
}

type coreRuntime interface {
	Apply(context.Context, []byte) (uint64, error)
	Status(context.Context) (bool, bool, string)
}

func Run(ctx context.Context, config nodeconfig.Config, version string, stdout, stderr io.Writer, log *slog.Logger) error {
	store, err := localstore.OpenWithOutboxLimit(config.DataDirectory, config.MaxOutboxBytes)
	if err != nil {
		return err
	}
	defer store.Close()
	core := coremgr.New(config.CoreBinary, config.DataDirectory, config.CoreControl, stdout, stderr, log)
	defer core.Stop()
	state, err := loadState(store)
	if err != nil {
		return err
	}
	runContext, stop := context.WithCancel(ctx)
	defer stop()
	service := &Service{
		config: config, version: version, store: store, core: core,
		state: state, bootID: randomID(), startedAt: time.Now(), restart: stop, log: log,
	}
	collector := statscollector.New(config.Interfaces, config.SysClassNet, config.CoreControl, store)
	go collectTraffic(runContext, store, collector, config.CollectInterval, log)
	go superviseCore(runContext, core, log)
	go collectCoreRuntimeEvents(runContext, core, store, log)
	return service.reconnect(runContext)
}

func (s *Service) reconnect(ctx context.Context) error {
	delay := initialReconnectDelay
	for ctx.Err() == nil {
		connectedAt := time.Now()
		err := s.openSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errCertificateRenewal) {
			s.log.Info("rotating node certificate")
			delay = initialReconnectDelay
			continue
		}
		if time.Since(connectedAt) >= s.config.Heartbeat {
			delay = initialReconnectDelay
		}
		s.log.Warn("hub watch disconnected", "err", err, "retry_in", delay)
		if err := wait(ctx, delay); err != nil {
			return nil
		}
		delay = min(delay*2, maximumReconnectDelay)
	}
	return nil
}

func (s *Service) openSession(ctx context.Context) error {
	currentIdentity, err := identity.Ensure(ctx, s.config.DataDirectory, s.config.BootstrapSecret, s.version)
	if err != nil {
		return err
	}
	s.identity = currentIdentity
	return s.connect(ctx)
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
