package coremgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	corev1 "bproxy-core/api/control/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	coreReadyTimeout    = 15 * time.Second
	coreShutdownTimeout = 10 * time.Second
)

type Manager struct {
	mu             sync.Mutex
	binary         string
	configPath     string
	lastGoodPath   string
	controlAddress string
	stdout         io.Writer
	stderr         io.Writer
	log            *slog.Logger
	cmd            *exec.Cmd
	done           chan error
	lastErr        error
}

type RuntimeEventStream interface {
	Recv() (*corev1.CoreRuntimeEvent, error)
	Close() error
}

type grpcRuntimeEventStream struct {
	connection *grpc.ClientConn
	stream     grpc.ServerStreamingClient[corev1.CoreRuntimeEvent]
}

func (s *grpcRuntimeEventStream) Recv() (*corev1.CoreRuntimeEvent, error) { return s.stream.Recv() }
func (s *grpcRuntimeEventStream) Close() error                            { return s.connection.Close() }

func New(binary, dataDir, controlAddress string, stdout, stderr io.Writer, log *slog.Logger) *Manager {
	coreDir := filepath.Join(dataDir, "core")
	return &Manager{
		binary: binary, configPath: filepath.Join(coreDir, "config.toml"), lastGoodPath: filepath.Join(coreDir, "config.last-good.toml"), controlAddress: controlAddress,
		stdout: stdout, stderr: stderr, log: log,
	}
}

func (m *Manager) ConfigPath() string { return m.configPath }

func (m *Manager) Apply(ctx context.Context, config []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o700); err != nil {
		return 0, err
	}
	candidate, err := os.CreateTemp(filepath.Dir(m.configPath), ".candidate-*.toml")
	if err != nil {
		return 0, err
	}
	candidatePath := candidate.Name()
	defer os.Remove(candidatePath)
	if err := candidate.Chmod(0o600); err != nil {
		candidate.Close()
		return 0, err
	}
	if _, err = candidate.Write(config); err == nil {
		err = candidate.Sync()
	}
	if closeErr := candidate.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	validate := exec.CommandContext(ctx, m.binary, "serve", "--config", candidatePath, "--test")
	if output, err := validate.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("validate core config: %w: %s", err, output)
	}
	previous, previousErr := os.ReadFile(m.configPath)
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		return 0, previousErr
	}
	if err := os.Rename(candidatePath, m.configPath); err != nil {
		return 0, err
	}
	revision, err := m.activateLocked(ctx)
	if err != nil {
		rollbackErr := m.rollbackWithFreshContextLocked(previous, previousErr == nil)
		if rollbackErr != nil {
			return 0, fmt.Errorf("activate desired config: %w; rollback failed: %v", err, rollbackErr)
		}
		return 0, fmt.Errorf("activate desired config: %w; previous config restored", err)
	}
	if err := writeAtomic(m.lastGoodPath, config); err != nil {
		rollbackErr := m.rollbackWithFreshContextLocked(previous, previousErr == nil)
		if rollbackErr != nil {
			return 0, fmt.Errorf("persist last-known-good config: %w; rollback failed: %v", err, rollbackErr)
		}
		return 0, fmt.Errorf("persist last-known-good config: %w; previous config restored", err)
	}
	return revision, nil
}

func (m *Manager) rollbackWithFreshContextLocked(previous []byte, hadPrevious bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), coreShutdownTimeout+coreReadyTimeout)
	defer cancel()
	return m.rollbackLocked(ctx, previous, hadPrevious)
}

func (m *Manager) activateLocked(ctx context.Context) (uint64, error) {
	if m.runningLocked() {
		if revision, err := reloadCore(ctx, m.controlAddress); err == nil {
			return revision, nil
		} else {
			m.log.Info("core reload requires restart", "err", err)
			if err := m.stopLocked(); err != nil {
				return 0, err
			}
		}
	}
	if err := m.startLocked(); err != nil {
		return 0, err
	}
	return waitForCore(ctx, m.controlAddress)
}

func (m *Manager) rollbackLocked(ctx context.Context, previous []byte, hadPrevious bool) error {
	if err := m.stopLocked(); err != nil {
		return err
	}
	if !hadPrevious {
		if err := os.Remove(m.configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := writeAtomic(m.configPath, previous); err != nil {
		return err
	}
	if err := m.startLocked(); err != nil {
		return err
	}
	_, err := waitForCore(ctx, m.controlAddress)
	return err
}

func writeAtomic(path string, value []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err = temporary.Write(value); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (m *Manager) Ensure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningLocked() {
		if _, err := getRuntime(ctx, m.controlAddress); err == nil {
			return nil
		}
		m.log.Warn("core process is alive but control API is unavailable; restarting")
		if err := m.stopLocked(); err != nil {
			return err
		}
	}
	if _, err := os.Stat(m.configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := m.startLocked(); err != nil {
		return err
	}
	_, err := waitForCore(ctx, m.controlAddress)
	return err
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *Manager) Status(ctx context.Context) (running, ready bool, errText string) {
	m.mu.Lock()
	running = m.runningLocked()
	lastErr := m.lastErr
	m.mu.Unlock()
	if !running {
		if lastErr != nil {
			return false, false, lastErr.Error()
		}
		return false, false, ""
	}
	stats, err := coreStats(ctx, m.controlAddress)
	if err != nil {
		return true, false, err.Error()
	}
	return true, stats.GetBoardsEnabled() > 0 && stats.GetBoardsRunning() > 0, ""
}

func (m *Manager) WatchEvents(ctx context.Context, bootID string, afterSequence uint64) (RuntimeEventStream, error) {
	connection, err := grpc.NewClient(m.controlAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	stream, err := corev1.NewControlServiceClient(connection).WatchRuntimeEvents(ctx, &corev1.WatchRuntimeEventsRequest{
		BootId: bootID, AfterSequence: afterSequence,
	})
	if err != nil {
		connection.Close()
		return nil, err
	}
	return &grpcRuntimeEventStream{connection: connection, stream: stream}, nil
}

func (m *Manager) RuntimeSnapshot(ctx context.Context) (*corev1.RuntimeSnapshot, error) {
	connection, err := grpc.NewClient(m.controlAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return corev1.NewControlServiceClient(connection).GetRuntimeSnapshot(callCtx, &emptypb.Empty{})
}

func (m *Manager) runningLocked() bool {
	if m.cmd == nil {
		return false
	}
	select {
	case err := <-m.done:
		m.lastErr = err
		m.cmd, m.done = nil, nil
		return false
	default:
		return true
	}
}

func (m *Manager) startLocked() error {
	cmd := exec.Command(m.binary, "serve", "--config", m.configPath)
	cmd.Stdout, cmd.Stderr = m.stdout, m.stderr
	if err := cmd.Start(); err != nil {
		m.lastErr = err
		return err
	}
	done := make(chan error, 1)
	m.cmd, m.done, m.lastErr = cmd, done, nil
	go func() { done <- cmd.Wait() }()
	m.log.Info("core started", "pid", cmd.Process.Pid, "config", m.configPath)
	return nil
}

func (m *Manager) stopLocked() error {
	if !m.runningLocked() {
		return nil
	}
	cmd, done := m.cmd, m.done
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	timer := time.NewTimer(coreShutdownTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		m.lastErr = err
	case <-timer.C:
		_ = cmd.Process.Kill()
		m.lastErr = <-done
	}
	m.cmd, m.done = nil, nil
	return nil
}

func waitForCore(ctx context.Context, address string) (uint64, error) {
	deadline := time.NewTimer(coreReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline.C:
			return 0, errors.New("core did not expose its control API before timeout")
		case <-ticker.C:
			info, err := getRuntime(ctx, address)
			if err == nil {
				return info.GetRevision(), nil
			}
		}
	}
}

func getRuntime(ctx context.Context, address string) (*corev1.RuntimeInfo, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return corev1.NewControlServiceClient(conn).GetRuntime(callCtx, &emptypb.Empty{})
}

func reloadCore(ctx context.Context, address string) (uint64, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := corev1.NewControlServiceClient(conn).Reload(callCtx, &corev1.RevisionRequest{})
	if err != nil {
		return 0, err
	}
	return result.GetRevision(), nil
}

func coreStats(ctx context.Context, address string) (*corev1.RuntimeStats, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return corev1.NewControlServiceClient(conn).GetStats(callCtx, &emptypb.Empty{})
}
