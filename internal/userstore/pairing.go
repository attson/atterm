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

// ConsumePairingToken atomically marks the token consumed, mints a new API
// token for the same user (source='pairing'), and returns it. The three
// failure conditions (unknown, expired, consumed) collapse into a single
// ErrPairingInvalid so callers cannot distinguish them (anti-oracle).
//
// Concurrency: the atomic UPDATE with the consumed_at IS NULL guard makes
// "exactly one consumer wins"; the rest get ErrPairingInvalid even if they
// pass the validity check on the read row.
func (s *SQLiteStore) ConsumePairingToken(ctx context.Context, plaintext string) (Secret, string, error) {
	hash := pairingHash(plaintext)
	now := time.Now().Unix()

	res, err := s.db.ExecContext(ctx,
		`UPDATE pairing_tokens
		 SET consumed_at = ?
		 WHERE token_hash = ?
		   AND consumed_at IS NULL
		   AND expires_at > ?`,
		now, hash, now,
	)
	if err != nil {
		return Secret{}, "", fmt.Errorf("consume pairing: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Secret{}, "", fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return Secret{}, "", ErrPairingInvalid
	}

	// We won the race. Look up the owning user_id and mint a fresh API token.
	var userID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM pairing_tokens WHERE token_hash = ?`, hash,
	).Scan(&userID); err != nil {
		return Secret{}, "", fmt.Errorf("lookup user_id: %w", err)
	}

	secret, _, err := s.CreateAPITokenWithSource(ctx, userID, "mobile (paired)", "pairing")
	if err != nil {
		return Secret{}, "", fmt.Errorf("mint api_token: %w", err)
	}
	return secret, userID, nil
}
