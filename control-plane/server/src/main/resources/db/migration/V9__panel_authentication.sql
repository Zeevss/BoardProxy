CREATE TABLE panel_administrators (
    singleton       boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    username        varchar(64) NOT NULL CHECK (btrim(username) <> ''),
    password_hash   varchar(100) NOT NULL CHECK (btrim(password_hash) <> ''),
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE UNIQUE INDEX panel_administrators_username_idx
    ON panel_administrators(lower(username));

CREATE TABLE panel_sessions (
    id              varchar(128) PRIMARY KEY,
    administrator   boolean NOT NULL REFERENCES panel_administrators(singleton) ON DELETE CASCADE,
    token_hash      char(64) NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    last_used_at    timestamptz,
    revoked_at      timestamptz
);

CREATE INDEX panel_sessions_active_hash_idx
    ON panel_sessions(token_hash, expires_at)
    WHERE revoked_at IS NULL;
