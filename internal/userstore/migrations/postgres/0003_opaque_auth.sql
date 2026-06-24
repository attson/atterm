-- 0003_opaque_auth.sql (postgres) — OPAQUE record + account-key wrap +
-- per-relay OPRF singleton. BLOB -> BYTEA.
CREATE TABLE user_opaque_records (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    record     BYTEA  NOT NULL,
    created_at BIGINT NOT NULL
);

CREATE TABLE user_account_key_wraps (
    user_id    TEXT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method     TEXT  NOT NULL,
    wrapped    BYTEA NOT NULL,
    nonce      BYTEA NOT NULL,
    salt       BYTEA NOT NULL,
    kdf_params TEXT  NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, method)
);

CREATE TABLE opaque_server_state (
    id            BIGINT PRIMARY KEY CHECK (id = 1),
    oprf_seed     BYTEA  NOT NULL,
    server_ake_sk BYTEA  NOT NULL,
    server_ake_pk BYTEA  NOT NULL,
    suite         TEXT   NOT NULL,
    created_at    BIGINT NOT NULL
);
