-- 0007_relay_config.sql — move relay.json + web-push.json into the DB.
CREATE TABLE relay_config (
    id                      INTEGER PRIMARY KEY CHECK (id = 1),
    rate_limit_per_minute   INTEGER NOT NULL DEFAULT 0,
    max_connections_per_key INTEGER NOT NULL DEFAULT 0,
    allowed_origins         TEXT    NOT NULL DEFAULT '[]',  -- JSON array
    vapid_subject           TEXT    NOT NULL DEFAULT '',
    debug                   INTEGER NOT NULL DEFAULT 0,
    debug_payload           INTEGER NOT NULL DEFAULT 0,
    feishu_enabled          INTEGER NOT NULL DEFAULT 0,
    feishu_encrypt_key      TEXT    NOT NULL DEFAULT '',
    feishu_base_url         TEXT    NOT NULL DEFAULT '',
    version                 INTEGER NOT NULL DEFAULT 1,
    updated_at              INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE web_push_keys (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    private_key TEXT    NOT NULL,
    public_key  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE web_push_subscriptions (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT    NOT NULL,
    p256dh     TEXT    NOT NULL,
    auth       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, endpoint)
);
CREATE INDEX web_push_subscriptions_user ON web_push_subscriptions(user_id);
