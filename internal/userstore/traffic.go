package userstore

import (
	"context"
	"fmt"
	"time"
)

// TrafficDelta is one increment to fold into the daily rollup.
type TrafficDelta struct {
	UserID    string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

// TrafficRow is one persisted daily rollup row.
type TrafficRow struct {
	UserID    string
	Day       string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

// AddTrafficDeltas folds deltas into relay_traffic_daily for the given UTC
// day. Each (user_id, day, frame_type, direction) row is upserted with
// bytes/frames accumulated, so multiple relay instances writing the same
// row converge to a global total.
func (s *DBStore) AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic tx: %w", err)
	}
	now := time.Now().Unix()
	stmt := s.dia.Rebind(`INSERT INTO relay_traffic_daily
		(user_id, day, frame_type, direction, bytes, frames, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, day, frame_type, direction)
		DO UPDATE SET bytes = relay_traffic_daily.bytes + excluded.bytes,
		              frames = relay_traffic_daily.frames + excluded.frames,
		              updated_at = excluded.updated_at`)
	for _, d := range deltas {
		if _, err := tx.ExecContext(ctx, stmt,
			d.UserID, day, d.FrameType, d.Direction, d.Bytes, d.Frames, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("upsert traffic: %w", err)
		}
	}
	return tx.Commit()
}

// QueryTraffic returns all rollup rows with day in [from, to] (inclusive,
// lexicographic on the YYYY-MM-DD strings). Aggregation into detail/group/
// summary views is the caller's job.
func (s *DBStore) QueryTraffic(ctx context.Context, from, to string) ([]TrafficRow, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT user_id, day, frame_type, direction, bytes, frames
		 FROM relay_traffic_daily
		 WHERE day >= ? AND day <= ?
		 ORDER BY day, user_id, frame_type, direction`), from, to)
	if err != nil {
		return nil, fmt.Errorf("query traffic: %w", err)
	}
	defer rows.Close()
	var out []TrafficRow
	for rows.Next() {
		var r TrafficRow
		if err := rows.Scan(&r.UserID, &r.Day, &r.FrameType, &r.Direction, &r.Bytes, &r.Frames); err != nil {
			return nil, fmt.Errorf("scan traffic: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
