package application

import (
	"context"
	"testing"
	"time"

	"bproxy-control-plane/internal/domain"
)

type sessionRepository struct {
	desired domain.ConfigRevision
	status  domain.NodeStatus
}

func (*sessionRepository) AppendDesired(context.Context, string, uint64, uint64, []byte, string) (domain.ConfigRevision, error) {
	return domain.ConfigRevision{}, nil
}
func (r *sessionRepository) Desired(context.Context, string) (domain.ConfigRevision, error) {
	return r.desired, nil
}
func (*sessionRepository) DesiredHistory(context.Context, string) ([]domain.ConfigRevision, error) {
	return nil, nil
}
func (*sessionRepository) StoreTraffic(context.Context, string, domain.TrafficKind, string, []byte) error {
	return nil
}
func (r *sessionRepository) NodeStatus(context.Context, string) (domain.NodeStatus, error) {
	if r.status.Version == 0 {
		return domain.NodeStatus{}, domain.ErrNotFound
	}
	return r.status, nil
}
func (r *sessionRepository) SaveNodeStatus(_ context.Context, status domain.NodeStatus, expected uint64) error {
	if r.status.Version != expected {
		return domain.ErrConflict
	}
	r.status = status
	return nil
}

func TestNodeSessionSendsOnlyDriftRetriesFailuresAndProjectsStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0)
	repository := &sessionRepository{desired: domain.ConfigRevision{Revision: 2, ConfigSHA256: "new"}}
	session := NewNodeSession(repository, repository, repository, "node-1", domain.AppliedState{Revision: 1, SHA256: "old"})
	if err := session.Connected(ctx, domain.NodeHello{
		BootID: "boot-1", AgentVersion: "agent-1", AppliedRevision: 1, ConfigSHA256: "old",
	}, now); err != nil {
		t.Fatal(err)
	}
	_, send, err := session.PendingDesired(ctx, now)
	if err != nil || !send {
		t.Fatalf("initial send=%t err=%v", send, err)
	}
	_, send, _ = session.PendingDesired(ctx, now)
	if send {
		t.Fatal("same desired state sent twice")
	}
	failed := domain.ApplyResult{DesiredRevision: 2, ConfigSHA256: "new", Error: "failed", AppliedAt: now}
	if err := session.RecordApply(ctx, failed, now); err != nil {
		t.Fatal(err)
	}
	_, send, _ = session.PendingDesired(ctx, now.Add(29*time.Second))
	if send {
		t.Fatal("failed desired retried before backoff")
	}
	_, send, _ = session.PendingDesired(ctx, now.Add(30*time.Second))
	if !send {
		t.Fatal("failed desired was not retried")
	}
	succeeded := domain.ApplyResult{DesiredRevision: 2, RuntimeRevision: 8, ConfigSHA256: "new", AppliedAt: now.Add(30 * time.Second)}
	if err := session.RecordApply(ctx, succeeded, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, send, _ = session.PendingDesired(ctx, now.Add(time.Minute))
	if send {
		t.Fatal("applied desired state sent again")
	}
	if repository.status.DesiredRevision != 2 || repository.status.AppliedRevision != 2 || repository.status.Drifted() {
		t.Fatalf("unexpected status after apply: %+v", repository.status)
	}
	if err := session.RecordHeartbeat(ctx, domain.NodeHeartbeat{
		SampledAt: now.Add(time.Minute), CoreRunning: true, CoreReady: true, AppliedRevision: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if !repository.status.CoreReady || !repository.status.Connected {
		t.Fatalf("heartbeat projection=%+v", repository.status)
	}
	if err := session.Disconnected(ctx, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if repository.status.Connected || repository.status.CoreReady {
		t.Fatalf("disconnect projection=%+v", repository.status)
	}
}

func TestStaleSessionCannotMarkNewBootDisconnected(t *testing.T) {
	repository := &sessionRepository{status: domain.NodeStatus{NodeID: "node-1", BootID: "new-boot", Connected: true, Version: 1}}
	session := NewNodeSession(repository, repository, repository, "node-1", domain.AppliedState{})
	session.bootID = "old-boot"
	if err := session.Disconnected(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if !repository.status.Connected || repository.status.BootID != "new-boot" {
		t.Fatal("stale session overwrote the new connection")
	}
}

func TestHeartbeatRuntimeResetTriggersDesiredReplay(t *testing.T) {
	now := time.Unix(100, 0)
	repository := &sessionRepository{desired: domain.ConfigRevision{Revision: 2, ConfigSHA256: "sha"}}
	session := NewNodeSession(repository, repository, repository, "node-1", domain.AppliedState{Revision: 2, SHA256: "sha"})
	if _, send, err := session.PendingDesired(context.Background(), now); err != nil || send {
		t.Fatalf("up-to-date send=%t err=%v", send, err)
	}
	if err := session.RecordHeartbeat(context.Background(), domain.NodeHeartbeat{
		SampledAt: now, CoreRunning: true, AppliedRevision: 0,
	}); err != nil {
		t.Fatal(err)
	}
	revision, send, err := session.PendingDesired(context.Background(), now)
	if err != nil || !send || revision.Revision != 2 {
		t.Fatalf("reset replay desired=%+v send=%t err=%v", revision, send, err)
	}
}

func TestStatusSaveRetriesOptimisticConflict(t *testing.T) {
	repository := &conflictingStatusRepository{sessionRepository: sessionRepository{}}
	session := NewNodeSession(repository, repository, repository, "node-1", domain.AppliedState{})
	if err := session.Connected(context.Background(), domain.NodeHello{BootID: "boot"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if repository.conflicts != 1 || repository.status.Version == 0 {
		t.Fatalf("conflicts=%d status=%+v", repository.conflicts, repository.status)
	}
}

type conflictingStatusRepository struct {
	sessionRepository
	conflicts int
}

func (r *conflictingStatusRepository) SaveNodeStatus(ctx context.Context, status domain.NodeStatus, expected uint64) error {
	if r.conflicts == 0 {
		r.conflicts++
		return domain.ErrConflict
	}
	return r.sessionRepository.SaveNodeStatus(ctx, status, expected)
}
