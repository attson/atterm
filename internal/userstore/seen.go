package userstore

import (
	"context"
	"fmt"
)

func (s *SQLiteStore) SetSeen(ctx context.Context, userID string, sessionIDs []string, at int64) error {
	if userID == "" || len(sessionIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SetSeen: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO session_seen(user_id, session_id, seen_at)
		 VALUES(?,?,?)
		 ON CONFLICT(user_id, session_id) DO UPDATE SET seen_at=excluded.seen_at WHERE excluded.seen_at > session_seen.seen_at`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare SetSeen: %w", err)
	}
	defer stmt.Close()
	for _, sid := range sessionIDs {
		if sid == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, userID, sid, at); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec SetSeen: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) SeenAt(ctx context.Context, userID string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, seen_at FROM session_seen WHERE user_id=?`, userID)
	if err != nil {
		return nil, fmt.Errorf("query SeenAt: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var sid string
		var at int64
		if err := rows.Scan(&sid, &at); err != nil {
			return nil, fmt.Errorf("scan SeenAt: %w", err)
		}
		out[sid] = at
	}
	return out, rows.Err()
}

func (s *SQLiteStore) PruneSeenSession(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM session_seen WHERE session_id=?`, sessionID); err != nil {
		return fmt.Errorf("prune session_seen: %w", err)
	}
	return nil
}
