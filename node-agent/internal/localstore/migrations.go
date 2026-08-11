package localstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type migration struct {
	version    int
	statements []string
}

var migrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE checkpoints (
				key TEXT PRIMARY KEY NOT NULL CHECK (length(key) > 0),
				value BLOB NOT NULL,
				updated_at INTEGER NOT NULL
			) STRICT`,
			`CREATE TABLE outbox (
				batch_id TEXT PRIMARY KEY NOT NULL CHECK (length(batch_id) > 0),
				event BLOB NOT NULL,
				created_at INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX outbox_delivery_order ON outbox (created_at, batch_id)`,
		},
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, len(migrations))
	}
	for _, migration := range migrations {
		if migration.version <= current {
			continue
		}
		if migration.version != current+1 {
			return fmt.Errorf("non-contiguous migration: current=%d next=%d", current, migration.version)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, tx, migration); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d commit: %w", migration.version, err)
		}
		current = migration.version
	}
	return nil
}

func applyMigration(ctx context.Context, tx *sql.Tx, migration migration) error {
	for _, statement := range migration.statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		migration.version, time.Now().UTC().UnixMilli(),
	)
	return err
}
