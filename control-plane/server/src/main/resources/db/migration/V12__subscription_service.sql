-- Control-plane становится владельцем настроек сервиса подписок: subscribe
-- знает только адрес хаба и свой токен, остальное забирает сам.
-- Таблица-синглтон: настройка ровно одна на инсталляцию.
CREATE TABLE subscription_service_settings (
    id                          boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled                     boolean NOT NULL DEFAULT false,
    service_name                text NOT NULL DEFAULT 'BoardProxy',
    icon                        text NOT NULL DEFAULT '',
    public_url                  text NOT NULL DEFAULT '',
    yandex_editor_url           text NOT NULL DEFAULT '',
    recovery_key_id             text NOT NULL DEFAULT '',
    -- Приватный recovery-ключ генерирует control-plane и хранит зашифрованным,
    -- как остальные секреты; публичный лежит рядом открытым.
    recovery_private_ciphertext bytea,
    recovery_private_nonce      bytea,
    recovery_private_key_id     text,
    recovery_public_key         text,
    apps                        jsonb NOT NULL DEFAULT '[]'::jsonb,
    revision                    bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    -- Растёт на каждое нажатие «перезапустить»; сервис сравнивает со своим.
    restart_nonce               bigint NOT NULL DEFAULT 0 CHECK (restart_nonce >= 0),
    token_id                    varchar(128) REFERENCES api_tokens(id) ON DELETE SET NULL,
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (recovery_private_ciphertext IS NULL AND recovery_private_nonce IS NULL
         AND recovery_private_key_id IS NULL AND recovery_public_key IS NULL)
        OR (recovery_private_ciphertext IS NOT NULL AND recovery_private_nonce IS NOT NULL
            AND recovery_private_key_id IS NOT NULL AND recovery_public_key IS NOT NULL)
    )
);

INSERT INTO subscription_service_settings (id) VALUES (true);

-- Наблюдаемое состояние сервиса: проекция, не desired state.
CREATE TABLE subscription_service_status (
    id                     boolean PRIMARY KEY DEFAULT true CHECK (id),
    last_seen_at           timestamptz,
    service_version        text,
    applied_revision       bigint,
    recovery_watcher_ready boolean,
    started_at             timestamptz,
    acked_restart_nonce    bigint NOT NULL DEFAULT 0
);

INSERT INTO subscription_service_status (id) VALUES (true);
