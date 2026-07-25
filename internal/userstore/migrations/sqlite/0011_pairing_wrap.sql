-- 0011_pairing_wrap.sql
-- Store the AEAD-sealed account_key uploaded by the desktop during
-- /api/pair/create. Returned verbatim to the mobile during
-- /api/pair/consume so the phone can decrypt sealed session fields.
-- Nullable: sessions created before this migration, or by a desktop
-- whose account_key was locked at generation time, have no wrap.

ALTER TABLE pairing_tokens ADD COLUMN wrapped_account_key BLOB;
