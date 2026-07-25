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

// Pairing-token sentinels. Callers in the relay layer collapse all three into
// the same wire-level response to prevent oracle attacks on token validity,
// but the userstore distinguishes them so logs/admin tools can tell apart a
// missing code from an expired or already-consumed one.
var (
	ErrPairingNotFound = errors.New("userstore: pairing token not found")
	ErrPairingConsumed = errors.New("userstore: pairing token already consumed")
	ErrPairingExpired  = errors.New("userstore: pairing token expired")
)

// ErrPairingInvalid is the legacy umbrella sentinel kept for callers that
// want the anti-oracle behaviour. New code should prefer the specific
// ErrPairing{NotFound,Consumed,Expired} errors above.
var ErrPairingInvalid = errors.New("userstore: pairing token invalid, expired, or already consumed")

// PairingToken is the row shape exposed to callers. It never carries the
// plaintext — only the stored hash and a short prefix for audit logging.
type PairingToken struct {
	ID         int64
	Hash       string
	Prefix     string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

func pairingHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreatePairingToken mints a new single-use pairing token for userID with the
// given TTL. The plaintext is returned exactly once through Secret.Expose()
// and is never persisted server-side.
//
// Plaintext format: "pair_" + base64url-no-padding(32 random bytes) ≈ 47 chars.
// wrap may be nil; when non-nil, stored verbatim in wrapped_account_key.
func (s *SQLiteStore) CreatePairingToken(ctx context.Context, userID string, ttl time.Duration, wrap []byte) (Secret, *PairingToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, nil, fmt.Errorf("rand: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	plaintext := "pair_" + body

	hash := pairingHash(plaintext)
	prefix := plaintext[:12] // "pair_" (5) + first 7 body chars

	now := time.Now()
	expires := now.Add(ttl)

	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO pairing_tokens(token_hash, prefix, user_id, created_at, expires_at, consumed_at, wrapped_account_key)
		 VALUES(?, ?, ?, ?, ?, NULL, ?)`),
		hash, prefix, userID, now.Unix(), expires.Unix(), wrap,
	)
	if err != nil {
		return Secret{}, nil, fmt.Errorf("insert pairing_token: %w", err)
	}
	id, _ := res.LastInsertId()

	return NewSecret(plaintext, "pair_"), &PairingToken{
		ID:        id,
		Hash:      hash,
		Prefix:    prefix,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}

// ConsumePairingToken validates a pair code, marks it consumed, and returns
// the owning user. The caller is responsible for minting a session token.
//
// Concurrency: the atomic UPDATE with the consumed_at IS NULL guard makes
// "exactly one consumer wins"; the rest get ErrPairingConsumed even if they
// pass the validity check on the read row.
func (s *SQLiteStore) ConsumePairingToken(ctx context.Context, plaintext string) (*User, error) {
	hash := pairingHash(plaintext)

	var (
		userID     string
		expiresAt  int64
		consumedAt sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT user_id, expires_at, consumed_at
		 FROM pairing_tokens WHERE token_hash = ?`), hash,
	).Scan(&userID, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPairingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup pairing_token: %w", err)
	}
	if consumedAt.Valid {
		return nil, ErrPairingConsumed
	}
	if time.Now().Unix() > expiresAt {
		return nil, ErrPairingExpired
	}

	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE pairing_tokens SET consumed_at = ?
		 WHERE token_hash = ? AND consumed_at IS NULL`),
		time.Now().Unix(), hash,
	)
	if err != nil {
		return nil, fmt.Errorf("consume pairing_token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		// Lost the race to another consumer.
		return nil, ErrPairingConsumed
	}

	return s.GetUser(ctx, userID)
}
