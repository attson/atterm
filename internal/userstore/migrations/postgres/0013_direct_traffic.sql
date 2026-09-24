CREATE TABLE direct_traffic_daily (
  user_id        TEXT    NOT NULL,
  day            TEXT    NOT NULL,
  attempts       BIGINT  NOT NULL DEFAULT 0,
  successes      BIGINT  NOT NULL DEFAULT 0,
  fallbacks      BIGINT  NOT NULL DEFAULT 0,
  bytes_sent     BIGINT  NOT NULL DEFAULT 0,
  bytes_received BIGINT  NOT NULL DEFAULT 0,
  updated_at     BIGINT  NOT NULL,
  PRIMARY KEY (user_id, day)
);
CREATE INDEX idx_direct_traffic_day ON direct_traffic_daily (day);
