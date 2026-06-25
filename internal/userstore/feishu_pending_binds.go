// internal/userstore/feishu_pending_binds.go
package userstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrFeishuPendingBindNotFound is returned by Consume when no matching
// non-expired row exists. We do not distinguish "wrong code" from
// "expired" — both surface as the same error to avoid an oracle.
var ErrFeishuPendingBindNotFound = errors.New("userstore: feishu pending bind not found or expired")

// feishuPairAlphabet excludes I, O, 0, 1, L (visually confusable).
const feishuPairAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateFeishuPairCode returns a 6-char short-code drawn from the
// confusable-free alphabet. Entropy: ~30 bits (32^6 ≈ 1 billion);
// safe against brute force inside a 15-minute window.
func GenerateFeishuPairCode() string {
	buf := make([]byte, 6)
	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		// rand.Read should never fail; panicking matches stdlib practice.
		panic(fmt.Sprintf("rand.Read: %v", err))
	}
	for i, b := range rb {
		buf[i] = feishuPairAlphabet[int(b)%len(feishuPairAlphabet)]
	}
	return string(buf)
}

// PutFeishuPendingBind upserts the user's pending code (one row per user).
// expiresAt is unix seconds.
func (s *SQLiteStore) PutFeishuPendingBind(ctx context.Context, userID, code string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO feishu_pending_binds(user_id, code, expires_at)
		 VALUES(?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   code = excluded.code,
		   expires_at = excluded.expires_at`),
		userID, code, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("put pending bind: %w", err)
	}
	return nil
}

// ConsumeFeishuPendingBind atomically deletes a non-expired row matching
// code and returns the owning user_id. Concurrent callers race on the
// DELETE — only one wins.
func (s *SQLiteStore) ConsumeFeishuPendingBind(ctx context.Context, code string) (string, error) {
	now := time.Now().Unix()
	var userID string
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`DELETE FROM feishu_pending_binds
		 WHERE code = ? AND expires_at > ?
		 RETURNING user_id`),
		code, now,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrFeishuPendingBindNotFound
	}
	if err != nil {
		return "", fmt.Errorf("consume pending bind: %w", err)
	}
	return userID, nil
}

// SweepExpiredFeishuPendingBinds deletes all expired rows. Returns count.
func (s *SQLiteStore) SweepExpiredFeishuPendingBinds(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM feishu_pending_binds WHERE expires_at <= ?`),
		time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("sweep: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
