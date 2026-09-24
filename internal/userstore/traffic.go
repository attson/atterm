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

// DirectTrafficDelta is one user's aggregate P2P usage increment.
type DirectTrafficDelta struct {
	UserID        string
	Attempts      int64
	Successes     int64
	Fallbacks     int64
	BytesSent     int64
	BytesReceived int64
}

// DirectTrafficRow is one persisted per-user, per-day P2P rollup row.
type DirectTrafficRow struct {
	UserID        string
	Day           string
	Attempts      int64
	Successes     int64
	Fallbacks     int64
	BytesSent     int64
	BytesReceived int64
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

// QueryTrafficForUser returns only userID's relay rollups in [from, to].
// Keeping the filter in SQL prevents personal-account handlers from ever
// loading another account's usage into memory.
func (s *DBStore) QueryTrafficForUser(ctx context.Context, userID, from, to string) ([]TrafficRow, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT user_id, day, frame_type, direction, bytes, frames
		 FROM relay_traffic_daily
		 WHERE user_id = ? AND day >= ? AND day <= ?
		 ORDER BY day, frame_type, direction`), userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query user traffic: %w", err)
	}
	defer rows.Close()
	var out []TrafficRow
	for rows.Next() {
		var r TrafficRow
		if err := rows.Scan(&r.UserID, &r.Day, &r.FrameType, &r.Direction, &r.Bytes, &r.Frames); err != nil {
			return nil, fmt.Errorf("scan user traffic: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddDirectTrafficDeltas folds aggregate P2P deltas into the UTC daily
// rollup. The additive upsert converges when several relay instances share
// the same Postgres database.
func (s *DBStore) AddDirectTrafficDeltas(ctx context.Context, day string, deltas []DirectTrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin direct traffic tx: %w", err)
	}
	now := time.Now().Unix()
	stmt := s.dia.Rebind(`INSERT INTO direct_traffic_daily
		(user_id, day, attempts, successes, fallbacks, bytes_sent, bytes_received, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, day)
		DO UPDATE SET attempts = direct_traffic_daily.attempts + excluded.attempts,
		              successes = direct_traffic_daily.successes + excluded.successes,
		              fallbacks = direct_traffic_daily.fallbacks + excluded.fallbacks,
		              bytes_sent = direct_traffic_daily.bytes_sent + excluded.bytes_sent,
		              bytes_received = direct_traffic_daily.bytes_received + excluded.bytes_received,
		              updated_at = excluded.updated_at`)
	for _, d := range deltas {
		if _, err := tx.ExecContext(ctx, stmt, d.UserID, day, d.Attempts, d.Successes,
			d.Fallbacks, d.BytesSent, d.BytesReceived, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("upsert direct traffic: %w", err)
		}
	}
	return tx.Commit()
}

// QueryDirectTraffic returns userID's P2P rollups in [from, to].
func (s *DBStore) QueryDirectTraffic(ctx context.Context, userID, from, to string) ([]DirectTrafficRow, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT user_id, day, attempts, successes, fallbacks, bytes_sent, bytes_received
		 FROM direct_traffic_daily
		 WHERE user_id = ? AND day >= ? AND day <= ?
		 ORDER BY day`), userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query direct traffic: %w", err)
	}
	defer rows.Close()
	var out []DirectTrafficRow
	for rows.Next() {
		var r DirectTrafficRow
		if err := rows.Scan(&r.UserID, &r.Day, &r.Attempts, &r.Successes, &r.Fallbacks,
			&r.BytesSent, &r.BytesReceived); err != nil {
			return nil, fmt.Errorf("scan direct traffic: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
