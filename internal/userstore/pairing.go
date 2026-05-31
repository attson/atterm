package userstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrPairingInvalid is returned when a pairing token is unknown, expired, or
// already consumed. The three conditions are deliberately indistinguishable
// to prevent oracle attacks on token validity.
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
func (s *SQLiteStore) CreatePairingToken(ctx context.Context, userID string, ttl time.Duration) (Secret, *PairingToken, error) {
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
		`INSERT INTO pairing_tokens(token_hash, prefix, user_id, created_at, expires_at, consumed_at)
		 VALUES(?, ?, ?, ?, ?, NULL)`,
		hash, prefix, userID, now.Unix(), expires.Unix(),
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

// ConsumePairingToken stub — real implementation lands in Task B3.
func (s *SQLiteStore) ConsumePairingToken(ctx context.Context, plaintext string) (Secret, string, error) {
	return Secret{}, "", ErrPairingInvalid
}
