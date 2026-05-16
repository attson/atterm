package userstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrWebSessionInvalid is returned when a web session is not found, expired,
// or its owner is disabled. The three conditions are intentionally
// indistinguishable to callers to prevent oracles.
var ErrWebSessionInvalid = errors.New("userstore: web session not found, expired, or user disabled")

// webSessionTTL is the session lifetime. expires_at is stored in milliseconds
// (UnixMilli) so sliding renewal is visible with sub-second precision in tests.
// The INTEGER column in SQLite happily holds int64 millisecond values.
const webSessionTTL = 30 * 24 * time.Hour

// sessionHash returns the lowercase hex sha256 of the plaintext cookie value.
func sessionHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateWebSession generates a new cookie session for userID.
// Returns a Secret whose Expose() value is the opaque cookie (32 bytes of
// crypto/rand encoded as base64url, no padding, ~43 chars). The Secret has an
// empty prefix because the cookie is never displayed in a UI listing.
//
// Only the sha256 hash (id_hash) is persisted — never the plaintext.
func (s *SQLiteStore) CreateWebSession(ctx context.Context, userID, userAgent, ipPrefix string) (Secret, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, fmt.Errorf("rand: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw) // ~43 chars

	hash := sessionHash(plaintext)

	nowMs := time.Now().UnixMilli()
	expiresMs := nowMs + webSessionTTL.Milliseconds()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO web_sessions(id_hash, user_id, created_at, expires_at, user_agent, ip_prefix)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		hash, userID, nowMs, expiresMs, userAgent, ipPrefix,
	)
	if err != nil {
		return Secret{}, fmt.Errorf("insert web_session: %w", err)
	}

	// Empty prefix: the cookie value is opaque and never shown in a UI listing.
	return NewSecret(plaintext, ""), nil
}

// LookupWebSession validates plaintext and returns (userID, csrfSecret) on
// success. It checks that the session is not expired and that the owning user
// is not disabled. On success it slides expires_at forward by webSessionTTL
// using a transaction so the SELECT and UPDATE are atomic.
//
// Returns ErrWebSessionInvalid for any invalid-session condition.
func (s *SQLiteStore) LookupWebSession(ctx context.Context, plaintext string) (userID string, csrfSecret []byte, err error) {
	hash := sessionHash(plaintext)
	nowMs := time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var (
		uid        string
		disabledAt sql.NullInt64
		expiresAt  int64
		csrf       []byte
	)

	queryErr := tx.QueryRowContext(ctx,
		`SELECT ws.user_id, u.disabled_at, ws.expires_at, u.csrf_secret
		 FROM web_sessions ws
		 JOIN users u ON u.id = ws.user_id
		 WHERE ws.id_hash = ?`,
		hash,
	).Scan(&uid, &disabledAt, &expiresAt, &csrf)

	if errors.Is(queryErr, sql.ErrNoRows) {
		err = ErrWebSessionInvalid
		return "", nil, err
	}
	if queryErr != nil {
		err = fmt.Errorf("lookup web_session: %w", queryErr)
		return "", nil, err
	}

	// Check expiry in Go (milliseconds).
	if expiresAt < nowMs {
		err = ErrWebSessionInvalid
		return "", nil, err
	}

	// Check user not disabled.
	if disabledAt.Valid {
		err = ErrWebSessionInvalid
		return "", nil, err
	}

	// Slide expires_at forward.
	newExpiry := nowMs + webSessionTTL.Milliseconds()
	if _, err = tx.ExecContext(ctx,
		`UPDATE web_sessions SET expires_at = ? WHERE id_hash = ?`,
		newExpiry, hash,
	); err != nil {
		err = fmt.Errorf("renew web_session: %w", err)
		return "", nil, err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("commit web_session renewal: %w", err)
		return "", nil, err
	}

	return uid, csrf, nil
}

// DeleteWebSession removes the session identified by the plaintext cookie value.
// Subsequent lookups will return ErrWebSessionInvalid.
func (s *SQLiteStore) DeleteWebSession(ctx context.Context, plaintext string) error {
	hash := sessionHash(plaintext)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM web_sessions WHERE id_hash = ?`, hash,
	)
	if err != nil {
		return fmt.Errorf("delete web_session: %w", err)
	}
	return nil
}

// PurgeExpiredWebSessions deletes all sessions whose expires_at is in the past.
// Returns the number of rows deleted. Called by the background purge goroutine
// in Task 3.0; provided here as the database-layer primitive.
func (s *SQLiteStore) PurgeExpiredWebSessions(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM web_sessions WHERE expires_at < ?`, nowMs,
	)
	if err != nil {
		return 0, fmt.Errorf("purge expired web_sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
