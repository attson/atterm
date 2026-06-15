package main

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// TestBootstrapAdmin_GeneratesClaimToken verifies that calling
// bootstrapAdmin with a non-empty email inserts exactly one un-consumed
// claim token bound to that email + the "admin" role. Reaching directly
// into the underlying SQLite handle (via DB()) keeps the assertion
// independent of the printed plaintext, which we deliberately don't
// capture from log output.
func TestBootstrapAdmin_GeneratesClaimToken(t *testing.T) {
	store := userstore.NewInMemory(t)
	ctx := context.Background()

	if err := bootstrapAdmin(ctx, store, "admin@example.com"); err != nil {
		t.Fatalf("bootstrapAdmin: %v", err)
	}

	var (
		count int
		email string
		role  string
	)
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM claim_tokens WHERE consumed_at IS NULL`,
	).Scan(&count); err != nil {
		t.Fatalf("count claim_tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 un-consumed claim_token, got %d", count)
	}

	if err := store.DB().QueryRowContext(ctx,
		`SELECT email, role FROM claim_tokens LIMIT 1`,
	).Scan(&email, &role); err != nil {
		t.Fatalf("select claim_token: %v", err)
	}
	if email != "admin@example.com" {
		t.Fatalf("email: got %q want admin@example.com", email)
	}
	if role != "admin" {
		t.Fatalf("role: got %q want admin", role)
	}
}

// TestBootstrapAdmin_EmptyEmail_NoOp verifies the early-return path: a
// relay booted without ATTERM_BOOTSTRAP_ADMIN_EMAIL set must not error
// and must not insert any token rows.
func TestBootstrapAdmin_EmptyEmail_NoOp(t *testing.T) {
	store := userstore.NewInMemory(t)
	ctx := context.Background()

	if err := bootstrapAdmin(ctx, store, ""); err != nil {
		t.Fatalf("expected no error on empty email, got %v", err)
	}

	var count int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM claim_tokens`,
	).Scan(&count); err != nil {
		t.Fatalf("count claim_tokens: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 claim_tokens after empty-email bootstrap, got %d", count)
	}
}
