-- 0003_opaque_auth.sql — replace bcrypt password column with OPAQUE auth.
-- Per feedback_no_backward_compat, this migration drops password_hash
-- entirely; existing accounts must re-register.

ALTER TABLE users DROP COLUMN password_hash;
ALTER TABLE users ADD COLUMN auth_mode TEXT NOT NULL DEFAULT 'opaque';

CREATE TABLE user_opaque_records (
    user_id    TEXT PRIMARY KEY,
    record     BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE user_account_key_wraps (
    user_id    TEXT NOT NULL,
    method     TEXT NOT NULL,
    wrapped    BLOB NOT NULL,
    nonce      BLOB NOT NULL,
    salt       BLOB NOT NULL,
    kdf_params TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, method),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- OPAQUE OPRF seed for this relay instance; generated on first boot.
CREATE TABLE opaque_server_state (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    oprf_seed     BLOB NOT NULL,
    server_ake_sk BLOB NOT NULL,
    server_ake_pk BLOB NOT NULL,
    suite         TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);
