ALTER TABLE relay_traffic_daily
  ADD COLUMN client_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN client_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN client_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE relay_traffic_daily DROP CONSTRAINT relay_traffic_daily_pkey;
ALTER TABLE relay_traffic_daily
  ADD PRIMARY KEY (user_id, day, client_id, frame_type, direction);

ALTER TABLE relay_traffic_buckets
  ADD COLUMN client_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN client_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN client_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE relay_traffic_buckets DROP CONSTRAINT relay_traffic_buckets_pkey;
ALTER TABLE relay_traffic_buckets
  ADD PRIMARY KEY (user_id, bucket, started_at, client_id, frame_type, direction);
