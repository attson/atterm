package webpush

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestServiceUsesDBForKeysAndSubs(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// First Open generates + persists a VAPID keypair.
	svc, err := Open(store, "mailto:x@y.z")
	if err != nil {
		t.Fatalf("open svc: %v", err)
	}
	pub := svc.PublicKey()
	if pub == "" {
		t.Fatalf("expected generated public key")
	}
	// Second Open reuses the same persisted key (no regeneration).
	svc2, err := Open(store, "mailto:x@y.z")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if svc2.PublicKey() != pub {
		t.Fatalf("vapid key changed across Open: %q vs %q", svc2.PublicKey(), pub)
	}

	u, err := store.CreateOpaqueUser(ctx, "wp@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	var sub Subscription
	sub.Endpoint = "https://push/ep"
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := svc.AddSubscription(u.ID, sub); err != nil {
		t.Fatalf("add: %v", err)
	}
	subs := svc.SubscriptionsForUser(u.ID)
	if len(subs) != 1 || subs[0].Endpoint != "https://push/ep" {
		t.Fatalf("subs = %+v", subs)
	}
}
