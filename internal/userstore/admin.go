package userstore

import (
	"context"
	"fmt"
)

// AdminExists reports whether any enabled admin user exists. Used by the
// first-run setup flow: while it returns false, a registration whose email
// matches ATTERM_BOOTSTRAP_ADMIN_EMAIL is auto-promoted to admin.
func (s *SQLiteStore) AdminExists(ctx context.Context) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT EXISTS(SELECT 1 FROM users WHERE is_admin = 1 AND disabled_at IS NULL)`),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("admin exists: %w", err)
	}
	return exists == 1, nil
}

// SetUserAdmin flips users.is_admin for the given userID. Idempotent;
// no-op (and no error) when the userID does not exist.
func (s *SQLiteStore) SetUserAdmin(ctx context.Context, userID string, admin bool) error {
	var v int
	if admin {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE users SET is_admin = ? WHERE id = ?`), v, userID)
	if err != nil {
		return fmt.Errorf("set is_admin: %w", err)
	}
	return nil
}
