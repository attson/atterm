ALTER TABLE relay_config
    ADD COLUMN direct_signal_enabled BIGINT NOT NULL DEFAULT 0;
