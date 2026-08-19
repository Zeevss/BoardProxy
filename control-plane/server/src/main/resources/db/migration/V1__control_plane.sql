-- Схема control-plane. Владеемое состояние отделено от производного и от наблюдаемого:
--   * владеемое   — nodes, boards, users, grants, quotas, subscriptions, credentials;
--   * производное — node_desired_config (пересобирается компилятором из владеемого);
--   * наблюдаемое — agent_status, node_runtime, runtime_events, трафик.
-- Пользователь флотовый: один человек — одна строка, один ключ, одна квота.
-- Размещение по нодам выражают гранты, а не копии пользователя.

-- ---------------------------------------------------------------------------
-- Агенты
-- ---------------------------------------------------------------------------

-- Внешний процесс, которым управляет хаб: нода или сервис подписок. Общее у них
-- ровно то, что лежит в agent_*: ревизия конфигурации, отчёт о состоянии и
-- одноразовые команды. Транспорт и способ публикации конфигурации — разные.
CREATE TABLE agents (
    id          varchar(128) PRIMARY KEY,
    kind        varchar(32) NOT NULL CHECK (kind IN ('node', 'subscription-service')),
    name        text NOT NULL CHECK (btrim(name) <> ''),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Наблюдаемое состояние агента. Онлайн не хранится флагом: он вычисляется
-- при чтении как now() - last_report_at < порога, поэтому фоновая job не нужна.
CREATE TABLE agent_status (
    agent_id         varchar(128) PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    boot_id          varchar(128),
    seq              bigint NOT NULL DEFAULT 0 CHECK (seq >= 0),
    applied_revision bigint NOT NULL DEFAULT 0 CHECK (applied_revision >= 0),
    applied_sha256   char(64),
    apply_error      text,
    agent_version    text,
    uptime_seconds   bigint CHECK (uptime_seconds >= 0),
    last_report_at   timestamptz,
    -- Поля, специфичные для вида агента: recoveryWatcherReady, startedAt и т.п.
    details          jsonb NOT NULL DEFAULT '{}'::jsonb
);

-- Команда доставляется ровно один раз: потерянная доставка лечится повторным
-- нажатием оператора, а не бесконечным циклом. Факт доставки ведёт хаб, потому
-- что агент не хранит состояние между запусками.
CREATE TABLE agent_commands (
    agent_id     varchar(128) NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    nonce        bigint NOT NULL CHECK (nonce > 0),
    kind         varchar(32) NOT NULL CHECK (kind IN ('restart')),
    issued_by    text NOT NULL CHECK (btrim(issued_by) <> ''),
    issued_at    timestamptz NOT NULL,
    delivered_at timestamptz,
    PRIMARY KEY (agent_id, nonce)
);

CREATE INDEX agent_commands_pending_idx
    ON agent_commands(agent_id, nonce)
    WHERE delivered_at IS NULL;

-- Ключ идемпотентности отчёта. Повтор с тем же batch_id не должен удваивать
-- трафик, поэтому приём начинается со вставки сюда: 0 строк = дубликат.
CREATE TABLE agent_reports (
    agent_id    varchar(128) NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    batch_id    varchar(128) NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, batch_id)
);

CREATE INDEX agent_reports_received_idx ON agent_reports(received_at);

-- ---------------------------------------------------------------------------
-- Владеемое состояние
-- ---------------------------------------------------------------------------

CREATE TABLE nodes (
    id                    varchar(128) PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    name                  text NOT NULL CHECK (btrim(name) <> ''),
    state                 varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    core_settings         jsonb NOT NULL,
    server_key_ciphertext bytea NOT NULL,
    server_key_nonce      bytea NOT NULL,
    server_key_key_id     text NOT NULL,
    resource_version      bigint NOT NULL CHECK (resource_version > 0),
    updated_at            timestamptz NOT NULL
);

-- Борд принадлежит ровно одной ноде: hub-slide не делится между серверами,
-- поэтому node_id входит в первичный ключ, а не выносится в таблицу связей.
CREATE TABLE boards (
    node_id          varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    id               varchar(128) NOT NULL,
    name             text NOT NULL CHECK (btrim(name) <> ''),
    board_hash       text NOT NULL CHECK (btrim(board_hash) <> ''),
    hub_slide        text,
    api_base         text,
    guest_name       text,
    state            varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    max_lanes        integer NOT NULL CHECK (max_lanes BETWEEN 1 AND 32),
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    updated_at       timestamptz NOT NULL,
    PRIMARY KEY (node_id, id),
    UNIQUE (node_id, board_hash)
);

-- Пользователь флотовый. Приватный ключ хранится один раз, а не по копии на
-- каждую ноду, поэтому отпечаток уникален глобально.
CREATE TABLE users (
    id                     varchar(128) PRIMARY KEY,
    name                   text NOT NULL CHECK (btrim(name) <> ''),
    private_key_ciphertext bytea,
    private_key_nonce      bytea,
    private_key_key_id     text,
    public_key             text,
    identity_fingerprint   text NOT NULL UNIQUE,
    state                  varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    max_sessions           integer NOT NULL CHECK (max_sessions >= 0),
    max_lanes              integer NOT NULL CHECK (max_lanes BETWEEN 1 AND 32),
    resource_version       bigint NOT NULL CHECK (resource_version > 0),
    updated_at             timestamptz NOT NULL,
    -- Либо мы владеем приватным ключом и умеем собрать keylink, либо знаем
    -- только публичный. Третьего состояния не бывает.
    CHECK (
        (private_key_ciphertext IS NOT NULL AND private_key_nonce IS NOT NULL
         AND private_key_key_id IS NOT NULL AND public_key IS NULL)
        OR
        (private_key_ciphertext IS NULL AND private_key_nonce IS NULL
         AND private_key_key_id IS NULL AND public_key IS NOT NULL)
    )
);

-- Размещение: на какой ноде и на каком её борде пользователь имеет доступ.
-- Заменяет node_users, node_user_boards и assignment_versions разом.
CREATE TABLE grants (
    user_id  varchar(128) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id  varchar(128) NOT NULL,
    board_id varchar(128) NOT NULL,
    PRIMARY KEY (user_id, node_id, board_id),
    FOREIGN KEY (node_id, board_id) REFERENCES boards(node_id, id) ON DELETE CASCADE
);

CREATE INDEX grants_node_idx ON grants(node_id);

-- ---------------------------------------------------------------------------
-- Производное состояние
-- ---------------------------------------------------------------------------

-- Только текущая конфигурация: историю ведут снимки исходного состояния.
-- Ревизия растёт лишь когда меняются байты TOML, а не на каждую правку.
CREATE TABLE node_desired_config (
    node_id           varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    revision          bigint NOT NULL CHECK (revision > 0),
    config_ciphertext bytea NOT NULL,
    config_nonce      bytea NOT NULL,
    config_key_id     text NOT NULL,
    config_sha256     char(64) NOT NULL,
    updated_at        timestamptz NOT NULL
);

-- Снимок исходного состояния ноды перед изменением: по нему считается diff и
-- делается rollback. Скомпилированный TOML в истории не хранится — rollback
-- всё равно применяет снимок как обычные записи и пересобирает конфигурацию.
CREATE TABLE node_config_snapshots (
    node_id            varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    seq                bigint NOT NULL CHECK (seq > 0),
    payload_ciphertext bytea NOT NULL,
    payload_nonce      bytea NOT NULL,
    payload_key_id     text NOT NULL,
    cause              text NOT NULL,
    actor              text NOT NULL CHECK (btrim(actor) <> ''),
    created_at         timestamptz NOT NULL,
    PRIMARY KEY (node_id, seq)
);

CREATE INDEX node_config_snapshots_time_idx
    ON node_config_snapshots(node_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Наблюдаемое состояние ноды
-- ---------------------------------------------------------------------------

-- Полный снимок, заменяется целиком. Проекции по событиям нет: нода знает своё
-- состояние лучше хаба, поэтому присылает его готовым, а не по кусочкам.
CREATE TABLE node_runtime (
    node_id     varchar(128) PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    snapshot    jsonb NOT NULL,
    observed_at timestamptz NOT NULL
);

-- Журнал активности для панели. Append-only, без последовательностей и реплея:
-- ничего не проецирует, поэтому разрыв в нём безвреден.
CREATE TABLE runtime_events (
    id          bigserial PRIMARY KEY,
    node_id     varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    event_type  varchar(64) NOT NULL,
    payload     jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX runtime_events_node_time_idx ON runtime_events(node_id, occurred_at DESC);

-- ---------------------------------------------------------------------------
-- Трафик
-- ---------------------------------------------------------------------------

-- Два несмешиваемых дерева: показания интерфейсов и полезная нагрузка
-- пользователей. Дельты привязаны к отчёту, чтобы повтор не удваивал суммы.
CREATE TABLE interface_traffic_deltas (
    agent_id       varchar(128) NOT NULL,
    batch_id       varchar(128) NOT NULL,
    interface_name text NOT NULL,
    rx_bytes       bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes       bigint NOT NULL CHECK (tx_bytes >= 0),
    rx_packets     bigint NOT NULL CHECK (rx_packets >= 0),
    tx_packets     bigint NOT NULL CHECK (tx_packets >= 0),
    rx_errors      bigint NOT NULL CHECK (rx_errors >= 0),
    tx_errors      bigint NOT NULL CHECK (tx_errors >= 0),
    rx_dropped     bigint NOT NULL CHECK (rx_dropped >= 0),
    tx_dropped     bigint NOT NULL CHECK (tx_dropped >= 0),
    observed_at    timestamptz NOT NULL,
    PRIMARY KEY (agent_id, batch_id, interface_name),
    FOREIGN KEY (agent_id, batch_id) REFERENCES agent_reports(agent_id, batch_id) ON DELETE CASCADE
);

-- Ссылки на users нет намеренно: отчёт о трафике не должен отвергаться целиком
-- из-за пользователя, удалённого между сбором и доставкой. Сироты вычищает
-- retention.
CREATE TABLE user_traffic_deltas (
    agent_id    varchar(128) NOT NULL,
    batch_id    varchar(128) NOT NULL,
    user_id     varchar(128) NOT NULL,
    rx_bytes    bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes    bigint NOT NULL CHECK (tx_bytes >= 0),
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (agent_id, batch_id, user_id),
    FOREIGN KEY (agent_id, batch_id) REFERENCES agent_reports(agent_id, batch_id) ON DELETE CASCADE
);

CREATE INDEX user_traffic_deltas_user_idx ON user_traffic_deltas(user_id, observed_at DESC);

CREATE TABLE traffic_hourly_rollups (
    node_id      varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    traffic_kind varchar(16) NOT NULL CHECK (traffic_kind IN ('interface', 'user')),
    subject      varchar(128) NOT NULL,
    bucket_start timestamptz NOT NULL,
    rx_bytes     bigint NOT NULL CHECK (rx_bytes >= 0),
    tx_bytes     bigint NOT NULL CHECK (tx_bytes >= 0),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, traffic_kind, subject, bucket_start)
);

CREATE INDEX traffic_hourly_rollups_time_idx ON traffic_hourly_rollups(bucket_start DESC);

-- Квота флотовая: лимит у пользователя один, а расход суммируется по всем нодам.
CREATE TABLE user_traffic_quotas (
    user_id          varchar(128) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    period           varchar(16) NOT NULL CHECK (period IN ('daily', 'weekly', 'monthly', 'none')),
    limit_bytes      bigint NOT NULL CHECK (limit_bytes > 0),
    action           varchar(16) NOT NULL CHECK (action IN ('alert', 'reset', 'disable')),
    enabled          boolean NOT NULL DEFAULT true,
    -- NULL — счётчик идёт с начала календарного периода; политика reset сдвигает.
    counter_start    timestamptz,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    updated_at       timestamptz NOT NULL
);

-- Состояние текущего периода. Флаг exceeded — вход компилятора конфигурации,
-- а не команда: телеметрия не пишет в desired state, поэтому сброс периода
-- возвращает пользователя в строй сам, без вмешательства оператора.
CREATE TABLE user_traffic_quota_state (
    user_id      varchar(128) PRIMARY KEY REFERENCES user_traffic_quotas(user_id) ON DELETE CASCADE,
    period_start timestamptz NOT NULL,
    used_bytes   bigint NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    exceeded     boolean NOT NULL DEFAULT false,
    changed_at   timestamptz NOT NULL
);

-- ---------------------------------------------------------------------------
-- Доступ
-- ---------------------------------------------------------------------------

-- Токен API и сессия панели — одно и то же: хешированный предъявитель с ролью
-- и сроком. Разводит их только kind.
CREATE TABLE credentials (
    id           varchar(128) PRIMARY KEY,
    kind         varchar(16) NOT NULL CHECK (kind IN ('api_token', 'panel_session')),
    subject      text NOT NULL CHECK (btrim(subject) <> ''),
    secret_hash  char(64) NOT NULL UNIQUE,
    role         varchar(16) NOT NULL CHECK (role IN ('subscriber', 'viewer', 'operator', 'admin')),
    created_by   text NOT NULL CHECK (btrim(created_by) <> ''),
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX credentials_active_hash_idx
    ON credentials(secret_hash)
    WHERE revoked_at IS NULL;

CREATE TABLE panel_administrators (
    singleton     boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    username      varchar(64) NOT NULL CHECK (btrim(username) <> ''),
    password_hash varchar(100) NOT NULL CHECK (btrim(password_hash) <> ''),
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);

CREATE UNIQUE INDEX panel_administrators_username_idx
    ON panel_administrators(lower(username));

-- ---------------------------------------------------------------------------
-- Подписки
-- ---------------------------------------------------------------------------

-- Подписка указывает на пользователя, а не перечисляет пары (нода, пользователь):
-- набор ключей выводится из грантов, поэтому разойтись с размещением не может.
-- Секреты хранятся восстановимо, чтобы панель показывала постоянную ссылку.
CREATE TABLE subscriptions (
    id                          varchar(128) PRIMARY KEY,
    name                        text NOT NULL CHECK (btrim(name) <> ''),
    user_id                     varchar(128) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash                  char(64) NOT NULL UNIQUE,
    token_ciphertext            bytea NOT NULL,
    token_nonce                 bytea NOT NULL,
    token_key_id                text NOT NULL,
    recovery_public_key         varchar(43) NOT NULL UNIQUE,
    recovery_private_ciphertext bytea NOT NULL,
    recovery_private_nonce      bytea NOT NULL,
    recovery_private_key_id     text NOT NULL,
    state                       varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    resource_version            bigint NOT NULL CHECK (resource_version > 0),
    created_at                  timestamptz NOT NULL,
    updated_at                  timestamptz NOT NULL
);

CREATE INDEX subscriptions_user_idx ON subscriptions(user_id);

-- Настройки внешнего сервиса подписок. Сам сервис знает только адрес хаба и
-- свой токен, остальное забирает отсюда. Состояние и перезапуск — в agent_*.
CREATE TABLE subscription_service_settings (
    id                          boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled                     boolean NOT NULL DEFAULT false,
    service_name                text NOT NULL DEFAULT 'BoardProxy',
    icon                        text NOT NULL DEFAULT '',
    public_url                  text NOT NULL DEFAULT '',
    yandex_editor_url           text NOT NULL DEFAULT '',
    recovery_key_id             text NOT NULL DEFAULT '',
    recovery_private_ciphertext bytea,
    recovery_private_nonce      bytea,
    recovery_private_key_id     text,
    recovery_public_key         text,
    apps                        jsonb NOT NULL DEFAULT '[]'::jsonb,
    revision                    bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    token_id                    varchar(128) REFERENCES credentials(id) ON DELETE SET NULL,
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    -- Recovery-пара либо есть целиком, либо её ещё не генерировали.
    CHECK (
        (recovery_private_ciphertext IS NULL AND recovery_private_nonce IS NULL
         AND recovery_private_key_id IS NULL AND recovery_public_key IS NULL)
        OR (recovery_private_ciphertext IS NOT NULL AND recovery_private_nonce IS NOT NULL
            AND recovery_private_key_id IS NOT NULL AND recovery_public_key IS NOT NULL)
    )
);

INSERT INTO subscription_service_settings (id) VALUES (true);

-- ---------------------------------------------------------------------------
-- PKI
-- ---------------------------------------------------------------------------

CREATE TABLE enrollment_tokens (
    token_hash  char(64) PRIMARY KEY,
    node_id     varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz
);

CREATE TABLE node_certificates (
    serial_number      text PRIMARY KEY,
    node_id            varchar(128) NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    certificate_pem    text NOT NULL,
    fingerprint_sha256 char(64),
    issued_at          timestamptz NOT NULL,
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    revoked_reason     text,
    last_seen_at       timestamptz
);

CREATE UNIQUE INDEX node_certificates_fingerprint_idx
    ON node_certificates(fingerprint_sha256)
    WHERE fingerprint_sha256 IS NOT NULL;

CREATE INDEX node_certificates_node_active_idx
    ON node_certificates(node_id, expires_at)
    WHERE revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Журналы
-- ---------------------------------------------------------------------------

CREATE TABLE audit_events (
    event_id         varchar(128) PRIMARY KEY,
    node_id          varchar(128) REFERENCES nodes(id) ON DELETE SET NULL,
    actor            text NOT NULL,
    action           text NOT NULL,
    resource_type    text NOT NULL,
    resource_id      text NOT NULL,
    resource_version bigint NOT NULL CHECK (resource_version >= 0),
    details          jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at      timestamptz NOT NULL
);

CREATE INDEX audit_events_time_idx ON audit_events(occurred_at DESC);
CREATE INDEX audit_events_node_time_idx ON audit_events(node_id, occurred_at DESC);

CREATE TABLE outbox_events (
    event_id         varchar(128) PRIMARY KEY,
    aggregate_type   text NOT NULL,
    aggregate_id     varchar(128) NOT NULL,
    event_type       text NOT NULL,
    payload          jsonb NOT NULL,
    occurred_at      timestamptz NOT NULL,
    published_at     timestamptz,
    attempts         integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  timestamptz,
    dead_lettered_at timestamptz,
    last_error       text
);

CREATE INDEX outbox_events_pending_idx
    ON outbox_events(COALESCE(next_attempt_at, occurred_at))
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;
