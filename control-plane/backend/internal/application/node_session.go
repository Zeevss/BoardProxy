package application

import (
	"context"
	"errors"
	"time"

	"bproxy-control-plane/internal/domain"
	"bproxy-control-plane/internal/ports"
)

const (
	desiredRetryDelay = 30 * time.Second
	statusSaveRetries = 4
)

type NodeSession struct {
	desired    ports.DesiredRevisions
	statuses   ports.NodeStatusStore
	traffic    ports.TrafficSink
	nodeID     string
	bootID     string
	applied    domain.AppliedState
	lastSent   domain.AppliedState
	retryAfter time.Time
}

func NewNodeSession(desired ports.DesiredRevisions, statuses ports.NodeStatusStore, traffic ports.TrafficSink, nodeID string, applied domain.AppliedState) *NodeSession {
	return &NodeSession{desired: desired, statuses: statuses, traffic: traffic, nodeID: nodeID, applied: applied}
}

func (s *NodeSession) NodeID() string { return s.nodeID }

func (s *NodeSession) Connected(ctx context.Context, hello domain.NodeHello, now time.Time) error {
	s.bootID = hello.BootID
	return updateNodeStatus(ctx, s.statuses, s.nodeID, func(status *domain.NodeStatus) bool {
		status.Connected = true
		status.BootID = hello.BootID
		status.AgentVersion = hello.AgentVersion
		status.CoreVersion = hello.CoreVersion
		status.AppliedRevision = hello.AppliedRevision
		status.ConfigSHA256 = hello.ConfigSHA256
		status.LastSeen = now.UTC()
		return true
	})
}

func (s *NodeSession) PendingDesired(ctx context.Context, now time.Time) (domain.ConfigRevision, bool, error) {
	if now.Before(s.retryAfter) {
		return domain.ConfigRevision{}, false, nil
	}
	desired, err := s.desired.Desired(ctx, s.nodeID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ConfigRevision{}, false, nil
		}
		return domain.ConfigRevision{}, false, err
	}
	if err := updateNodeStatus(ctx, s.statuses, s.nodeID, func(status *domain.NodeStatus) bool {
		if status.DesiredRevision == desired.Revision {
			return false
		}
		status.DesiredRevision = desired.Revision
		return true
	}); err != nil {
		return domain.ConfigRevision{}, false, err
	}
	current := domain.AppliedState{Revision: desired.Revision, SHA256: desired.ConfigSHA256}
	if desired.Revision < s.applied.Revision || current == s.applied || current == s.lastSent {
		return domain.ConfigRevision{}, false, nil
	}
	s.lastSent = current
	return desired, true, nil
}

func (s *NodeSession) RecordApply(ctx context.Context, result domain.ApplyResult, now time.Time) error {
	if result.Error == "" {
		s.applied = domain.AppliedState{Revision: result.DesiredRevision, SHA256: result.ConfigSHA256}
		s.retryAfter = time.Time{}
	} else {
		s.lastSent = domain.AppliedState{}
		s.retryAfter = now.Add(desiredRetryDelay)
	}
	return updateNodeStatus(ctx, s.statuses, s.nodeID, func(status *domain.NodeStatus) bool {
		copy := result
		status.LastApply = &copy
		status.DesiredRevision = max(status.DesiredRevision, result.DesiredRevision)
		status.LastSeen = now.UTC()
		status.LastError = result.Error
		if result.Error == "" {
			status.AppliedRevision = result.DesiredRevision
			status.ConfigSHA256 = result.ConfigSHA256
		}
		return true
	})
}

func (s *NodeSession) RecordHeartbeat(ctx context.Context, heartbeat domain.NodeHeartbeat) error {
	if heartbeat.AppliedRevision != s.applied.Revision {
		// A core restart can lose the in-memory runtime revision while the node
		// stream remains connected. Clear the digest so the next reconcile sends
		// the authoritative snapshot again.
		s.applied = domain.AppliedState{Revision: heartbeat.AppliedRevision}
		s.lastSent = domain.AppliedState{}
		s.retryAfter = time.Time{}
	}
	return updateNodeStatus(ctx, s.statuses, s.nodeID, func(status *domain.NodeStatus) bool {
		status.CoreRunning = heartbeat.CoreRunning
		status.CoreReady = heartbeat.CoreReady
		status.AppliedRevision = heartbeat.AppliedRevision
		status.LastError = heartbeat.Error
		status.LastSeen = heartbeat.SampledAt.UTC()
		return true
	})
}

func (s *NodeSession) Disconnected(ctx context.Context, now time.Time) error {
	return updateNodeStatus(ctx, s.statuses, s.nodeID, func(status *domain.NodeStatus) bool {
		if status.BootID != s.bootID {
			return false // a newer session already owns the projection
		}
		status.Connected = false
		status.CoreReady = false
		status.LastSeen = now.UTC()
		return true
	})
}

func (s *NodeSession) StoreTraffic(ctx context.Context, kind domain.TrafficKind, batchID string, payload []byte) error {
	return s.traffic.StoreTraffic(ctx, s.nodeID, kind, batchID, payload)
}

func updateNodeStatus(ctx context.Context, statuses ports.NodeStatusStore, nodeID string, mutate func(*domain.NodeStatus) bool) error {
	for attempt := 0; attempt < statusSaveRetries; attempt++ {
		status, err := statuses.NodeStatus(ctx, nodeID)
		expectedVersion := uint64(0)
		if err == nil {
			expectedVersion = status.Version
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		} else {
			status.NodeID = nodeID
		}
		if !mutate(&status) {
			return nil
		}
		status.Version = expectedVersion + 1
		if err := statuses.SaveNodeStatus(ctx, status, expectedVersion); err == nil {
			return nil
		} else if !errors.Is(err, domain.ErrConflict) {
			return err
		}
	}
	return domain.ErrConflict
}
