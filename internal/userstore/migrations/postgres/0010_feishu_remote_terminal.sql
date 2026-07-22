-- 0010_feishu_remote_terminal.sql (postgres)
-- Add remote-terminal settings to Feishu bindings.

ALTER TABLE feishu_bindings ADD COLUMN remote_terminal_enabled BIGINT NOT NULL DEFAULT 0;
ALTER TABLE feishu_bindings ADD COLUMN session_auto_attach TEXT NOT NULL DEFAULT 'ai';
