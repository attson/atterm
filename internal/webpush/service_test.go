package webpush

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func newTestStore(t *testing.T) *userstore.DBStore {
	t.Helper()
	ctx := context.Background()
	st, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenFreshStoreGeneratesKeysAndReturnsService(t *testing.T) {
	st := newTestStore(t)
	svc, err := Open(st, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if svc == nil {
		t.Fatal("Open returned nil service")
	}
	if svc.PublicKey() == "" {
		t.Fatal("PublicKey empty")
	}
}

func TestOpenLoadsExistingState(t *testing.T) {
	st := newTestStore(t)
	first, err := Open(st, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	pubKey := first.PublicKey()
	second, err := Open(st, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if second.PublicKey() != pubKey {
		t.Fatalf("second PublicKey changed (regenerated?); first=%q second=%q", pubKey, second.PublicKey())
	}
}

func TestServiceAddAndRemoveSubscriptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	svc, _ := Open(st, "mailto:test@example.com")

	// Need a real user since subscriptions FK to users(id).
	u, err := st.CreateOpaqueUser(ctx, "sub-roundtrip@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}

	var sub Subscription
	sub.Endpoint = "https://push.example/abc"
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := svc.AddSubscription(u.ID, sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	// Re-open with same store should see the persisted subscription.
	svc2, _ := Open(st, "mailto:test@example.com")
	if got := svc2.SubscriptionsForUser(u.ID); len(got) != 1 || got[0].Endpoint != sub.Endpoint {
		t.Fatalf("persisted subs not loaded; got %v", got)
	}

	if err := svc.RemoveSubscription(u.ID, sub.Endpoint); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	svc3, _ := Open(st, "mailto:test@example.com")
	if got := svc3.SubscriptionsForUser(u.ID); len(got) != 0 {
		t.Fatalf("subs not removed after persist; got %v", got)
	}
}
