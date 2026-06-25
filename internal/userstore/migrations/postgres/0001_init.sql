-- 0001_init.sql (postgres) — final auth schema. Booleans stored as BIGINT
-- (0/1) to match the shared Go scan code; timestamps are unix epoch BIGINT.
-- email is lowercased in Go, so a plain UNIQUE replaces SQLite COLLATE NOCASE.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    is_admin      BIGINT NOT NULL DEFAULT 0,
    auth_mode     TEXT NOT NULL DEFAULT 'opaque',
    created_at    BIGINT NOT NULL,
    disabled_at   BIGINT
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL DEFAULT 0,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip_prefix    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE pairing_tokens (
    token_hash   TEXT PRIMARY KEY,
    prefix       TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    consumed_at  BIGINT
);
CREATE INDEX pairing_tokens_user_idx ON pairing_tokens(user_id);

CREATE TABLE invitations (
    code_hash      TEXT PRIMARY KEY,
    created_by     TEXT NOT NULL,
    created_at     BIGINT NOT NULL,
    expires_at     BIGINT,
    consumed_at    BIGINT,
    consumed_by    TEXT REFERENCES users(id),
    note           TEXT
);

CREATE TABLE session_seen (
    user_id    TEXT   NOT NULL,
    session_id TEXT   NOT NULL,
    seen_at    BIGINT NOT NULL,
    PRIMARY KEY (user_id, session_id)
);
