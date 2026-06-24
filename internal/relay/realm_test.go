package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestLoadOrInitRealm(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// First boot generates a realm.
	r1, err := LoadOrInitRealm(ctx, store, "")
	if err != nil || r1 == "" {
		t.Fatalf("first init: %v %q", err, r1)
	}
	// Idempotent: second call returns the same realm.
	r2, err := LoadOrInitRealm(ctx, store, "")
	if err != nil || r2 != r1 {
		t.Fatalf("idempotent: %v %q != %q", err, r2, r1)
	}
	// Matching env pin is accepted.
	r3, err := LoadOrInitRealm(ctx, store, r1)
	if err != nil || r3 != r1 {
		t.Fatalf("matching env: %v %q", err, r3)
	}
	// Conflicting env pin is a hard error.
	_, err = LoadOrInitRealm(ctx, store, "some-other-realm")
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting env: want conflict error, got %v", err)
	}
}

func TestLoadOrInitRealm_EnvPinFreshBoot(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	r, err := LoadOrInitRealm(ctx, store, "pinned-realm")
	if err != nil || r != "pinned-realm" {
		t.Fatalf("env pin on fresh boot: %v %q", err, r)
	}
}
