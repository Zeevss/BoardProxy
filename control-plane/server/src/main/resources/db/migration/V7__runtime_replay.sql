ALTER TABLE runtime_event_batches
    ADD COLUMN snapshot jsonb;

CREATE INDEX runtime_event_batches_snapshot_idx
    ON runtime_event_batches(node_id, received_at DESC)
    WHERE snapshot IS NOT NULL;
