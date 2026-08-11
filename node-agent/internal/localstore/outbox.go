package localstore

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"time"

	nodev1 "bproxy-control-plane/api/node/v1"

	"google.golang.org/protobuf/proto"
)

var ErrBatchConflict = errors.New("localstore: outbox batch id already contains another event")

type Pending struct {
	BatchID string
	Event   *nodev1.NodeEvent
}

type encodedEvent struct {
	batchID string
	raw     []byte
}

// CommitCollection atomically advances collector checkpoints and appends all
// resulting traffic events. A crash cannot create a missing interval: either
// both changes commit or neither does.
func (s *Store) CommitCollection(checkpoints map[string][]byte, events map[string]*nodev1.NodeEvent) error {
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
	return nil
}

func encodeEvents(events map[string]*nodev1.NodeEvent) ([]encodedEvent, error) {
	encoded := make([]encodedEvent, 0, len(events))
	for batchID, event := range events {
		if batchID == "" {
			return nil, errors.New("localstore: outbox batch id is required")
		}
		if event == nil {
			return nil, fmt.Errorf("localstore: outbox event %q is required", batchID)
		}
		if actual := trafficBatchID(event); actual != batchID {
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

func trafficBatchID(event *nodev1.NodeEvent) string {
	if batch := event.GetInterfaceTraffic(); batch != nil {
		return batch.GetBatchId()
	}
	if batch := event.GetUserTraffic(); batch != nil {
		return batch.GetBatchId()
	}
	return ""
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
	rows, err := s.db.Query(`SELECT batch_id, event FROM outbox ORDER BY created_at, batch_id`)
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
		event := new(nodev1.NodeEvent)
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
