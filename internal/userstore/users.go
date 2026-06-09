package userstore

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var (
	ErrEmailTaken        = errors.New("userstore: email already registered")
	ErrUserNotFound      = errors.New("userstore: user not found")
	ErrPasswordIncorrect = errors.New("userstore: current password incorrect")
)

// User is the row shape exposed to callers. password_hash is intentionally
// not exported.
type User struct {
	ID         string
	Email      string
	IsAdmin    bool
	CreatedAt  time.Time
	DisabledAt *time.Time
}

// argonParams are the SEC-2 fixed parameters. Stored hashes carry these
// inline so future tuning does not invalidate old hashes.
var argonParams = struct {
	time, memory uint32
	threads      uint8
	keyLen       uint32
}{time: 3, memory: 64 * 1024, threads: 2, keyLen: 32}

// dummyHash is generated once at process start so missing-email login
// paths spend the same wall-clock time as a real verify (SEC-3).
var dummyHash = func() string {
	h, err := hashPassword("missing-email-dummy-value-9e8d7c6b5a")
	if err != nil {
		panic("userstore: dummy hash init failed: " + err.Error())
	}
	return h
}()

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt,
		argonParams.time, argonParams.memory, argonParams.threads, argonParams.keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonParams.memory, argonParams.time, argonParams.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &t, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// HashPasswordForBootstrap is a public wrapper around the internal hashPassword
// helper. It exists for callers in internal/relay that need to generate
// argon2id hashes at process startup (e.g. the dummy hash for timing-safe
// missing-email login). Application code should use CreateUser instead.
func HashPasswordForBootstrap(pw string) (string, error) { return hashPassword(pw) }

// VerifyPasswordForBootstrap is a public wrapper around verifyPassword.
// Same caveat as HashPasswordForBootstrap: prefer VerifyPassword for the
// application path. This is for low-level pool/argon2 plumbing only.
func VerifyPasswordForBootstrap(pw, encoded string) bool { return verifyPassword(pw, encoded) }

// CreateUser inserts a new user with the given email and password.
// Email is lowercased for storage; uniqueness is case-insensitive via
// COLLATE NOCASE on the column.
func (s *SQLiteStore) CreateUser(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	id := defaultIDs.New()
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users(id, email, password_hash, created_at)
		 VALUES(?, ?, ?, ?)`,
		id, email, hash, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.email") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &User{
		ID: id, Email: email,
		CreatedAt: time.Unix(now, 0),
	}, nil
}

// VerifyPassword returns the matched user on success, or (nil, nil) when
// either the email does not exist OR the password is wrong. Both paths
// run argon2id verification against either the real hash or the global
// dummyHash, so wall-clock time is independent of email existence.
func (s *SQLiteStore) VerifyPassword(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		id         string
		hash       string
		createdAt  int64
		disabledAt sql.NullInt64
		isAdmin    int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash, created_at, disabled_at, is_admin
		 FROM users WHERE email = ?`, email,
	).Scan(&id, &hash, &createdAt, &disabledAt, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		// Constant-time dummy verify, ignore result.
		_ = verifyPassword(password, dummyHash)
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if disabledAt.Valid {
		// Run dummy verify anyway for timing parity.
		_ = verifyPassword(password, dummyHash)
		return nil, nil
	}
	if !verifyPassword(password, hash) {
		return nil, nil
	}
	u := &User{
		ID: id, Email: email,
		IsAdmin:   isAdmin != 0,
		CreatedAt: time.Unix(createdAt, 0),
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
		`SELECT email, created_at, disabled_at, is_admin
		 FROM users WHERE id = ?`, id,
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
		`UPDATE users SET disabled_at = strftime('%s','now')
		 WHERE id = ? AND disabled_at IS NULL`, id)
	return err
}

// ListUsers returns all users ordered by created_at descending (newest first).
func (s *SQLiteStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, created_at, disabled_at, is_admin FROM users ORDER BY created_at DESC`)
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

// ChangePassword verifies currentPlaintext against userID's stored hash.
// On success it hashes newPlaintext, deletes all sessions for the user
// (caller must issue a new session), and returns nil.
// Returns ErrUserNotFound if the user ID is unknown, ErrPasswordIncorrect if
// the current password does not match.
func (s *SQLiteStore) ChangePassword(ctx context.Context, userID, currentPlaintext, newPlaintext string) error {
	// Look up current password_hash.
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM users WHERE id = ?`, userID,
	).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	// Verify current password.
	if !verifyPassword(currentPlaintext, hash) {
		return ErrPasswordIncorrect
	}

	// Hash the new password.
	newHash, err := hashPassword(newPlaintext)
	if err != nil {
		return fmt.Errorf("hash: %w", err)
	}

	// Update in a transaction: new hash + delete all sessions.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash=? WHERE id=?`,
		newHash, userID); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ResetUserPassword generates a new temporary password (prefix "tmp_"), updates
// the user's password_hash, and deletes all sessions for that user — all in
// one transaction. Returns the plaintext password once.
func (s *SQLiteStore) ResetUserPassword(ctx context.Context, userID string) (Secret, error) {
	// Generate 16 random bytes → base64url → "tmp_<22 chars>" (total ~26 chars).
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, fmt.Errorf("rand: %w", err)
	}
	plaintext := "tmp_" + base64.RawURLEncoding.EncodeToString(raw)

	hash, err := hashPassword(plaintext)
	if err != nil {
		return Secret{}, fmt.Errorf("hash: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Secret{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash=? WHERE id=?`,
		hash, userID); err != nil {
		return Secret{}, fmt.Errorf("update password: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return Secret{}, fmt.Errorf("delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Secret{}, fmt.Errorf("commit: %w", err)
	}

	return NewSecret(plaintext, "tmp_"), nil
}
