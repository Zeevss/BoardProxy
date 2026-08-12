CREATE TABLE traffic_hourly_rollups (
    node_id         varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    traffic_kind    varchar(16) NOT NULL CHECK (traffic_kind IN ('interface', 'user')),
    subject         varchar(128) NOT NULL,
    bucket_start    timestamptz NOT NULL,
    rx_bytes        bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes        bigint NOT NULL CHECK (tx_bytes >= 0),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, traffic_kind, subject, bucket_start)
);

CREATE INDEX traffic_hourly_rollups_time_idx
    ON traffic_hourly_rollups(bucket_start DESC);
CREATE INDEX traffic_batches_interval_end_idx
    ON traffic_batches(interval_end);

CREATE TABLE user_traffic_quotas (
    node_id         varchar(128) NOT NULL,
    user_tag        varchar(128) NOT NULL,
    period          varchar(16) NOT NULL CHECK (period IN ('daily', 'monthly')),
    limit_bytes     bigint NOT NULL CHECK (limit_bytes > 0),
    action          varchar(16) NOT NULL CHECK (action IN ('alert', 'disable')),
    enabled         boolean NOT NULL DEFAULT true,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    updated_at      timestamptz NOT NULL,
    PRIMARY KEY (node_id, user_tag),
    FOREIGN KEY (node_id, user_tag) REFERENCES users(node_id, id) ON DELETE CASCADE
);

CREATE TABLE user_traffic_quota_state (
    node_id         varchar(128) NOT NULL,
    user_tag        varchar(128) NOT NULL,
    period_start    timestamptz NOT NULL,
    exceeded_at     timestamptz,
    enforced_at     timestamptz,
    PRIMARY KEY (node_id, user_tag, period_start),
    FOREIGN KEY (node_id, user_tag) REFERENCES user_traffic_quotas(node_id, user_tag) ON DELETE CASCADE
);
