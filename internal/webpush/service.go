package webpush

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/userstore"
)

// HTTPClient mirrors webpush-go's HTTPClient interface (single Do method)
// so tests outside the package can satisfy it without importing webpush-go.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// InjectTransportForTesting replaces the transport with one driven by the
// given HTTPClient. Test-only; production callers should never invoke this.
func InjectTransportForTesting(s *Service, hc HTTPClient) {
	s.tr = newTransport(s.vapidPriv, s.vapidPub, s.subject, hc)
}

// Service is the public face of the webpush package. One per relay process.
type Service struct {
	subject   string
	store     userstore.Store
	vapidPriv string
	vapidPub  string
	tr        *transport
}

// Open initializes the service. On first open, if no VAPID keys exist in the
// DB, a fresh keypair is generated and persisted. Returns a non-nil error only
// when key generation or DB operations fail.
func Open(store userstore.Store, vapidSubject string) (*Service, error) {
	if vapidSubject == "" {
		vapidSubject = "mailto:noreply@atterm.local"
	}
	ctx := context.Background()
	keys, ok, err := store.GetVAPIDKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("load vapid keys: %w", err)
	}
	if !ok {
		priv, pub, gerr := generateVAPIDKeypair()
		if gerr != nil {
			return nil, fmt.Errorf("generate vapid: %w", gerr)
		}
		keys = userstore.VAPIDKeys{PrivateKey: priv, PublicKey: pub}
		if serr := store.SetVAPIDKeys(ctx, keys); serr != nil {
			return nil, fmt.Errorf("persist vapid: %w", serr)
		}
	}
	s := &Service{
		subject:   vapidSubject,
		store:     store,
		vapidPriv: keys.PrivateKey,
		vapidPub:  keys.PublicKey,
	}
	s.tr = newTransport(s.vapidPriv, s.vapidPub, s.subject, nil)
	return s, nil
}

// PublicKey returns the VAPID public key as a base64url string for the
// browser's applicationServerKey. vapidPub is immutable after Open, so no
// lock is needed.
func (s *Service) PublicKey() string {
	return s.vapidPub
}

// AddSubscription registers a subscription in the DB.
func (s *Service) AddSubscription(userID string, sub Subscription) error {
	ctx := context.Background()
	dbSub := userstore.WebPushSubscription{
		Endpoint:  sub.Endpoint,
		P256dh:    sub.Keys.P256dh,
		Auth:      sub.Keys.Auth,
		CreatedAt: sub.CreatedAt,
	}
	if dbSub.CreatedAt == 0 {
		dbSub.CreatedAt = time.Now().Unix()
	}
	return s.store.AddWebPushSubscription(ctx, userID, dbSub)
}

// RemoveSubscription deregisters an endpoint from the DB.
func (s *Service) RemoveSubscription(userID, endpoint string) error {
	return s.store.RemoveWebPushSubscription(context.Background(), userID, endpoint)
}

// SubscriptionsForUser returns subscriptions for the given user ID.
// Used by tests and the HTTP layer to verify state.
func (s *Service) SubscriptionsForUser(userID string) []Subscription {
	subs, err := s.store.ListWebPushSubscriptions(context.Background(), userID)
	if err != nil {
		logging.Warn("webpush", "SubscriptionsForUser(%s): %v", userID, err)
		return nil
	}
	out := make([]Subscription, len(subs))
	for i, dbSub := range subs {
		var ws Subscription
		ws.Endpoint = dbSub.Endpoint
		ws.Keys.P256dh = dbSub.P256dh
		ws.Keys.Auth = dbSub.Auth
		ws.CreatedAt = dbSub.CreatedAt
		out[i] = ws
	}
	return out
}
