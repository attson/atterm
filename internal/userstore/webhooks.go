package userstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrWebhookNotOwnedOrMissing is returned by DeleteWebhook when the id is
// unknown or owned by another user. Callers map it to HTTP 404 to avoid an
// existence oracle.
var ErrWebhookNotOwnedOrMissing = errors.New("userstore: webhook not found or not owned by the requesting user")

// Webhook is a per-user outbound webhook configuration. The URL is stored and
// returned to its owner verbatim (Feishu URLs embed a bot token).
type Webhook struct {
	ID     string
	UserID string
	Name   string
	URL    string
	// Format describes how the relay renders the event body.
	// Only "generic" is supported. The legacy "feishu" custom-bot URL
	// path was removed when the relay gained Feishu app-mode integration
	// (see internal/feishu/).
	Format        string
	AllowInsecure bool
	CreatedAt     time.Time
}

func (s *SQLiteStore) CreateWebhook(ctx context.Context, userID, url, format, name string, allowInsecure bool) (*Webhook, error) {
	id := defaultIDs.New()
	now := time.Now()
	insecure := 0
	if allowInsecure {
		insecure = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks(id, user_id, name, url, format, allow_insecure, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, url, format, insecure, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert webhook: %w", err)
	}
	return &Webhook{ID: id, UserID: userID, Name: name, URL: url, Format: format, AllowInsecure: allowInsecure, CreatedAt: now}, nil
}

func (s *SQLiteStore) ListWebhooks(ctx context.Context, userID string) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, url, format, allow_insecure, created_at
		 FROM webhooks WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		var (
			id, uid, name, url, format string
			insecure                   int
			createdAt                  int64
		)
		if err := rows.Scan(&id, &uid, &name, &url, &format, &insecure, &createdAt); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		out = append(out, Webhook{
			ID: id, UserID: uid, Name: name, URL: url, Format: format,
			AllowInsecure: insecure != 0, CreatedAt: time.Unix(createdAt, 0),
		})
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteWebhook(ctx context.Context, webhookID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = ? AND user_id = ?`,
		webhookID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrWebhookNotOwnedOrMissing
	}
	return nil
}
