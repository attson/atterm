-- 0004_claim_tokens.sql — single-use claim tokens for bootstrap admin
-- and (future) operator-initiated account creation. Operator generates
-- a token via `atterm-relay bootstrap-admin`; user completes OPAQUE
-- registration using the token; token is consumed.

CREATE TABLE claim_tokens (
    token_hash  TEXT PRIMARY KEY,           -- sha256 hex of plaintext token
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,              -- "admin" for bootstrap
    expires_at  INTEGER NOT NULL,           -- unix seconds
    consumed_at INTEGER                     -- unix seconds when consumed; NULL if pending
);

CREATE INDEX claim_tokens_email ON claim_tokens(email);
