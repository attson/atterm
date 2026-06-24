-- 0004_claim_tokens.sql (postgres)
CREATE TABLE claim_tokens (
    token_hash  TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    expires_at  BIGINT NOT NULL,
    consumed_at BIGINT
);
CREATE INDEX claim_tokens_email ON claim_tokens(email);
