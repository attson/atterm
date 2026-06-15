package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClaimToken_CreateLookupConsume(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	plaintext, err := store.CreateClaimToken(ctx, "admin@example.com", "admin", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plaintext == "" {
		t.Fatalf("empty plaintext")
	}
	if !strings.HasPrefix(plaintext, "claim_") {
		t.Fatalf("plaintext does not start with claim_: %q", plaintext)
	}

	tok, err := store.LookupClaimToken(ctx, plaintext)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if tok.Email != "admin@example.com" || tok.Role != "admin" {
		t.Fatalf("unexpected: %+v", tok)
	}

	if err := store.ConsumeClaimToken(ctx, plaintext); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	// Second consume must fail.
	if err := store.ConsumeClaimToken(ctx, plaintext); !errors.Is(err, ErrClaimTokenConsumed) {
		t.Fatalf("expected ErrClaimTokenConsumed, got %v", err)
	}
	// Lookup after consume must show consumed.
	if _, err := store.LookupClaimToken(ctx, plaintext); !errors.Is(err, ErrClaimTokenConsumed) {
		t.Fatalf("expected ErrClaimTokenConsumed on lookup, got %v", err)
	}
}

func TestClaimToken_NotFound(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.LookupClaimToken(context.Background(), "bogus"); !errors.Is(err, ErrClaimTokenNotFound) {
		t.Fatalf("expected ErrClaimTokenNotFound, got %v", err)
	}
}

func TestClaimToken_Expired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	plaintext, err := store.CreateClaimToken(ctx, "admin@example.com", "admin", -1*time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.LookupClaimToken(ctx, plaintext); !errors.Is(err, ErrClaimTokenExpired) {
		t.Fatalf("expected ErrClaimTokenExpired, got %v", err)
	}
}
