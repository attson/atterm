-- 0006_drop_webhooks.sql
-- Remove the outbound webhooks feature. Relay notifications now go through the
-- Feishu app integration (/v1/feishu/*, Admin → Feishu) and Web Push; the
-- per-user webhook table and its /api/me/webhooks routes are gone.
-- Runs after 0001 (created the table) and 0005 (pruned feishu-format rows),
-- so this DROP is safe on both fresh and existing databases.
DROP INDEX IF EXISTS idx_webhooks_user;
DROP TABLE IF EXISTS webhooks;
