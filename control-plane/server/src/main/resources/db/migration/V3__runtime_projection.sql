ALTER TABLE runtime_events DROP CONSTRAINT runtime_events_pkey;
ALTER TABLE runtime_events ADD PRIMARY KEY (node_id, event_id);

ALTER TABLE node_runtime_projection
    ADD COLUMN runtime_revision bigint NOT NULL DEFAULT 0 CHECK (runtime_revision >= 0),
    ADD COLUMN captured_at timestamptz,
    ADD COLUMN sessions jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN session_details_complete boolean NOT NULL DEFAULT true;

CREATE INDEX runtime_events_node_boot_sequence_idx
    ON runtime_events(node_id, core_boot_id, sequence_number);
