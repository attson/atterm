-- 0008_node_selection.sql (postgres)
CREATE TABLE relay_instances (
    instance_id    TEXT   PRIMARY KEY,
    public_url     TEXT   NOT NULL,
    last_heartbeat BIGINT NOT NULL
);

CREATE TABLE user_home (
    user_id     TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    instance_id TEXT   NOT NULL,
    updated_at  BIGINT NOT NULL
);
