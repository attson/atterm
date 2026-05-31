-- 0004_pairing_tokens.sql: short-lived QR pairing tokens + api_tokens.source

CREATE TABLE pairing_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    consumed_at  INTEGER
);
CREATE INDEX idx_pairing_tokens_user ON pairing_tokens(user_id);

ALTER TABLE api_tokens ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
