ALTER TABLE relay_config
    ADD COLUMN direct_signal_enabled INTEGER NOT NULL DEFAULT 0;
