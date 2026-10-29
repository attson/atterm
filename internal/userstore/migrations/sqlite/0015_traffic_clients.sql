ALTER TABLE relay_traffic_daily RENAME TO relay_traffic_daily_legacy;

CREATE TABLE relay_traffic_daily (
  user_id TEXT NOT NULL,
  day TEXT NOT NULL,
  client_id TEXT NOT NULL DEFAULT '',
  client_name TEXT NOT NULL DEFAULT '',
  client_kind TEXT NOT NULL DEFAULT '',
  frame_type INTEGER NOT NULL,
  direction INTEGER NOT NULL,
  bytes BIGINT NOT NULL DEFAULT 0,
  frames BIGINT NOT NULL DEFAULT 0,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, day, client_id, frame_type, direction)
);

INSERT INTO relay_traffic_daily
  (user_id, day, client_id, client_name, client_kind, frame_type, direction, bytes, frames, updated_at)
SELECT user_id, day, '', '', '', frame_type, direction, bytes, frames, updated_at
FROM relay_traffic_daily_legacy;

DROP TABLE relay_traffic_daily_legacy;
CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);

ALTER TABLE relay_traffic_buckets RENAME TO relay_traffic_buckets_legacy;

CREATE TABLE relay_traffic_buckets (
  user_id TEXT NOT NULL,
  bucket TEXT NOT NULL,
  started_at BIGINT NOT NULL,
  client_id TEXT NOT NULL DEFAULT '',
  client_name TEXT NOT NULL DEFAULT '',
  client_kind TEXT NOT NULL DEFAULT '',
  frame_type INTEGER NOT NULL,
  direction INTEGER NOT NULL,
  bytes BIGINT NOT NULL DEFAULT 0,
  frames BIGINT NOT NULL DEFAULT 0,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, bucket, started_at, client_id, frame_type, direction)
);

INSERT INTO relay_traffic_buckets
  (user_id, bucket, started_at, client_id, client_name, client_kind, frame_type, direction, bytes, frames, updated_at)
SELECT user_id, bucket, started_at, '', '', '', frame_type, direction, bytes, frames, updated_at
FROM relay_traffic_buckets_legacy;

DROP TABLE relay_traffic_buckets_legacy;
CREATE INDEX idx_traffic_buckets_time ON relay_traffic_buckets (bucket, started_at);
