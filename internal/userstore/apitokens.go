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

// ErrTokenInvalid is returned when an API token is not found, revoked, or its
// owner is disabled. The three conditions are intentionally indistinguishable
// to callers to prevent oracles.
var ErrTokenInvalid = errors.New("userstore: api token invalid, revoked, or owner disabled")

// APIToken is the row shape exposed for admin/user listings. It never carries
// the plaintext token — only the stored prefix for UI hinting.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	Prefix     string     // "atk_xxxxxxxx" (12 chars: "atk_" + 8 body chars) for UI listings
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// tokenHash returns the hex-lowercased sha256 of the plaintext API token.
func tokenHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken generates a new API token for userID with the given display name.
// Returns the Secret (plaintext accessible only via .Expose()) and the stored
// APIToken metadata. The plaintext is returned exactly once and is never stored.
//
// Token format: "atk_" + base64url-no-padding(32 random bytes) ≈ 47 chars total.
// token_prefix = first 12 chars of plaintext ("atk_" + first 8 body chars).
// token_hash   = hex(sha256(plaintext)).
func (s *SQLiteStore) CreateAPIToken(ctx context.Context, userID, name string) (Secret, *APIToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, nil, fmt.Errorf("rand: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw) // 43 chars
	plaintext := "atk_" + body                        // 47 chars total

	hash := tokenHash(plaintext)
	prefix := plaintext[:12] // "atk_" (4) + first 8 body chars

	id := defaultIDs.New()
	now := time.Now()
	nowUnix := now.Unix()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens(id, user_id, name, token_hash, token_prefix, created_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		id, userID, name, hash, prefix, nowUnix,
	)
	if err != nil {
		return Secret{}, nil, fmt.Errorf("insert api_token: %w", err)
	}

	tok := &APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		CreatedAt: now,
	}
	return NewSecret(plaintext, "atk_"), tok, nil
}

// LookupAPIToken validates plaintext and returns (tokenID, userID) on success.
// Returns ErrTokenInvalid when the token is unknown, revoked, or its owner is
// disabled — all three map to the same error to prevent oracles.
func (s *SQLiteStore) LookupAPIToken(ctx context.Context, plaintext string) (tokenID, userID string, err error) {
	hash := tokenHash(plaintext)

	err = s.db.QueryRowContext(ctx,
		`SELECT api_tokens.id, api_tokens.user_id
		 FROM api_tokens
		 JOIN users ON users.id = api_tokens.user_id
		 WHERE api_tokens.token_hash = ?
		   AND api_tokens.revoked_at IS NULL
		   AND users.disabled_at IS NULL`,
		hash,
	).Scan(&tokenID, &userID)

	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("lookup api_token: %w", err)
	}
	return tokenID, userID, nil
}

// RevokeAPIToken marks the token as revoked. It verifies that tokenID belongs
// to userID to prevent cross-user revocation. Returns an error if the token
// does not exist or belongs to a different user.
func (s *SQLiteStore) RevokeAPIToken(ctx context.Context, tokenID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens
		 SET revoked_at = strftime('%s','now')
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		tokenID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke api_token: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("userstore: token %q not found or not owned by user %q", tokenID, userID)
	}
	return nil
}

// ListAPITokens returns all API tokens for userID, most recent first.
// The returned APIToken rows carry Prefix for UI hinting but no plaintext.
func (s *SQLiteStore) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_prefix, created_at, last_used_at, revoked_at
		 FROM api_tokens
		 WHERE user_id = ?
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list api_tokens: %w", err)
	}
	defer rows.Close()

	var out []APIToken
	for rows.Next() {
		var (
			id         string
			uid        string
			name       string
			prefix     string
			createdAt  int64
			lastUsedAt sql.NullInt64
			revokedAt  sql.NullInt64
		)
		if err := rows.Scan(&id, &uid, &name, &prefix, &createdAt, &lastUsedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan api_token: %w", err)
		}
		tok := APIToken{
			ID:        id,
			UserID:    uid,
			Name:      name,
			Prefix:    prefix,
			CreatedAt: time.Unix(createdAt, 0),
		}
		if lastUsedAt.Valid {
			t := time.Unix(lastUsedAt.Int64, 0)
			tok.LastUsedAt = &t
		}
		if revokedAt.Valid {
			t := time.Unix(revokedAt.Int64, 0)
			tok.RevokedAt = &t
		}
		out = append(out, tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api_tokens: %w", err)
	}
	return out, nil
}

// TouchAPIToken updates last_used_at for the given token ID.
// This is a single-statement update; no select-then-update race.
// Called by the Task 3.3 batch committer — not on the hot lookup path.
func (s *SQLiteStore) TouchAPIToken(ctx context.Context, tokenID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = strftime('%s','now') WHERE id = ?`,
		tokenID,
	)
	if err != nil {
		return fmt.Errorf("touch api_token: %w", err)
	}
	return nil
}
