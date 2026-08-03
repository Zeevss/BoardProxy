// Package sqlite реализует store.Store поверх SQLite (modernc.org/sqlite —
// чистый Go, без cgo). Единственный писатель — сам сервер (управление идёт
// через него по unix-сокету), поэтому файловой БД достаточно: пул сведён к
// одному соединению, чего хватает для последовательной записи.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bproxy-core/internal/store"

	_ "embed"

	sqlitedrv "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// sqliteConstraintUnique — расширенный код ошибки SQLITE_CONSTRAINT_UNIQUE
// (нарушение UNIQUE-индекса). Часть протокола SQLite, стабилен между версиями;
// именно он отличает конфликт по public_key от прочих ошибок записи.
const sqliteConstraintUnique = 2067

// Store — реализация store.Store поверх файла SQLite.
type Store struct {
	db *sql.DB
}

// Validate verifies an uploaded database before it can replace the live store.
// It checks both SQLite integrity and the minimal BoardProxy schema without
// running migrations or modifying the candidate file.
func Validate(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("sqlite: validate open: %w", err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return fmt.Errorf("sqlite: integrity check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("sqlite: integrity check failed: %s", integrity)
	}
	for _, table := range []string{"users", "hubs"} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			return fmt.Errorf("sqlite: validate schema: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("sqlite: required table %q is missing", table)
		}
	}
	return nil
}

// Open открывает (создавая при отсутствии) файл БД по пути path и применяет
// схему. Недостающие родительские каталоги создаются. Пустого файла достаточно:
// схема идемпотентна (CREATE TABLE IF NOT EXISTS), так что новый файл сразу
// готов к работе.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sqlite: create dir: %w", err)
		}
	}
	// WAL + busy_timeout делают запись устойчивой к кратким гонкам, хотя
	// писатель у нас один. foreign_keys не нужны — внешних ключей в схеме нет.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	// Один писатель — один коннект: SQLite сериализует запись на файл, а
	// нескольких соединений в пуле хватило бы лишь для "database is locked".
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	if err := migrateUserTrafficColumns(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrateUserTrafficColumns добавляет rx_bytes/tx_bytes в уже существующие БД,
// созданные до появления учёта трафика: CREATE TABLE IF NOT EXISTS схему не
// трогает, если таблица уже есть, поэтому старые файлы нуждаются в явном
// ALTER TABLE. Для новых БД (только что созданных schema.sql) колонки уже на
// месте — PRAGMA table_info их найдёт, и миграция станет no-op.
func migrateUserTrafficColumns(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(users)`)
	if err != nil {
		return fmt.Errorf("table_info: %w", err)
	}
	have := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("table_info: scan: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info: %w", err)
	}
	for _, col := range []string{"rx_bytes", "tx_bytes"} {
		if have[col] {
			continue
		}
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf(`ALTER TABLE users ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, col)); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Backup делает консистентный снимок БД в файл dstPath средствами самого SQLite
// (VACUUM INTO): корректно работает на живой WAL-БД без остановки сервера и
// отдаёт цельный файл без сопутствующих -wal/-shm. dstPath не должен
// существовать — VACUUM INTO отказывается перезаписывать.
func (s *Store) Backup(ctx context.Context, dstPath string) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dstPath); err != nil {
		return fmt.Errorf("sqlite: backup: %w", err)
	}
	return nil
}

func (s *Store) CreateUser(ctx context.Context, pubKey []byte, name string) (store.User, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (public_key, name, status, created_at) VALUES (?, ?, ?, ?)`,
		pubKey, name, string(store.UserActive), formatTime(now))
	if err != nil {
		if isUniqueViolation(err) {
			return store.User{}, store.ErrConflict
		}
		return store.User{}, fmt.Errorf("sqlite: create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return store.User{}, fmt.Errorf("sqlite: create user: last id: %w", err)
	}
	return store.User{
		ID:        id,
		PublicKey: pubKey,
		Name:      name,
		Status:    store.UserActive,
		CreatedAt: now,
	}, nil
}

const userColumns = `id, public_key, name, status, created_at, last_seen, rx_bytes, tx_bytes`

func (s *Store) UserByPublicKey(ctx context.Context, pubKey []byte) (store.User, error) {
	return s.userByRow(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE public_key = ?`,
		pubKey), "user by public key")
}

func (s *Store) UserByID(ctx context.Context, id int64) (store.User, error) {
	return s.userByRow(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`,
		id), "user by id")
}

func (s *Store) userByRow(row *sql.Row, op string) (store.User, error) {
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.User{}, store.ErrNotFound
		}
		return store.User{}, fmt.Errorf("sqlite: %s: %w", op, err)
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: list users: scan: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	return out, nil
}

func (s *Store) SetUserStatus(ctx context.Context, id int64, status store.UserStatus) error {
	return s.updateStatus(ctx, "set user status",
		`UPDATE users SET status = ? WHERE id = ?`, string(status), id)
}

func (s *Store) SetUserName(ctx context.Context, id int64, name string) error {
	return s.updateStatus(ctx, "set user name",
		`UPDATE users SET name = ? WHERE id = ?`, name, id)
}

// AddUserTraffic добавляет rx/tx к накопленным счётчикам пользователя
// (не заменяет — конкурентные закрытия разных сессий одного пользователя не
// должны затирать друг друга).
func (s *Store) AddUserTraffic(ctx context.Context, id int64, rx, tx uint64) error {
	return s.updateStatus(ctx, "add user traffic",
		`UPDATE users SET rx_bytes = rx_bytes + ?, tx_bytes = tx_bytes + ? WHERE id = ?`,
		int64(rx), int64(tx), id)
}

// TouchUser проставляет last_seen текущим моментом (UTC).
func (s *Store) TouchUser(ctx context.Context, id int64) error {
	return s.updateStatus(ctx, "touch user",
		`UPDATE users SET last_seen = ? WHERE id = ?`, formatTime(time.Now().UTC()), id)
}

// DeleteUser безвозвратно удаляет пользователя.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return s.updateStatus(ctx, "delete user", `DELETE FROM users WHERE id = ?`, id)
}

func (s *Store) UpsertHub(ctx context.Context, id, name, hubSlide string) (store.Hub, error) {
	// created_at выставляется только при вставке; при обновлении сохраняется
	// прежнее значение (excluded не трогает created_at).
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO hubs (id, name, hub_slide, status, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET name = excluded.name, hub_slide = excluded.hub_slide`,
		id, name, hubSlide, string(store.HubActive), formatTime(now))
	if err != nil {
		return store.Hub{}, fmt.Errorf("sqlite: upsert hub: %w", err)
	}
	h, err := s.hubByID(ctx, id)
	if err != nil {
		return store.Hub{}, fmt.Errorf("sqlite: upsert hub: reload: %w", err)
	}
	return h, nil
}

func (s *Store) hubByID(ctx context.Context, id string) (store.Hub, error) {
	return scanHub(s.db.QueryRowContext(ctx,
		`SELECT id, name, hub_slide, status, created_at FROM hubs WHERE id = ?`, id))
}

func (s *Store) ListHubs(ctx context.Context) ([]store.Hub, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, hub_slide, status, created_at FROM hubs ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list hubs: %w", err)
	}
	defer rows.Close()
	var out []store.Hub
	for rows.Next() {
		h, err := scanHub(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: list hubs: scan: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list hubs: %w", err)
	}
	return out, nil
}

func (s *Store) SetHubStatus(ctx context.Context, id string, status store.HubStatus) error {
	return s.updateStatus(ctx, "set hub status",
		`UPDATE hubs SET status = ? WHERE id = ?`, string(status), id)
}

func (s *Store) SetHubName(ctx context.Context, id string, name string) error {
	return s.updateStatus(ctx, "set hub name",
		`UPDATE hubs SET name = ? WHERE id = ?`, name, id)
}

// DeleteHub безвозвратно удаляет запись хаба.
func (s *Store) DeleteHub(ctx context.Context, id string) error {
	return s.updateStatus(ctx, "delete hub", `DELETE FROM hubs WHERE id = ?`, id)
}

// updateStatus выполняет UPDATE ... WHERE id = ? и возвращает ErrNotFound,
// если строка не затронута (нет такой записи).
func (s *Store) updateStatus(ctx context.Context, op, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sqlite: %s: %w", op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: %s: rows affected: %w", op, err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// scanner — общий контракт *sql.Row и *sql.Rows (у обоих есть Scan).
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(sc scanner) (store.User, error) {
	var (
		u         store.User
		status    string
		lastSeen  sql.NullString
		createdAt string
		rx, tx    int64
	)
	if err := sc.Scan(&u.ID, &u.PublicKey, &u.Name, &status, &createdAt, &lastSeen, &rx, &tx); err != nil {
		return store.User{}, err
	}
	u.Status = store.UserStatus(status)
	u.CreatedAt = parseTime(createdAt)
	u.LastSeen = parseNullTime(lastSeen)
	u.RxBytes = uint64(rx)
	u.TxBytes = uint64(tx)
	return u, nil
}

func scanHub(sc scanner) (store.Hub, error) {
	var (
		h         store.Hub
		status    string
		createdAt string
	)
	if err := sc.Scan(&h.ID, &h.Name, &h.HubSlide, &status, &createdAt); err != nil {
		return store.Hub{}, err
	}
	h.Status = store.HubStatus(status)
	h.CreatedAt = parseTime(createdAt)
	return h, nil
}

// formatTime сериализует момент в RFC3339 с наносекундами; нулевое время
// сериализуется в пустую строку (для NULLABLE-колонок вызывающий передаёт nil).
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func parseNullTime(ns sql.NullString) time.Time {
	if !ns.Valid {
		return time.Time{}
	}
	return parseTime(ns.String)
}

func isUniqueViolation(err error) bool {
	var sqliteErr *sqlitedrv.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique
}
