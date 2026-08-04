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

// Claim-token sentinels. Mirrors the pairing-token shape (NotFound vs.
// Consumed vs. Expired) so the OPAQUE register/finalize handler can map
// them all to a single generic 401 while leaving operator-facing logs
// able to tell the cases apart.
var (
	ErrClaimTokenNotFound = errors.New("userstore: claim token not found")
	ErrClaimTokenConsumed = errors.New("userstore: claim token already consumed")
	ErrClaimTokenExpired  = errors.New("userstore: claim token expired")
)

// ClaimToken is the row shape exposed to callers. It never carries the
// plaintext — only the bound email/role and the consumption window.
type ClaimToken struct {
	Email      string
	Role       string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

func claimTokenHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateClaimToken mints a new single-use claim token returning the
// plaintext (shown to operator exactly once) and persists the hash.
//
// Plaintext format: "claim_" + base64url-no-padding(32 random bytes) ≈ 49 chars.
func (s *DBStore) CreateClaimToken(ctx context.Context, email, role string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	plaintext := "claim_" + base64.RawURLEncoding.EncodeToString(raw)

	hash := claimTokenHash(plaintext)
	now := time.Now()
	expires := now.Add(ttl)
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO claim_tokens(token_hash, email, role, expires_at, consumed_at)
		 VALUES(?, ?, ?, ?, NULL)`),
		hash, email, role, expires.Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("insert claim_token: %w", err)
	}
	return plaintext, nil
}

// LookupClaimToken validates a claim token (existence, not expired, not
// consumed) and returns the email + role to register. Does NOT consume —
// the OPAQUE register/finalize handler validates up-front to avoid
// orphaning a user row, then calls ConsumeClaimToken after the user
// has been created.
func (s *DBStore) LookupClaimToken(ctx context.Context, plaintext string) (ClaimToken, error) {
	hash := claimTokenHash(plaintext)
	var (
		email, role string
		expiresAt   int64
		consumedAt  sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT email, role, expires_at, consumed_at
		 FROM claim_tokens WHERE token_hash = ?`), hash,
	).Scan(&email, &role, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ClaimToken{}, ErrClaimTokenNotFound
	}
	if err != nil {
		return ClaimToken{}, fmt.Errorf("query claim_token: %w", err)
	}
	if consumedAt.Valid {
		return ClaimToken{}, ErrClaimTokenConsumed
	}
	if time.Now().Unix() > expiresAt {
		return ClaimToken{}, ErrClaimTokenExpired
	}
	return ClaimToken{
		Email:     email,
		Role:      role,
		ExpiresAt: time.Unix(expiresAt, 0).UTC(),
	}, nil
}

// ConsumeClaimToken atomically marks the token consumed. Returns
// ErrClaimTokenConsumed if it was already consumed (lost the race) —
// same shape as ConsumePairingToken so the relay can collapse both into
// a generic 401 to the client.
func (s *DBStore) ConsumeClaimToken(ctx context.Context, plaintext string) error {
	hash := claimTokenHash(plaintext)
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE claim_tokens SET consumed_at = ?
		 WHERE token_hash = ? AND consumed_at IS NULL`),
		time.Now().Unix(), hash,
	)
	if err != nil {
		return fmt.Errorf("consume claim_token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrClaimTokenConsumed
	}
	return nil
}
