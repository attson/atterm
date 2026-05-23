package webhook

import (
	"context"
	"net/http"
	"time"
)

// Webhook is the minimal config the dispatcher needs. The relay maps
// userstore.Webhook into this type at the dispatch boundary so this package
// never imports userstore (avoids an import cycle).
type Webhook struct {
	ID     string
	URL    string
	Format string
}

// WebhookStore is the slice of persistence the Service depends on.
type WebhookStore interface {
	ListWebhooks(ctx context.Context, userID string) ([]Webhook, error)
}

type Service struct {
	store  WebhookStore
	client *http.Client
}

// New constructs a Service with an 8s per-POST timeout.
func New(store WebhookStore) *Service {
	return &Service{store: store, client: newClient(8 * time.Second)}
}
