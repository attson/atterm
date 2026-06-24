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

// ErrSessionInvalid is returned when a session is not found, expired,
// or its owner is disabled. The three conditions are intentionally
// indistinguishable to callers to prevent oracles.
var ErrSessionInvalid = errors.New("userstore: session not found, expired, or user disabled")

// DefaultSessionTTL is the default session lifetime. expires_at is stored in
// milliseconds (UnixMilli) so sliding renewal is visible with sub-second
// precision in tests. The INTEGER column in SQLite happily holds int64
// millisecond values.
const DefaultSessionTTL = 30 * 24 * time.Hour

// Session is the public view of a row in the sessions table. id_hash is
// opaque (already hashed); plaintext tokens are not stored or exposed.
type Session struct {
	IDHash     string
	UserID     string
	UserAgent  string
	IPPrefix   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// sessionHash returns the lowercase hex sha256 of the plaintext token value.
func sessionHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// SessionHash exposes the same hash used by CreateSession / LookupSession so
// HTTP handlers can compute the id_hash of a token they hold without
// round-tripping the store.
func SessionHash(plaintext string) string { return sessionHash(plaintext) }

// CreateSession generates a new session token for userID. Returns the
// plaintext token (~43 chars of base64url, 32 bytes of crypto/rand) and the
// resulting Session row. Only the sha256 hash (id_hash) is persisted —
// never the plaintext.
//
// ttl is the lifetime; pass <=0 for an already-expired row (useful for
// tests).
func (s *SQLiteStore) CreateSession(ctx context.Context, userID, userAgent, ipPrefix string, ttl time.Duration) (string, *Session, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("rand: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw) // ~43 chars

	hash := sessionHash(plaintext)

	nowMs := time.Now().UnixMilli()
	expiresMs := nowMs + ttl.Milliseconds()

	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO sessions(id_hash, user_id, created_at, expires_at, user_agent, ip_prefix)
		 VALUES(?, ?, ?, ?, ?, ?)`),
		hash, userID, nowMs, expiresMs, userAgent, ipPrefix,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}

	sess := &Session{
		IDHash:    hash,
		UserID:    userID,
		UserAgent: userAgent,
		IPPrefix:  ipPrefix,
		CreatedAt: time.UnixMilli(nowMs),
		ExpiresAt: time.UnixMilli(expiresMs),
	}
	return plaintext, sess, nil
}

// LookupSession hashes plaintext, looks up the row joined with the owning
// user, validates that the session is not expired and that the user is not
// disabled, and returns both the *Session and the joined *User on success.
//
// Returns ErrSessionInvalid for any invalid-session condition.
func (s *SQLiteStore) LookupSession(ctx context.Context, plaintext string) (*Session, *User, error) {
	hash := sessionHash(plaintext)
	nowMs := time.Now().UnixMilli()

	var (
		idHash      string
		userID      string
		createdMs   int64
		expiresMs   int64
		lastSeenMs  int64
		userAgent   string
		ipPrefix    string
		uID         string
		uEmail      string
		uIsAdmin    int
		uCreatedAt  int64
		uDisabledAt sql.NullInt64
	)

	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT s.id_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at, s.user_agent, s.ip_prefix,
		        u.id, u.email, u.is_admin, u.created_at, u.disabled_at
		 FROM sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.id_hash = ? AND s.expires_at > ?`),
		hash, nowMs,
	).Scan(&idHash, &userID, &createdMs, &expiresMs, &lastSeenMs, &userAgent, &ipPrefix,
		&uID, &uEmail, &uIsAdmin, &uCreatedAt, &uDisabledAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lookup session: %w", err)
	}

	// Reject sessions whose owning user is disabled. Indistinguishable from
	// "not found" to callers.
	if uDisabledAt.Valid {
		return nil, nil, ErrSessionInvalid
	}

	sess := &Session{
		IDHash:     idHash,
		UserID:     userID,
		UserAgent:  userAgent,
		IPPrefix:   ipPrefix,
		CreatedAt:  time.UnixMilli(createdMs),
		ExpiresAt:  time.UnixMilli(expiresMs),
		LastSeenAt: time.UnixMilli(lastSeenMs),
	}
	user := &User{
		ID:        uID,
		Email:     uEmail,
		IsAdmin:   uIsAdmin != 0,
		CreatedAt: time.Unix(uCreatedAt, 0),
	}
	return sess, user, nil
}

// DeleteSession removes the session identified by the plaintext token value.
// Subsequent lookups will return ErrSessionInvalid.
func (s *SQLiteStore) DeleteSession(ctx context.Context, plaintext string) error {
	hash := sessionHash(plaintext)
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM sessions WHERE id_hash = ?`), hash,
	)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// PurgeExpiredSessions deletes all sessions whose expires_at is in the past.
// Returns the number of rows deleted. Called by the background purge
// goroutine.
func (s *SQLiteStore) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	nowMs := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM sessions WHERE expires_at < ?`), nowMs,
	)
	if err != nil {
		return 0, fmt.Errorf("purge expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
