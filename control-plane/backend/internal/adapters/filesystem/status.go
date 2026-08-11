package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"bproxy-control-plane/internal/domain"
)

func (s *Store) NodeStatus(_ context.Context, nodeID string) (domain.NodeStatus, error) {
	if !domain.ValidID(nodeID) {
		return domain.NodeStatus{}, domain.ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readNodeStatus(nodeID)
}

func (s *Store) SaveNodeStatus(_ context.Context, status domain.NodeStatus, expectedVersion uint64) error {
	if !domain.ValidID(status.NodeID) || status.Version != expectedVersion+1 {
		return domain.ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readNodeStatus(status.NodeID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if current.Version != expectedVersion {
		return domain.ErrConflict
	}
	return writeJSONAtomic(s.path("status", status.NodeID+".json"), status, 0o600)
}

func (s *Store) readNodeStatus(nodeID string) (domain.NodeStatus, error) {
	raw, err := os.ReadFile(s.path("status", nodeID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return domain.NodeStatus{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.NodeStatus{}, err
	}
	var status domain.NodeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return domain.NodeStatus{}, err
	}
	return status, nil
}
