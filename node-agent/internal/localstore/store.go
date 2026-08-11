// Package localstore persists operational node state. It is deliberately kept
// behind the agent's small storage interfaces: desired business state belongs
// to the control plane, while this database contains only checkpoints and an
// at-least-once telemetry outbox.
package localstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	databaseName       = "node.sqlite3"
	legacyDatabaseName = "node.db"
)

var ErrLegacyDatabase = errors.New("localstore: legacy bbolt database requires explicit migration")

type Store struct{ db *sql.DB }

func Open(dataDirectory string) (*Store, error) {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("localstore: create data directory: %w", err)
	}
	if err := os.Chmod(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("localstore: protect data directory: %w", err)
	}

	path := filepath.Join(dataDirectory, databaseName)
	if err := rejectUnmigratedLegacyDatabase(dataDirectory, path); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("localstore: open sqlite: %w", err)
	}
	// Pragmas are connection-local. A node has one lightweight writer, so using
	// one long-lived connection keeps the durability policy deterministic.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("localstore: protect database: %w", err)
	}
	return store, nil
}

func rejectUnmigratedLegacyDatabase(dataDirectory, sqlitePath string) error {
	if _, err := os.Stat(sqlitePath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localstore: inspect sqlite database: %w", err)
	}
	legacyPath := filepath.Join(dataDirectory, legacyDatabaseName)
	if _, err := os.Stat(legacyPath); err == nil {
		return fmt.Errorf("%w: move %s out of the data directory after preserving any required telemetry",
			ErrLegacyDatabase, legacyPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localstore: inspect legacy database: %w", err)
	}
	return nil
}

func (s *Store) initialize(ctx context.Context) error {
	var journalMode string
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		return fmt.Errorf("localstore: enable WAL: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("localstore: sqlite selected journal mode %q, want wal", journalMode)
	}
	for _, statement := range []string{
		`PRAGMA synchronous = FULL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("localstore: configure sqlite: %w", err)
		}
	}
	if err := applyMigrations(ctx, s.db); err != nil {
		return fmt.Errorf("localstore: migrate: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Checkpoint(key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("localstore: checkpoint key is required")
	}
	var value []byte
	err := s.db.QueryRow(`SELECT value FROM checkpoints WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("localstore: read checkpoint %q: %w", key, err)
	}
	return value, nil
}

func (s *Store) PutCheckpoint(key string, value []byte) error {
	if key == "" {
		return errors.New("localstore: checkpoint key is required")
	}
	if value == nil {
		return errors.New("localstore: checkpoint value is required")
	}
	_, err := s.db.Exec(`
		INSERT INTO checkpoints (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("localstore: write checkpoint %q: %w", key, err)
	}
	return nil
}
