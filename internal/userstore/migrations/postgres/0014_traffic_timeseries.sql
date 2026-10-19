CREATE TABLE relay_traffic_buckets (
  user_id TEXT NOT NULL,
  bucket TEXT NOT NULL,
  started_at BIGINT NOT NULL,
  frame_type INTEGER NOT NULL,
  direction INTEGER NOT NULL,
  bytes BIGINT NOT NULL DEFAULT 0,
  frames BIGINT NOT NULL DEFAULT 0,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, bucket, started_at, frame_type, direction)
);
CREATE INDEX idx_traffic_buckets_time ON relay_traffic_buckets (bucket, started_at);
