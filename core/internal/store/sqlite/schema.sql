CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    public_key  BLOB NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('active','disabled')) DEFAULT 'active',
    created_at  TEXT NOT NULL,
    last_seen   TEXT,
    rx_bytes    INTEGER NOT NULL DEFAULT 0,
    tx_bytes    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS hubs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    hub_slide   TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('active','disabled')) DEFAULT 'active',
	max_lanes   INTEGER NOT NULL DEFAULT 8,
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS access_keys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    prefix      TEXT NOT NULL,
    digest      BLOB NOT NULL UNIQUE,
    created_at  TEXT NOT NULL,
    revoked_at  TEXT
);
