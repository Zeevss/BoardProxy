package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"bproxy-control-plane/internal/domain"
)

type revisionLog struct {
	Revisions []domain.ConfigRevision `json:"revisions"`
}

func (s *Store) AppendDesired(_ context.Context, nodeID string, expectedRevision, catalogVersion uint64, config []byte, cause string) (domain.ConfigRevision, error) {
	if !domain.ValidID(nodeID) || len(config) == 0 {
		return domain.ConfigRevision{}, domain.ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.readRevisionLog(nodeID)
	if err != nil {
		return domain.ConfigRevision{}, err
	}
	current := uint64(0)
	if len(log.Revisions) > 0 {
		current = log.Revisions[len(log.Revisions)-1].Revision
	}
	if current != expectedRevision {
		return domain.ConfigRevision{}, domain.ErrConflict
	}
	digest := sha256.Sum256(config)
	revision := domain.ConfigRevision{
		NodeID: nodeID, Revision: current + 1, PreviousRevision: current,
		CatalogVersion: catalogVersion, ConfigTOML: append([]byte(nil), config...),
		ConfigSHA256: hex.EncodeToString(digest[:]), Cause: cause, UpdatedAt: time.Now().UTC(),
	}
	log.Revisions = append(log.Revisions, revision)
	if err := writeJSONAtomic(s.path("revisions", nodeID+".json"), log, 0o600); err != nil {
		return domain.ConfigRevision{}, err
	}
	// Keep the small current-state projection for operator inspection and
	// compatibility with data directories created by the prototype.
	if err := writeJSONAtomic(s.path("desired", nodeID+".json"), revision, 0o600); err != nil {
		return domain.ConfigRevision{}, err
	}
	return revision, nil
}

func (s *Store) Desired(_ context.Context, nodeID string) (domain.ConfigRevision, error) {
	if !domain.ValidID(nodeID) {
		return domain.ConfigRevision{}, domain.ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.readRevisionLog(nodeID)
	if err != nil {
		return domain.ConfigRevision{}, err
	}
	if len(log.Revisions) == 0 {
		return domain.ConfigRevision{}, domain.ErrNotFound
	}
	return cloneRevision(log.Revisions[len(log.Revisions)-1]), nil
}

func (s *Store) DesiredHistory(_ context.Context, nodeID string) ([]domain.ConfigRevision, error) {
	if !domain.ValidID(nodeID) {
		return nil, domain.ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.readRevisionLog(nodeID)
	if err != nil {
		return nil, err
	}
	if len(log.Revisions) == 0 {
		return nil, domain.ErrNotFound
	}
	history := make([]domain.ConfigRevision, len(log.Revisions))
	for index, revision := range log.Revisions {
		history[index] = cloneRevision(revision)
	}
	return history, nil
}

func (s *Store) readRevisionLog(nodeID string) (revisionLog, error) {
	raw, err := os.ReadFile(s.path("revisions", nodeID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return s.readLegacyDesired(nodeID)
	}
	if err != nil {
		return revisionLog{}, err
	}
	var log revisionLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return revisionLog{}, err
	}
	return log, nil
}

func (s *Store) readLegacyDesired(nodeID string) (revisionLog, error) {
	raw, err := os.ReadFile(s.path("desired", nodeID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return revisionLog{}, nil
	}
	if err != nil {
		return revisionLog{}, err
	}
	var revision domain.ConfigRevision
	if err := json.Unmarshal(raw, &revision); err != nil {
		return revisionLog{}, err
	}
	if revision.NodeID == "" {
		revision.NodeID = nodeID
	}
	return revisionLog{Revisions: []domain.ConfigRevision{revision}}, nil
}

func cloneRevision(revision domain.ConfigRevision) domain.ConfigRevision {
	revision.ConfigTOML = append([]byte(nil), revision.ConfigTOML...)
	return revision
}
