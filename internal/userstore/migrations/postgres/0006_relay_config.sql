-- 0006_relay_config.sql (postgres) — booleans/timestamps as BIGINT.
CREATE TABLE relay_config (
    id                      BIGINT PRIMARY KEY CHECK (id = 1),
    rate_limit_per_minute   BIGINT NOT NULL DEFAULT 0,
    max_connections_per_key BIGINT NOT NULL DEFAULT 0,
    allowed_origins         TEXT   NOT NULL DEFAULT '[]',
    vapid_subject           TEXT   NOT NULL DEFAULT '',
    debug                   BIGINT NOT NULL DEFAULT 0,
    debug_payload           BIGINT NOT NULL DEFAULT 0,
    feishu_enabled          BIGINT NOT NULL DEFAULT 0,
    feishu_encrypt_key      TEXT   NOT NULL DEFAULT '',
    feishu_base_url         TEXT   NOT NULL DEFAULT '',
    version                 BIGINT NOT NULL DEFAULT 1,
    updated_at              BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE web_push_keys (
    id          BIGINT PRIMARY KEY CHECK (id = 1),
    private_key TEXT   NOT NULL,
    public_key  TEXT   NOT NULL,
    created_at  BIGINT NOT NULL
);

CREATE TABLE web_push_subscriptions (
    user_id    TEXT   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT   NOT NULL,
    p256dh     TEXT   NOT NULL,
    auth       TEXT   NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, endpoint)
);
CREATE INDEX web_push_subscriptions_user ON web_push_subscriptions(user_id);
