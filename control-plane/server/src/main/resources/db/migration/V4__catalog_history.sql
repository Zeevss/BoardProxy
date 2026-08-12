ALTER TABLE desired_config_revisions
    DROP CONSTRAINT desired_config_revisions_node_id_config_sha256_key;

CREATE INDEX desired_config_revisions_node_hash_idx
    ON desired_config_revisions(node_id, config_sha256);

CREATE TABLE catalog_snapshots (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    catalog_version     bigint NOT NULL CHECK (catalog_version > 0),
    payload_ciphertext  bytea NOT NULL,
    payload_nonce       bytea NOT NULL,
    payload_key_id      text NOT NULL,
    created_at          timestamptz NOT NULL,
    PRIMARY KEY (node_id, catalog_version)
);

CREATE INDEX catalog_snapshots_node_time_idx
    ON catalog_snapshots(node_id, created_at DESC);
