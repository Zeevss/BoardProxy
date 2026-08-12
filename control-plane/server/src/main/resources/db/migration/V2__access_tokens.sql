CREATE TABLE api_tokens (
    id              varchar(128) PRIMARY KEY,
    name            text NOT NULL CHECK (btrim(name) <> ''),
    token_hash      char(64) NOT NULL UNIQUE,
    role            varchar(16) NOT NULL CHECK (role IN ('viewer', 'operator', 'admin')),
    created_by      text NOT NULL CHECK (btrim(created_by) <> ''),
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz,
    revoked_at      timestamptz
);

CREATE INDEX api_tokens_active_hash_idx
    ON api_tokens(token_hash)
    WHERE revoked_at IS NULL;
