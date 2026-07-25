-- 0011_pairing_wrap.sql (postgres)
-- See sqlite/0011_pairing_wrap.sql for rationale.

ALTER TABLE pairing_tokens ADD COLUMN wrapped_account_key BYTEA;
