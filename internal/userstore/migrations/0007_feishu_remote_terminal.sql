-- 0007_feishu_remote_terminal.sql
-- Add remote-terminal settings to feishu_bindings.

ALTER TABLE feishu_bindings ADD COLUMN remote_terminal_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feishu_bindings ADD COLUMN session_auto_attach TEXT NOT NULL DEFAULT 'ai';
