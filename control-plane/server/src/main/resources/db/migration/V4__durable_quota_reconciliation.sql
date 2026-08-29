ALTER TABLE user_traffic_quota_state
    ADD COLUMN threshold_exceeded boolean NOT NULL DEFAULT false;

-- Поколение защищает от потери нового изменения, пока reconciler применяет
-- предыдущее. У записи намеренно нет FK: удаление quota/user тоже должно
-- гарантированно дойти до конфигурации и снять блокировку.
CREATE TABLE quota_config_changes (
    user_id     varchar(128) PRIMARY KEY,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    changed_at timestamptz NOT NULL
);

CREATE INDEX quota_config_changes_time_idx
    ON quota_config_changes(changed_at, user_id);
