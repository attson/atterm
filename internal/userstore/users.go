package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrEmailTaken   = errors.New("userstore: email already registered")
	ErrUserNotFound = errors.New("userstore: user not found")
)

// User is the row shape exposed to callers. password_hash is removed in
// favor of OPAQUE auth (see migration 0003_opaque_auth.sql); AuthMode
// records which auth backend owns the credential ("opaque" only in v1).
type User struct {
	ID         string
	Email      string
	IsAdmin    bool
	AuthMode   string // "opaque" only in v1
	CreatedAt  time.Time
	DisabledAt *time.Time
}

// CreateOpaqueUser inserts a user row tagged as auth_mode='opaque', with
// no password column populated. The caller is responsible for separately
// storing the OPAQUE registration record (StoreOpaqueRecord) and the
// account_key wrap (StoreAccountKeyWrap). Used by the OPAQUE
// register/finalize flow.
//
// Email is lowercased to match the case-insensitive UNIQUE COLLATE NOCASE
// constraint. Returns ErrEmailTaken on uniqueness conflict so callers
// can surface 409 consistently. We reuse the existing ULID generator
// (defaultIDs) so user IDs stay homogeneous.
func (s *SQLiteStore) CreateOpaqueUser(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	id := defaultIDs.New()
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO users(id, email, is_admin, auth_mode, created_at) VALUES (?, ?, ?, ?, ?)`),
		id, email, 0, "opaque", now.Unix())
	if err != nil {
		if s.dia.IsUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &User{
		ID:        id,
		Email:     email,
		IsAdmin:   false,
		AuthMode:  "opaque",
		CreatedAt: now,
	}, nil
}

// GetUserByEmail looks up a user row by email (case-insensitive via the
// users.email COLLATE NOCASE column). Returns ErrUserNotFound if no row
// matches. Used by the OPAQUE login init path which needs (a) the
// user_id to look up the stored RegistrationRecord and (b) the
// auth_mode to gate the flow.
func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		id         string
		authMode   sql.NullString
		createdAt  int64
		disabledAt sql.NullInt64
		isAdmin    int
	)
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT id, auth_mode, created_at, disabled_at, is_admin
		 FROM users WHERE email = ?`), email,
	).Scan(&id, &authMode, &createdAt, &disabledAt, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	} else if err != nil {
		return nil, fmt.Errorf("lookup user by email: %w", err)
	}
	u := &User{
		ID: id, Email: email,
		IsAdmin:   isAdmin != 0,
		AuthMode:  authMode.String,
		CreatedAt: time.Unix(createdAt, 0),
	}
	if disabledAt.Valid {
		t := time.Unix(disabledAt.Int64, 0)
		u.DisabledAt = &t
	}
	return u, nil
}

// GetUser by id; returns ErrUserNotFound if missing.
func (s *SQLiteStore) GetUser(ctx context.Context, id string) (*User, error) {
	var (
		email      string
		createdAt  int64
		disabledAt sql.NullInt64
		isAdmin    int
	)
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT email, created_at, disabled_at, is_admin
		 FROM users WHERE id = ?`), id,
	).Scan(&email, &createdAt, &disabledAt, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	} else if err != nil {
		return nil, err
	}
	u := &User{
		ID: id, Email: email,
		IsAdmin:   isAdmin != 0,
		CreatedAt: time.Unix(createdAt, 0),
	}
	if disabledAt.Valid {
		t := time.Unix(disabledAt.Int64, 0)
		u.DisabledAt = &t
	}
	return u, nil
}

// DisableUser sets disabled_at = now. Idempotent.
func (s *SQLiteStore) DisableUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE users SET disabled_at = ? WHERE id = ? AND disabled_at IS NULL`),
		time.Now().Unix(), id)
	return err
}

// ListUsers returns all users ordered by created_at descending (newest first).
func (s *SQLiteStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		s.dia.Rebind(`SELECT id, email, created_at, disabled_at, is_admin FROM users ORDER BY created_at DESC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var (
			id, email  string
			createdAt  int64
			disabledAt sql.NullInt64
			isAdmin    int
		)
		if err := rows.Scan(&id, &email, &createdAt, &disabledAt, &isAdmin); err != nil {
			return nil, err
		}
		u := User{ID: id, Email: email, IsAdmin: isAdmin != 0, CreatedAt: time.Unix(createdAt, 0)}
		if disabledAt.Valid {
			t := time.Unix(disabledAt.Int64, 0)
			u.DisabledAt = &t
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
