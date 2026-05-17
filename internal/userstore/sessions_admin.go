package userstore

import (
	"context"
	"fmt"
	"time"
)

// ListUserWebSessions returns all non-expired sessions for userID,
// ordered by created_at DESC.
func (s *SQLiteStore) ListUserWebSessions(ctx context.Context, userID string) ([]UserWebSession, error) {
	nowMs := time.Now().UnixMilli()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id_hash, COALESCE(user_agent, ''), COALESCE(ip_prefix, ''), created_at, expires_at
		 FROM web_sessions
		 WHERE user_id = ? AND expires_at >= ?
		 ORDER BY created_at DESC`,
		userID, nowMs,
	)
	if err != nil {
		return nil, fmt.Errorf("list web_sessions: %w", err)
	}
	defer rows.Close()
	var out []UserWebSession
	for rows.Next() {
		var (
			idHash    string
			ua        string
			ipPrefix  string
			createdMs int64
			expiresMs int64
		)
		if err := rows.Scan(&idHash, &ua, &ipPrefix, &createdMs, &expiresMs); err != nil {
			return nil, fmt.Errorf("scan web_session: %w", err)
		}
		out = append(out, UserWebSession{
			IDHash:    idHash,
			UserAgent: ua,
			IPPrefix:  ipPrefix,
			CreatedAt: time.UnixMilli(createdMs),
			ExpiresAt: time.UnixMilli(expiresMs),
		})
	}
	return out, rows.Err()
}
