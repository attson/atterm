package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// VAPIDKeys is the singleton Web Push signing keypair (web_push_keys, id=1).
type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

// GetVAPIDKeys returns the keypair and ok=false if none has been generated.
func (s *DBStore) GetVAPIDKeys(ctx context.Context) (VAPIDKeys, bool, error) {
	var k VAPIDKeys
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT private_key, public_key FROM web_push_keys WHERE id = 1`)).
		Scan(&k.PrivateKey, &k.PublicKey)
	if errors.Is(err, sql.ErrNoRows) {
		return VAPIDKeys{}, false, nil
	}
	if err != nil {
		return VAPIDKeys{}, false, fmt.Errorf("get vapid keys: %w", err)
	}
	return k, true, nil
}

// SetVAPIDKeys upserts the singleton keypair.
func (s *DBStore) SetVAPIDKeys(ctx context.Context, k VAPIDKeys) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO web_push_keys(id, private_key, public_key, created_at)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     private_key = excluded.private_key,
		     public_key  = excluded.public_key,
		     created_at  = excluded.created_at`),
		k.PrivateKey, k.PublicKey, nowUnix())
	if err != nil {
		return fmt.Errorf("set vapid keys: %w", err)
	}
	return nil
}

// WebPushSubscription is one browser push subscription for a user.
type WebPushSubscription struct {
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt int64
}

// maxWebPushSubsPerUser is the per-user push endpoint cap; overflow of NEW
// endpoints is silently dropped, matching the pre-DB behavior.
const maxWebPushSubsPerUser = 16

// AddWebPushSubscription upserts a subscription keyed by (user_id, endpoint).
// New endpoints that would push the user over maxWebPushSubsPerUser are
// silently dropped. Updates to existing endpoints are always allowed.
func (s *DBStore) AddWebPushSubscription(ctx context.Context, userID string, sub WebPushSubscription) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add web push subscription: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Check if this (user_id, endpoint) pair already exists.
	var existing int
	if err := tx.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT count(*) FROM web_push_subscriptions WHERE user_id = ? AND endpoint = ?`),
		userID, sub.Endpoint).Scan(&existing); err != nil {
		return fmt.Errorf("add web push subscription: check existing: %w", err)
	}

	if existing == 0 {
		// New endpoint — enforce the cap.
		var total int
		if err := tx.QueryRowContext(ctx, s.dia.Rebind(
			`SELECT count(*) FROM web_push_subscriptions WHERE user_id = ?`),
			userID).Scan(&total); err != nil {
			return fmt.Errorf("add web push subscription: count subs: %w", err)
		}
		if total >= maxWebPushSubsPerUser {
			// Silent drop: cap reached, no write needed.
			return nil
		}
	}

	// Upsert the subscription. created_at is intentionally preserved on
	// conflict so we don't reset the original registration timestamp.
	if _, err := tx.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO web_push_subscriptions(user_id, endpoint, p256dh, auth, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, endpoint) DO UPDATE SET
		     p256dh = excluded.p256dh,
		     auth   = excluded.auth`),
		userID, sub.Endpoint, sub.P256dh, sub.Auth, sub.CreatedAt); err != nil {
		return fmt.Errorf("add web push subscription: upsert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add web push subscription: commit: %w", err)
	}
	return nil
}

// RemoveWebPushSubscription deletes one (user_id, endpoint) subscription.
func (s *DBStore) RemoveWebPushSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`DELETE FROM web_push_subscriptions WHERE user_id = ? AND endpoint = ?`),
		userID, endpoint)
	if err != nil {
		return fmt.Errorf("remove web push subscription: %w", err)
	}
	return nil
}

// ListWebPushSubscriptions returns all subscriptions for a user.
func (s *DBStore) ListWebPushSubscriptions(ctx context.Context, userID string) ([]WebPushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT endpoint, p256dh, auth, created_at
		 FROM web_push_subscriptions WHERE user_id = ?`), userID)
	if err != nil {
		return nil, fmt.Errorf("list web push subscriptions: %w", err)
	}
	defer rows.Close()
	var out []WebPushSubscription
	for rows.Next() {
		var sub WebPushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
