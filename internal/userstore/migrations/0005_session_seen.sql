-- 0005_session_seen.sql: per-user seen timestamps for the session unread inbox.
CREATE TABLE session_seen (
    user_id    TEXT    NOT NULL,
    session_id TEXT    NOT NULL,
    seen_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, session_id)
);
