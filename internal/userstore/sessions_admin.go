package userstore

import (
	"context"
	"fmt"
	"time"
)

// ListSessions returns all non-expired sessions for userID, ordered by
// created_at DESC.
func (s *DBStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	nowMs := time.Now().UnixMilli()
	rows, err := s.db.QueryContext(ctx,
		s.dia.Rebind(`SELECT id_hash, user_id, COALESCE(user_agent, ''), COALESCE(ip_prefix, ''), created_at, expires_at, last_seen_at
		 FROM sessions
		 WHERE user_id = ? AND expires_at >= ?
		 ORDER BY created_at DESC`),
		userID, nowMs,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var (
			idHash     string
			uid        string
			ua         string
			ipPrefix   string
			createdMs  int64
			expiresMs  int64
			lastSeenMs int64
		)
		if err := rows.Scan(&idHash, &uid, &ua, &ipPrefix, &createdMs, &expiresMs, &lastSeenMs); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, Session{
			IDHash:     idHash,
			UserID:     uid,
			UserAgent:  ua,
			IPPrefix:   ipPrefix,
			CreatedAt:  time.UnixMilli(createdMs),
			ExpiresAt:  time.UnixMilli(expiresMs),
			LastSeenAt: time.UnixMilli(lastSeenMs),
		})
	}
	return out, rows.Err()
}

// DeleteSessionByIDHash revokes the session ONLY IF owned by userID.
// The (user_id, id_hash) WHERE clause is the security boundary.
func (s *DBStore) DeleteSessionByIDHash(ctx context.Context, userID, idHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM sessions WHERE user_id = ? AND id_hash = ?`),
		userID, idHash,
	)
	if err != nil {
		return false, fmt.Errorf("delete session by id_hash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// DeleteOtherSessionsForUser drops every row owned by userID except the one
// matching exceptIDHash. The caller is expected to pass the current
// request's session id_hash so the operator stays signed in.
func (s *DBStore) DeleteOtherSessionsForUser(ctx context.Context, userID, exceptIDHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM sessions WHERE user_id = ? AND id_hash != ?`),
		userID, exceptIDHash,
	)
	if err != nil {
		return 0, fmt.Errorf("delete other sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
