ALTER TABLE node_certificates
    ADD COLUMN fingerprint_sha256 char(64),
    ADD COLUMN revoked_reason text,
    ADD COLUMN last_seen_at timestamptz;

CREATE UNIQUE INDEX node_certificates_fingerprint_idx
    ON node_certificates(fingerprint_sha256)
    WHERE fingerprint_sha256 IS NOT NULL;
CREATE INDEX node_certificates_node_active_idx
    ON node_certificates(node_id, expires_at)
    WHERE revoked_at IS NULL;

ALTER TABLE api_tokens
    ADD COLUMN last_used_at timestamptz;

ALTER TABLE node_status
    ADD COLUMN fencing_token bigint NOT NULL DEFAULT 0 CHECK (fencing_token >= 0);

CREATE TABLE node_session_leases (
    node_id         varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    owner_id        varchar(128) NOT NULL,
    session_id      varchar(128) NOT NULL,
    fencing_token   bigint NOT NULL CHECK (fencing_token > 0),
    acquired_at     timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL
);
CREATE INDEX node_session_leases_expiry_idx ON node_session_leases(expires_at);

ALTER TABLE outbox_events
    ADD COLUMN next_attempt_at timestamptz,
    ADD COLUMN dead_lettered_at timestamptz;

DROP INDEX outbox_events_pending_idx;
CREATE INDEX outbox_events_pending_idx
    ON outbox_events(COALESCE(next_attempt_at, occurred_at))
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;
