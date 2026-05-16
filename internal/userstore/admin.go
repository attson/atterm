package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// ErrEmptyBootstrapPassword is returned by EnsureAdminUser when creating
// a brand-new user without a plaintext password to hash.
var ErrEmptyBootstrapPassword = errors.New("userstore: empty plaintext for new admin user")

// EnsureAdminUser is the bootstrap entry point — see the Store interface
// docstring for semantics.
func (s *SQLiteStore) EnsureAdminUser(ctx context.Context, email, plaintext string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var existingID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = ?`, email,
	).Scan(&existingID)
	if err == nil {
		// User exists: promote, ignore password.
		if err := s.SetUserAdmin(ctx, existingID, true); err != nil {
			return false, err
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("lookup admin email: %w", err)
	}
	// User does not exist. Need a plaintext password to create.
	if plaintext == "" {
		return false, ErrEmptyBootstrapPassword
	}
	u, err := s.CreateUser(ctx, email, plaintext)
	if err != nil {
		return false, fmt.Errorf("create admin user: %w", err)
	}
	if err := s.SetUserAdmin(ctx, u.ID, true); err != nil {
		return false, err
	}
	return true, nil
}
