package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	root string
	mu   sync.Mutex
}

func Open(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("filesystem store: data directory is required")
	}
	for _, directory := range []string{
		"tokens", "tokens-used", "desired", "revisions", "catalogs", "status", "audit",
		"traffic/interface", "traffic/user",
	} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.root}, parts...)...)
}
