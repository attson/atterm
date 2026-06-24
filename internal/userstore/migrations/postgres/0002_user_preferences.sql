-- 0002_user_preferences.sql (postgres)
CREATE TABLE user_preferences (
    user_id     TEXT   NOT NULL,
    key         TEXT   NOT NULL,
    value_json  TEXT   NOT NULL,
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (user_id, key)
);
CREATE INDEX user_preferences_user ON user_preferences(user_id);
