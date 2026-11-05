CREATE TABLE direct_traffic_buckets (
  user_id TEXT NOT NULL,
  started_at BIGINT NOT NULL,
  attempts BIGINT NOT NULL DEFAULT 0,
  successes BIGINT NOT NULL DEFAULT 0,
  fallbacks BIGINT NOT NULL DEFAULT 0,
  bytes_sent BIGINT NOT NULL DEFAULT 0,
  bytes_received BIGINT NOT NULL DEFAULT 0,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, started_at)
);

CREATE INDEX idx_direct_traffic_buckets_time ON direct_traffic_buckets (started_at);

CREATE TABLE traffic_bucket_coverage (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  started_at BIGINT NOT NULL
);

INSERT INTO traffic_bucket_coverage (id, started_at)
VALUES (1, CAST(strftime('%s', 'now') AS INTEGER) / 900 * 900);
