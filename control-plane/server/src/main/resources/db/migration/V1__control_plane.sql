CREATE TABLE nodes (
    id                  varchar(128) PRIMARY KEY,
    name                text NOT NULL CHECK (btrim(name) <> ''),
    state               varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    core_settings       jsonb NOT NULL,
    server_key_ciphertext bytea NOT NULL,
    server_key_nonce      bytea NOT NULL,
    server_key_key_id     text NOT NULL,
    resource_version    bigint NOT NULL CHECK (resource_version > 0),
    catalog_version     bigint NOT NULL CHECK (catalog_version > 0),
    updated_at          timestamptz NOT NULL,
    catalog_updated_at  timestamptz NOT NULL
);

CREATE TABLE boards (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    id                  varchar(128) NOT NULL,
    name                text NOT NULL CHECK (btrim(name) <> ''),
    board_hash          text NOT NULL CHECK (btrim(board_hash) <> ''),
    hub_slide           text,
    api_base            text,
    guest_name          text,
    state               varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    max_lanes           integer NOT NULL CHECK (max_lanes BETWEEN 1 AND 32),
    resource_version    bigint NOT NULL CHECK (resource_version > 0),
    updated_at          timestamptz NOT NULL,
    PRIMARY KEY (node_id, id),
    UNIQUE (node_id, board_hash)
);

CREATE TABLE users (
    node_id                 varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    id                      varchar(128) NOT NULL,
    name                    text NOT NULL CHECK (btrim(name) <> ''),
    private_key_ciphertext  bytea,
    private_key_nonce       bytea,
    private_key_key_id      text,
    public_key              text,
    identity_fingerprint    text NOT NULL,
    state                   varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    max_sessions            integer NOT NULL CHECK (max_sessions >= 0),
    max_lanes               integer NOT NULL CHECK (max_lanes BETWEEN 1 AND 32),
    resource_version        bigint NOT NULL CHECK (resource_version > 0),
    updated_at              timestamptz NOT NULL,
    PRIMARY KEY (node_id, id),
    UNIQUE (node_id, identity_fingerprint),
    CHECK (
        (private_key_ciphertext IS NOT NULL AND private_key_nonce IS NOT NULL AND private_key_key_id IS NOT NULL AND public_key IS NULL)
        OR
        (private_key_ciphertext IS NULL AND private_key_nonce IS NULL AND private_key_key_id IS NULL AND public_key IS NOT NULL)
    )
);

CREATE TABLE node_boards (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    board_id            varchar(128) NOT NULL,
    PRIMARY KEY (node_id, board_id),
    FOREIGN KEY (node_id, board_id) REFERENCES boards(node_id, id) ON DELETE CASCADE
);

CREATE TABLE node_users (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    user_id             varchar(128) NOT NULL,
    PRIMARY KEY (node_id, user_id),
    FOREIGN KEY (node_id, user_id) REFERENCES users(node_id, id) ON DELETE CASCADE
);

CREATE TABLE node_user_boards (
    node_id             varchar(128) NOT NULL,
    user_id             varchar(128) NOT NULL,
    board_id            varchar(128) NOT NULL,
    PRIMARY KEY (node_id, user_id, board_id),
    FOREIGN KEY (node_id, user_id) REFERENCES node_users(node_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (node_id, board_id) REFERENCES node_boards(node_id, board_id) ON DELETE CASCADE
);

CREATE TABLE assignment_versions (
    node_id             varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    resource_version    bigint NOT NULL CHECK (resource_version > 0),
    updated_at          timestamptz NOT NULL
);

CREATE TABLE desired_config_revisions (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    revision            bigint NOT NULL CHECK (revision > 0),
    previous_revision   bigint NOT NULL CHECK (previous_revision >= 0),
    catalog_version     bigint NOT NULL CHECK (catalog_version > 0),
    config_ciphertext   bytea NOT NULL,
    config_nonce        bytea NOT NULL,
    config_key_id       text NOT NULL,
    config_sha256       char(64) NOT NULL,
    cause               text NOT NULL,
    created_at          timestamptz NOT NULL,
    PRIMARY KEY (node_id, revision),
    UNIQUE (node_id, config_sha256)
);

CREATE TABLE node_status (
    node_id             varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    connected           boolean NOT NULL DEFAULT false,
    boot_id             varchar(128),
    agent_version       text,
    core_version        text,
    core_running        boolean NOT NULL DEFAULT false,
    core_ready          boolean NOT NULL DEFAULT false,
    desired_revision    bigint NOT NULL DEFAULT 0,
    applied_revision    bigint NOT NULL DEFAULT 0,
    config_sha256       char(64),
    last_error          text,
    last_seen           timestamptz,
    last_apply          jsonb,
    projection_version  bigint NOT NULL DEFAULT 1
);

CREATE TABLE runtime_events (
    event_id            varchar(128) PRIMARY KEY,
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    core_boot_id        varchar(128) NOT NULL,
    sequence_number     bigint NOT NULL CHECK (sequence_number >= 0),
    runtime_revision    bigint NOT NULL CHECK (runtime_revision >= 0),
    event_type          varchar(64) NOT NULL,
    payload             jsonb NOT NULL,
    occurred_at         timestamptz NOT NULL,
    received_at         timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX runtime_event_sequence_unique
    ON runtime_events(node_id, core_boot_id, sequence_number)
    WHERE sequence_number > 0;
CREATE INDEX runtime_events_node_time_idx ON runtime_events(node_id, occurred_at DESC);

CREATE TABLE runtime_event_batches (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    batch_id            varchar(128) NOT NULL,
    payload             bytea NOT NULL,
    received_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, batch_id)
);

CREATE TABLE node_runtime_projection (
    node_id             varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    core_boot_id        varchar(128),
    last_sequence       bigint NOT NULL DEFAULT 0,
    gap_detected        boolean NOT NULL DEFAULT false,
    last_event_at       timestamptz,
    users               jsonb NOT NULL DEFAULT '{}'::jsonb,
    boards              jsonb NOT NULL DEFAULT '{}'::jsonb,
    projection_version  bigint NOT NULL DEFAULT 1
);

CREATE TABLE traffic_batches (
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    batch_id            varchar(128) NOT NULL,
    traffic_kind        varchar(16) NOT NULL CHECK (traffic_kind IN ('interface', 'user')),
    interval_start      timestamptz NOT NULL,
    interval_end        timestamptz NOT NULL,
    payload             bytea NOT NULL,
    received_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, batch_id)
);

CREATE TABLE interface_traffic_deltas (
    node_id             varchar(128) NOT NULL,
    batch_id            varchar(128) NOT NULL,
    interface_name      text NOT NULL,
    rx_bytes            bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes            bigint NOT NULL CHECK (tx_bytes >= 0),
    rx_packets          bigint NOT NULL CHECK (rx_packets >= 0),
    tx_packets          bigint NOT NULL CHECK (tx_packets >= 0),
    rx_errors           bigint NOT NULL CHECK (rx_errors >= 0),
    tx_errors           bigint NOT NULL CHECK (tx_errors >= 0),
    rx_dropped          bigint NOT NULL CHECK (rx_dropped >= 0),
    tx_dropped          bigint NOT NULL CHECK (tx_dropped >= 0),
    PRIMARY KEY (node_id, batch_id, interface_name),
    FOREIGN KEY (node_id, batch_id) REFERENCES traffic_batches(node_id, batch_id) ON DELETE CASCADE
);

CREATE TABLE user_traffic_deltas (
    node_id             varchar(128) NOT NULL,
    batch_id            varchar(128) NOT NULL,
    user_tag            varchar(128) NOT NULL,
    rx_bytes            bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes            bigint NOT NULL CHECK (tx_bytes >= 0),
    PRIMARY KEY (node_id, batch_id, user_tag),
    FOREIGN KEY (node_id, batch_id) REFERENCES traffic_batches(node_id, batch_id) ON DELETE CASCADE
);

CREATE TABLE enrollment_tokens (
    token_hash          char(64) PRIMARY KEY,
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    expires_at          timestamptz NOT NULL,
    consumed_at         timestamptz
);

CREATE TABLE node_certificates (
    serial_number       text PRIMARY KEY,
    node_id             varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    certificate_pem     text NOT NULL,
    issued_at           timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL,
    revoked_at          timestamptz
);

CREATE TABLE audit_events (
    event_id            varchar(128) PRIMARY KEY,
    node_id             varchar(128) REFERENCES nodes(id) ON DELETE SET NULL,
    actor               text NOT NULL,
    action              text NOT NULL,
    resource_type       text NOT NULL,
    resource_id         text NOT NULL,
    resource_version    bigint NOT NULL CHECK (resource_version >= 0),
    catalog_version     bigint NOT NULL CHECK (catalog_version >= 0),
    details             jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at         timestamptz NOT NULL
);
CREATE INDEX audit_events_node_time_idx ON audit_events(node_id, occurred_at DESC);

CREATE TABLE outbox_events (
    event_id            varchar(128) PRIMARY KEY,
    aggregate_type      text NOT NULL,
    aggregate_id        varchar(128) NOT NULL,
    event_type          text NOT NULL,
    payload             jsonb NOT NULL,
    occurred_at         timestamptz NOT NULL,
    published_at        timestamptz,
    attempts            integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error          text
);
CREATE INDEX outbox_events_pending_idx ON outbox_events(occurred_at) WHERE published_at IS NULL;
