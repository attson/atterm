-- 0005_feishu.sql (postgres) — Feishu bindings + pending pair codes.
-- No webhooks table exists in the postgres schema, so the SQLite
-- "DELETE FROM webhooks" cleanup is omitted.
CREATE TABLE feishu_bindings (
    user_id          TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id_hash      TEXT   NOT NULL UNIQUE,
    app_id_enc       BYTEA  NOT NULL,
    app_secret_enc   BYTEA  NOT NULL,
    encrypt_key_enc  BYTEA  NOT NULL,
    verify_token_enc BYTEA  NOT NULL,
    open_id          TEXT,
    bound_at         BIGINT,
    disabled_at      BIGINT,
    created_at       BIGINT NOT NULL
);

CREATE TABLE feishu_pending_binds (
    user_id    TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT   NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL
);
CREATE INDEX feishu_pending_binds_expires ON feishu_pending_binds(expires_at);
