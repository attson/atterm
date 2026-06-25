package userstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrInviteInvalid is returned when an invitation code is not found, already
// consumed, or expired. The three conditions are intentionally indistinguishable
// to callers (SEC-4: no oracle on invitation validity).
var ErrInviteInvalid = errors.New("userstore: invitation invalid or already consumed")

// Invitation is the row shape exposed for admin listings. It never carries
// the plaintext code — only the hash and a short prefix for UI hinting.
type Invitation struct {
	CodeHash   string     // sha256(plaintext) hex; never reversible
	CodePrefix string     // "inv_" + first 4 chars of base32 body (UI hint)
	Note       string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	ConsumedAt *time.Time
	ConsumedBy string
}

// inviteEncoding is the base32 alphabet used for invitation codes.
var inviteEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// inviteHash returns the hex-lowercased sha256 of the plaintext invitation code.
// Exported at package level so tests can compute expected hashes.
func inviteHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateInvitation generates a fresh one-time invitation code, stores its
// hash, and returns the Secret (plaintext visible only via .Expose()) plus
// the row metadata.
//
// expiresAt may be nil for no expiry. note is a free-form admin memo.
func (s *SQLiteStore) CreateInvitation(ctx context.Context, expiresAt *time.Time, note string) (Secret, *Invitation, error) {
	// 16 random bytes → 26 base32 chars (no padding). With the "inv_" prefix
	// the total plaintext length is 30, satisfying the "26+ chars" requirement.
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, nil, fmt.Errorf("rand: %w", err)
	}
	body := inviteEncoding.EncodeToString(raw) // always 26 chars
	plaintext := "inv_" + body

	hash := inviteHash(plaintext)
	prefix := plaintext[:len("inv_")+4] // "inv_" + first 4 body chars

	now := time.Now()
	nowUnix := now.Unix()

	var expiresUnix sql.NullInt64
	if expiresAt != nil {
		expiresUnix = sql.NullInt64{Int64: expiresAt.Unix(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO invitations(code_hash, created_by, created_at, expires_at, consumed_at, consumed_by, note)
		 VALUES(?, 'admin', ?, ?, NULL, NULL, ?)`),
		hash, nowUnix, expiresUnix, note,
	)
	if err != nil {
		return Secret{}, nil, fmt.Errorf("insert invitation: %w", err)
	}

	inv := &Invitation{
		CodeHash:  hash,
		CodePrefix: prefix,
		Note:      note,
		CreatedAt: now,
	}
	if expiresAt != nil {
		t := *expiresAt
		inv.ExpiresAt = &t
	}

	return NewSecret(plaintext, "inv_"), inv, nil
}

// ConsumeInvitation atomically marks an invitation as consumed by userID.
// It returns ErrInviteInvalid if the code is unknown, already consumed, or
// expired — all three conditions return the same error to prevent oracles.
//
// The UPDATE is a single atomic statement; no explicit transaction is needed
// because SQLite serialises writes and RowsAffected() reflects the outcome.
func (s *SQLiteStore) ConsumeInvitation(ctx context.Context, plaintext string, userID string) error {
	hash := inviteHash(plaintext)
	now := time.Now().Unix()

	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE invitations
		 SET consumed_at = ?, consumed_by = ?
		 WHERE code_hash = ?
		   AND consumed_at IS NULL
		   AND (expires_at IS NULL OR expires_at > ?)`),
		now, userID, hash, now,
	)
	if err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrInviteInvalid
	}
	return nil
}

// ListInvitations returns all invitation rows, most recent first. The admin
// UI can derive status from (consumed_at, expires_at) fields.
func (s *SQLiteStore) ListInvitations(ctx context.Context) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx,
		s.dia.Rebind(`SELECT code_hash, note, created_at, expires_at, consumed_at, consumed_by
		 FROM invitations
		 ORDER BY created_at DESC`),
	)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()

	var out []Invitation
	for rows.Next() {
		var (
			hash       string
			note       string
			createdAt  int64
			expiresAt  sql.NullInt64
			consumedAt sql.NullInt64
			consumedBy sql.NullString
		)
		if err := rows.Scan(&hash, &note, &createdAt, &expiresAt, &consumedAt, &consumedBy); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		inv := Invitation{
			CodeHash:  hash,
			Note:      note,
			CreatedAt: time.Unix(createdAt, 0),
		}
		// CodePrefix: we only have the hash, not the plaintext, so reconstruct
		// from hash prefix for display purposes (first 8 hex chars as hint).
		// The admin-facing prefix is stored separately; since we don't store it,
		// we expose the first 8 chars of the hash as the hint.
		inv.CodePrefix = "inv_" + hash[:8]

		if expiresAt.Valid {
			t := time.Unix(expiresAt.Int64, 0)
			inv.ExpiresAt = &t
		}
		if consumedAt.Valid {
			t := time.Unix(consumedAt.Int64, 0)
			inv.ConsumedAt = &t
		}
		if consumedBy.Valid {
			inv.ConsumedBy = consumedBy.String
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invitations: %w", err)
	}
	return out, nil
}
