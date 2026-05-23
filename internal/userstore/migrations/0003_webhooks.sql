CREATE TABLE webhooks (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    url            TEXT NOT NULL,
    format         TEXT NOT NULL,
    allow_insecure INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX idx_webhooks_user ON webhooks(user_id);
