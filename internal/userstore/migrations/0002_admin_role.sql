-- Add is_admin column to users. SQLite has no BOOLEAN; INTEGER 0/1.
-- Default 0 keeps existing rows non-admin; PR A's bootstrap path is
-- the only way to flip this to 1 from outside the admin API.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
