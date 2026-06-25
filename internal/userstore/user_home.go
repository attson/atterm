package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetUserHome returns the user's selected home instance_id, ok=false if unset.
func (s *DBStore) GetUserHome(ctx context.Context, userID string) (string, bool, error) {
	var instanceID string
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT instance_id FROM user_home WHERE user_id = ?`), userID).Scan(&instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get user home: %w", err)
	}
	return instanceID, true, nil
}

// SetUserHome upserts the user's selected home instance (account-level).
func (s *DBStore) SetUserHome(ctx context.Context, userID, instanceID string) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO user_home(user_id, instance_id, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		     instance_id = excluded.instance_id,
		     updated_at  = excluded.updated_at`),
		userID, instanceID, nowUnix())
	if err != nil {
		return fmt.Errorf("set user home: %w", err)
	}
	return nil
}
