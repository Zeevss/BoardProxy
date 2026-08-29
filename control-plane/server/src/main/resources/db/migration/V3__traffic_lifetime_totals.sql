-- Почасовая история имеет конечный retention, но quota period=none и общий
-- расход подписки должны оставаться lifetime-счётчиками. Перед удалением
-- старые user-rollup сворачиваются сюда без временной детализации.
CREATE TABLE user_traffic_lifetime_totals (
    node_id     varchar(128) NOT NULL,
    user_id     varchar(128) NOT NULL,
    rx_bytes    bigint NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
    tx_bytes    bigint NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
    archived_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, user_id)
);

CREATE INDEX user_traffic_lifetime_totals_user_idx
    ON user_traffic_lifetime_totals(user_id);
