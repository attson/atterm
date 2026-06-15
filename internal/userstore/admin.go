package userstore

import (
	"context"
	"fmt"
)

// SetUserAdmin flips users.is_admin for the given userID. Idempotent;
// no-op (and no error) when the userID does not exist.
func (s *SQLiteStore) SetUserAdmin(ctx context.Context, userID string, admin bool) error {
	var v int
	if admin {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_admin = ? WHERE id = ?`, v, userID)
	if err != nil {
		return fmt.Errorf("set is_admin: %w", err)
	}
	return nil
}
