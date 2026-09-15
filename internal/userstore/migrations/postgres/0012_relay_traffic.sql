CREATE TABLE relay_traffic_daily (
  user_id    TEXT     NOT NULL,
  day        TEXT     NOT NULL,
  frame_type INTEGER  NOT NULL,
  direction  INTEGER  NOT NULL,
  bytes      BIGINT   NOT NULL DEFAULT 0,
  frames     BIGINT   NOT NULL DEFAULT 0,
  updated_at BIGINT   NOT NULL,
  PRIMARY KEY (user_id, day, frame_type, direction)
);
CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);
