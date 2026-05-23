package main

import (
	"context"

	"github.com/attson/atterm/internal/userstore"
	"github.com/attson/atterm/internal/webhook"
)

// webhookStoreAdapter maps userstore.Webhook rows into the minimal
// webhook.Webhook type the dispatcher needs, so internal/webhook never
// imports userstore (no import cycle).
type webhookStoreAdapter struct{ s userstore.Store }

func (a webhookStoreAdapter) ListWebhooks(ctx context.Context, userID string) ([]webhook.Webhook, error) {
	rows, err := a.s.ListWebhooks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]webhook.Webhook, 0, len(rows))
	for _, r := range rows {
		out = append(out, webhook.Webhook{ID: r.ID, URL: r.URL, Format: r.Format})
	}
	return out, nil
}
