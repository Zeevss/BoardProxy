package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bproxy-control-plane/internal/domain"
)

func (s *Store) Catalog(_ context.Context, nodeID string) (domain.Catalog, error) {
	if !domain.ValidID(nodeID) {
		return domain.Catalog{}, domain.ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readCatalog(nodeID)
}

func (s *Store) ListCatalogs(_ context.Context) ([]domain.Catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.path("catalogs"))
	if err != nil {
		return nil, err
	}
	var catalogs []domain.Catalog
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		nodeID := strings.TrimSuffix(entry.Name(), ".json")
		catalog, err := s.readCatalog(nodeID)
		if err != nil {
			return nil, err
		}
		catalogs = append(catalogs, catalog)
	}
	sort.Slice(catalogs, func(i, j int) bool { return catalogs[i].Node.ID < catalogs[j].Node.ID })
	return catalogs, nil
}

func (s *Store) SaveCatalog(_ context.Context, catalog domain.Catalog, expectedVersion uint64) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readCatalog(catalog.Node.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	actualVersion := current.Version
	if actualVersion != expectedVersion || catalog.Version != expectedVersion+1 {
		return domain.ErrConflict
	}
	return writeJSONAtomic(s.path("catalogs", catalog.Node.ID+".json"), catalog, 0o600)
}

func (s *Store) readCatalog(nodeID string) (domain.Catalog, error) {
	raw, err := os.ReadFile(s.path("catalogs", nodeID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return domain.Catalog{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Catalog{}, err
	}
	var catalog domain.Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return domain.Catalog{}, err
	}
	return catalog, nil
}
