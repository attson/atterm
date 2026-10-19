package userstore

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// TrafficDelta is one increment to fold into the daily rollup.
type TrafficDelta struct {
	// Minute is the UTC capture time; zero denotes a legacy daily-only increment.
	Minute    int64
	UserID    string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

// TrafficRow is a persisted rollup. Day is a UTC date for daily queries,
// or an RFC3339 bucket start for minute/hour queries.
type TrafficRow struct {
	UpdatedAt int64
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
// day (or the capture date when Minute is set), atomically with its minute
// and hour totals. Each (user_id, day, frame_type, direction) row is upserted with
// bytes/frames accumulated, so multiple relay instances writing the same
// row converge to a global total.
func (s *DBStore) AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	// Shared Postgres writers must acquire daily and fine-grained row locks
	// in a stable order, including flushes spanning a UTC day boundary.
	deltas = append([]TrafficDelta(nil), deltas...)
	sort.Slice(deltas, func(i, j int) bool {
		a, b := deltas[i], deltas[j]
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		if a.FrameType != b.FrameType {
			return a.FrameType < b.FrameType
		}
		if a.Direction != b.Direction {
			return a.Direction < b.Direction
		}
		return a.Minute < b.Minute
	})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic tx: %w", err)
	}
	defer tx.Rollback()
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
			d.UserID, trafficDeltaDay(day, d), d.FrameType, d.Direction, d.Bytes, d.Frames, now); err != nil {
			return fmt.Errorf("upsert traffic: %w", err)
		}
		if d.Minute != 0 {
			for _, bucket := range []struct {
				name    string
				seconds int64
			}{{"minute", 60}, {"hour", 3600}} {
				_, err := tx.ExecContext(ctx, s.dia.Rebind(`INSERT INTO relay_traffic_buckets
				(user_id, bucket, started_at, frame_type, direction, bytes, frames, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, bucket, started_at, frame_type, direction)
				DO UPDATE SET bytes = relay_traffic_buckets.bytes + excluded.bytes,
				frames = relay_traffic_buckets.frames + excluded.frames, updated_at = excluded.updated_at`),
					d.UserID, bucket.name, d.Minute/bucket.seconds*bucket.seconds, d.FrameType, d.Direction, d.Bytes, d.Frames, now)
				if err != nil {
					return fmt.Errorf("upsert traffic bucket: %w", err)
				}
			}
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

func trafficDeltaDay(fallback string, d TrafficDelta) string {
	if d.Minute == 0 {
		return fallback
	}
	return time.Unix(d.Minute, 0).UTC().Format("2006-01-02")
}

// QueryTrafficBuckets filters personal queries in SQL and reads shared rollups
// so every relay instance exposes the same durable traffic history.
func (s *DBStore) QueryTrafficBuckets(ctx context.Context, userID, bucket string, from, to int64) ([]TrafficRow, error) {
	query := `SELECT user_id, started_at, frame_type, direction, bytes, frames, updated_at
	FROM relay_traffic_buckets WHERE bucket = ? AND started_at >= ? AND started_at < ?`
	args := []any{bucket, from, to}
	if userID != "" {
		query += " AND user_id = ?"
		args = append(args, userID)
	}
	query += " ORDER BY started_at, user_id, frame_type, direction"
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("query traffic buckets: %w", err)
	}
	defer rows.Close()
	out := make([]TrafficRow, 0)
	for rows.Next() {
		var row TrafficRow
		var started int64
		if err := rows.Scan(&row.UserID, &started, &row.FrameType, &row.Direction, &row.Bytes, &row.Frames, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan traffic bucket: %w", err)
		}
		row.Day = time.Unix(started, 0).UTC().Format(time.RFC3339)
		out = append(out, row)
	}
	return out, rows.Err()
}

// PruneTrafficBuckets bounds fine-grained storage independently of daily totals.
func (s *DBStore) PruneTrafficBuckets(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(`DELETE FROM relay_traffic_buckets
	WHERE (bucket = 'minute' AND started_at < ?) OR (bucket = 'hour' AND started_at < ?)`),
		now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -7).Unix(), now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -90).Unix())
	return err
}
