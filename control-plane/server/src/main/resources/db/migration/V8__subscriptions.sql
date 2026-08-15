ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_role_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_role_check
    CHECK (role IN ('subscriber', 'viewer', 'operator', 'admin'));

CREATE TABLE subscriptions (
    id                  varchar(128) PRIMARY KEY,
    name                text NOT NULL CHECK (btrim(name) <> ''),
    token_hash          char(64) NOT NULL UNIQUE,
    recovery_public_key varchar(43) NOT NULL UNIQUE,
    state               varchar(16) NOT NULL CHECK (state IN ('enabled', 'disabled', 'revoked')),
    resource_version    bigint NOT NULL CHECK (resource_version > 0),
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);

CREATE TABLE subscription_keys (
    subscription_id varchar(128) NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    key_id          varchar(128) NOT NULL,
    display_name    text NOT NULL CHECK (btrim(display_name) <> ''),
    node_id         varchar(128) NOT NULL,
    user_id         varchar(128) NOT NULL,
    position        integer NOT NULL CHECK (position >= 0),
    PRIMARY KEY (subscription_id, key_id),
    UNIQUE (subscription_id, node_id, user_id),
    UNIQUE (subscription_id, position)
);

-- node_id/user_id are validated by the application. Catalog replacement recreates
-- user rows, so a foreign key here would either block a valid replacement or
-- accidentally delete subscription membership through ON DELETE CASCADE.

CREATE INDEX subscription_keys_node_user_idx ON subscription_keys(node_id, user_id);
