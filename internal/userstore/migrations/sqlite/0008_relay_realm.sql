-- 0008_relay_realm.sql — stable cluster realm identity (singleton).
CREATE TABLE relay_realm_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    realm_id   TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
