-- 0005_feishu.sql
-- Add Feishu app-mode bindings + pending pair codes.
-- Replace the legacy Format=feishu custom-bot URL path (no backward-compat).

CREATE TABLE feishu_bindings (
    user_id          TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id_hash      TEXT    NOT NULL UNIQUE,
    app_id_enc       BLOB    NOT NULL,
    app_secret_enc   BLOB    NOT NULL,
    encrypt_key_enc  BLOB    NOT NULL,
    verify_token_enc BLOB    NOT NULL,
    open_id          TEXT,
    bound_at         INTEGER,
    disabled_at      INTEGER,
    created_at       INTEGER NOT NULL
);

CREATE TABLE feishu_pending_binds (
    user_id    TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT    NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL
);
CREATE INDEX feishu_pending_binds_expires ON feishu_pending_binds(expires_at);

DELETE FROM webhooks WHERE format = 'feishu';
