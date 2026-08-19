package localstore

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/protobuf/proto"
)

var (
	ErrBatchConflict = errors.New("localstore: outbox batch id already contains another event")
	ErrOutboxFull    = errors.New("localstore: telemetry outbox byte limit reached")
)

type Pending struct {
	BatchID string
	Event   *nodev1.ReportRequest
}

type OutboxEvent struct {
	BatchID string
	Event   *nodev1.ReportRequest
}

type encodedEvent struct {
	batchID string
	raw     []byte
}

// CommitCollection atomically advances collector checkpoints and appends the
// resulting reports. A crash cannot create a missing interval: either both
// changes commit or neither does.
func (s *Store) CommitCollection(checkpoints map[string][]byte, events map[string]*nodev1.ReportRequest) error {
	ordered := make([]OutboxEvent, 0, len(events))
	for batchID, event := range events {
		ordered = append(ordered, OutboxEvent{BatchID: batchID, Event: event})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].BatchID < ordered[j].BatchID })
	return s.CommitOrderedCollection(checkpoints, ordered)
}

// CommitOrderedCollection preserves the supplied order in SQLite rowid, so a
// runtime snapshot never overtakes the events collected before it.
func (s *Store) CommitOrderedCollection(checkpoints map[string][]byte, events []OutboxEvent) error {
	encoded, err := encodeEvents(events)
	if err != nil {
		return err
	}
	for key, value := range checkpoints {
		if key == "" {
			return errors.New("localstore: checkpoint key is required")
		}
		if value == nil {
			return fmt.Errorf("localstore: checkpoint %q value is required", key)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("localstore: begin collection transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.ensureOutboxCapacity(tx, encoded); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	for key, value := range checkpoints {
		if _, err := tx.Exec(`
			INSERT INTO checkpoints (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, key, value, now); err != nil {
			return fmt.Errorf("localstore: write checkpoint %q: %w", key, err)
		}
	}
	for _, event := range encoded {
		if err := insertOutboxEvent(tx, event, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("localstore: commit collection: %w", err)
	}
	s.notifyChange()
	return nil
}

func (s *Store) ensureOutboxCapacity(tx *sql.Tx, events []encodedEvent) error {
	var current int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(length(event)), 0) FROM outbox`).Scan(&current); err != nil {
		return fmt.Errorf("localstore: measure outbox: %w", err)
	}
	additional := int64(0)
	for _, event := range events {
		var existing []byte
		err := tx.QueryRow(`SELECT event FROM outbox WHERE batch_id = ?`, event.batchID).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			additional += int64(len(event.raw))
		case err != nil:
			return fmt.Errorf("localstore: inspect outbox capacity for %q: %w", event.batchID, err)
		case !bytes.Equal(existing, event.raw):
			return fmt.Errorf("%w: %s", ErrBatchConflict, event.batchID)
		}
	}
	if current+additional > s.maxOutboxBytes {
		return fmt.Errorf("%w: current=%d additional=%d limit=%d", ErrOutboxFull, current, additional, s.maxOutboxBytes)
	}
	return nil
}

func encodeEvents(events []OutboxEvent) ([]encodedEvent, error) {
	encoded := make([]encodedEvent, 0, len(events))
	for _, item := range events {
		batchID, event := item.BatchID, item.Event
		if batchID == "" {
			return nil, errors.New("localstore: outbox batch id is required")
		}
		if event == nil {
			return nil, fmt.Errorf("localstore: outbox event %q is required", batchID)
		}
		if actual := outboxEventID(event); actual != batchID {
			return nil, fmt.Errorf("localstore: outbox key %q does not match event batch id %q", batchID, actual)
		}
		raw, err := proto.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("localstore: encode outbox event %q: %w", batchID, err)
		}
		encoded = append(encoded, encodedEvent{batchID: batchID, raw: raw})
	}
	return encoded, nil
}

func outboxEventID(event *nodev1.ReportRequest) string {
	return event.GetBatchId()
}

func insertOutboxEvent(tx *sql.Tx, event encodedEvent, createdAt int64) error {
	result, err := tx.Exec(`INSERT INTO outbox (batch_id, event, created_at) VALUES (?, ?, ?)
		ON CONFLICT(batch_id) DO NOTHING`, event.batchID, event.raw, createdAt)
	if err != nil {
		return fmt.Errorf("localstore: append outbox event %q: %w", event.batchID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("localstore: inspect outbox insert %q: %w", event.batchID, err)
	}
	if rows == 1 {
		return nil
	}
	var existing []byte
	if err := tx.QueryRow(`SELECT event FROM outbox WHERE batch_id = ?`, event.batchID).Scan(&existing); err != nil {
		return fmt.Errorf("localstore: inspect existing outbox event %q: %w", event.batchID, err)
	}
	if !bytes.Equal(existing, event.raw) {
		return fmt.Errorf("%w: %s", ErrBatchConflict, event.batchID)
	}
	return nil
}

func (s *Store) Pending() ([]Pending, error) {
	// rowid is the durable insertion order. Millisecond timestamps plus random
	// batch IDs can reorder adjacent core events (notably reset -> replay).
	rows, err := s.db.Query(`SELECT batch_id, event FROM outbox ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("localstore: list outbox: %w", err)
	}
	defer rows.Close()
	var pending []Pending
	for rows.Next() {
		var batchID string
		var raw []byte
		if err := rows.Scan(&batchID, &raw); err != nil {
			return nil, fmt.Errorf("localstore: scan outbox: %w", err)
		}
		event := new(nodev1.ReportRequest)
		if err := proto.Unmarshal(raw, event); err != nil {
			return nil, fmt.Errorf("localstore: decode outbox event %q: %w", batchID, err)
		}
		pending = append(pending, Pending{BatchID: batchID, Event: event})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("localstore: iterate outbox: %w", err)
	}
	return pending, nil
}

func (s *Store) Ack(batchID string) error {
	if batchID == "" {
		return errors.New("localstore: batch id is required")
	}
	if _, err := s.db.Exec(`DELETE FROM outbox WHERE batch_id = ?`, batchID); err != nil {
		return fmt.Errorf("localstore: acknowledge batch %q: %w", batchID, err)
	}
	return nil
}
