package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"bproxy-control-plane/internal/domain"
)

func (s *Store) AppendAudit(_ context.Context, event domain.AuditEvent) error {
	if !domain.ValidID(event.NodeID) || !domain.ValidID(event.ID) || event.Action == "" || event.Actor == "" {
		return domain.ErrInvalidState
	}
	directory := s.path("audit", event.NodeID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	err := writeJSONExclusive(filepath.Join(directory, event.ID+".json"), event)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}
