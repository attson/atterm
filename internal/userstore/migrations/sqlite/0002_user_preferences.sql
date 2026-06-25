-- 0002_user_preferences.sql — user preferences table for cross-platform settings sync.

CREATE TABLE user_preferences (
    user_id     TEXT    NOT NULL,
    key         TEXT    NOT NULL,
    value_json  TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, key)
);

CREATE INDEX user_preferences_user ON user_preferences(user_id);
