-- 0007_relay_realm.sql (postgres) — stable cluster realm identity (singleton).
CREATE TABLE relay_realm_state (
    id         BIGINT PRIMARY KEY CHECK (id = 1),
    realm_id   TEXT   NOT NULL,
    created_at BIGINT NOT NULL
);
