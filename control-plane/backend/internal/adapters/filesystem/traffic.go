package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"bproxy-control-plane/internal/domain"
)

func (s *Store) StoreTraffic(_ context.Context, nodeID string, kind domain.TrafficKind, batchID string, payload []byte) error {
	if !domain.ValidID(nodeID) || !domain.ValidID(batchID) || (kind != domain.InterfaceTraffic && kind != domain.UserTraffic) {
		return domain.ErrInvalidID
	}
	directory := s.path("traffic", string(kind), nodeID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, batchID+".pb"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = file.Write(payload); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
