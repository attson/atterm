package relay

import (
	"context"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestResolveHomeInstanceURL(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	u, _ := store.CreateOpaqueUser(ctx, "h@example.com")
	now := time.Now().Unix()

	// Single-instance (thisInstanceID empty) → empty home, no assignment.
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, ""); err != nil || url != "" {
		t.Fatalf("single-instance: %q %v", url, err)
	}
	if _, ok, _ := store.GetUserHome(ctx, u.ID); ok {
		t.Fatalf("single-instance must not assign a home")
	}

	// Unset home → auto-assign the serving node and return its URL.
	this := "https://this.example"
	url, err := resolveHomeInstanceURL(ctx, store, u.ID, this)
	if err != nil || url != this {
		t.Fatalf("auto-assign: %q %v", url, err)
	}
	if id, ok, _ := store.GetUserHome(ctx, u.ID); !ok || id != this {
		t.Fatalf("home not persisted: %q %v", id, ok)
	}

	// Already-selected + alive → returns its URL.
	_ = store.UpsertInstanceHeartbeat(ctx, this, this, now)
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, this); err != nil || url != this {
		t.Fatalf("selected+alive: %q %v", url, err)
	}

	// Selected node is DEAD (point home at a stale instance) → empty.
	_ = store.SetUserHome(ctx, u.ID, "https://dead.example")
	_ = store.UpsertInstanceHeartbeat(ctx, "https://dead.example", "https://dead.example", now-int64(InstanceLivenessWindow/time.Second)-100)
	other := "https://other.example"
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, other); err != nil || url != "" {
		t.Fatalf("dead home must return empty: %q %v", url, err)
	}
}
