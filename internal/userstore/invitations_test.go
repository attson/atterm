package userstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateInvitation_ReturnsPlaintextOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	secret, inv, err := s.CreateInvitation(ctx, nil, "bob")
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	plain := secret.Expose()
	if !strings.HasPrefix(plain, "inv_") {
		t.Fatalf("secret does not start with inv_: %q", plain)
	}
	if len(plain) < 26 {
		t.Fatalf("secret too short: len=%d, val=%q", len(plain), plain)
	}

	// code_hash must not contain the plaintext body
	if strings.Contains(inv.CodeHash, plain[4:]) {
		t.Fatalf("CodeHash leaks plaintext body: %s", inv.CodeHash)
	}
	if inv.CodePrefix == "" {
		t.Fatalf("CodePrefix is empty")
	}
	if inv.Note != "bob" {
		t.Fatalf("Note: got %q, want %q", inv.Note, "bob")
	}
	if inv.ExpiresAt != nil {
		t.Fatalf("ExpiresAt should be nil, got %v", inv.ExpiresAt)
	}
}

func TestConsumeInvitation_OneShotUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create a real user for the FK constraint on consumed_by.
	user, err := s.CreateOpaqueUser(ctx, "winner@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, _, err := s.CreateInvitation(ctx, nil, "concurrency-test")
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	code := secret.Expose()

	const n = 50
	var wg sync.WaitGroup
	results := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.ConsumeInvitation(ctx, code, user.ID)
		}()
	}
	wg.Wait()
	close(results)

	var wins, losses int
	for err := range results {
		if err == nil {
			wins++
		} else if err == ErrInviteInvalid {
			losses++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", wins)
	}
	if losses != n-1 {
		t.Fatalf("expected %d losers, got %d", n-1, losses)
	}

	// consumed_at must be non-NULL
	var consumedAt *int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT consumed_at FROM invitations WHERE code_hash IN (
			SELECT code_hash FROM invitations WHERE consumed_by = ?
		)`, user.ID,
	).Scan(&consumedAt); err != nil {
		t.Fatalf("query consumed_at: %v", err)
	}
	if consumedAt == nil {
		t.Fatal("consumed_at is NULL after successful consume")
	}
}

func TestConsumeInvitation_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	expiresAt := time.Now().Add(-time.Hour)
	secret, _, err := s.CreateInvitation(ctx, &expiresAt, "expired-invite")
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	user, err := s.CreateOpaqueUser(ctx, "expiry@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.ConsumeInvitation(ctx, secret.Expose(), user.ID); err != ErrInviteInvalid {
		t.Fatalf("expected ErrInviteInvalid for expired invite, got %v", err)
	}

	// consumed_at must remain NULL
	var consumedAt *int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT consumed_at FROM invitations WHERE code_hash = ?`,
		inviteHash(secret.Expose()),
	).Scan(&consumedAt); err != nil {
		t.Fatalf("query consumed_at: %v", err)
	}
	if consumedAt != nil {
		t.Fatal("consumed_at was set despite expired invite")
	}
}

func TestConsumeInvitation_BadCode(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.ConsumeInvitation(ctx, "inv_totalgibberish1234567890", "any-user-id")
	if err != ErrInviteInvalid {
		t.Fatalf("expected ErrInviteInvalid for bad code, got %v", err)
	}
}

func TestListInvitations_AdminView(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create a real user for consuming.
	user, err := s.CreateOpaqueUser(ctx, "list@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 1. Open invitation
	_, _, err = s.CreateInvitation(ctx, nil, "open")
	if err != nil {
		t.Fatalf("CreateInvitation open: %v", err)
	}

	// 2. Expired invitation
	expiresAt := time.Now().Add(-time.Hour)
	_, _, err = s.CreateInvitation(ctx, &expiresAt, "expired")
	if err != nil {
		t.Fatalf("CreateInvitation expired: %v", err)
	}

	// 3. Consumed invitation
	secret3, _, err := s.CreateInvitation(ctx, nil, "consumed")
	if err != nil {
		t.Fatalf("CreateInvitation consumed: %v", err)
	}
	if err := s.ConsumeInvitation(ctx, secret3.Expose(), user.ID); err != nil {
		t.Fatalf("ConsumeInvitation: %v", err)
	}

	list, err := s.ListInvitations(ctx)
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 invitations, got %d", len(list))
	}

	var openCount, expiredCount, consumedCount int
	for _, inv := range list {
		switch {
		case inv.ConsumedAt != nil:
			consumedCount++
			if inv.ConsumedBy != user.ID {
				t.Errorf("ConsumedBy: got %q, want %q", inv.ConsumedBy, user.ID)
			}
		case inv.ExpiresAt != nil && inv.ExpiresAt.Before(time.Now()):
			expiredCount++
		default:
			openCount++
		}
	}

	if openCount != 1 {
		t.Errorf("open invitations: got %d, want 1", openCount)
	}
	if expiredCount != 1 {
		t.Errorf("expired invitations: got %d, want 1", expiredCount)
	}
	if consumedCount != 1 {
		t.Errorf("consumed invitations: got %d, want 1", consumedCount)
	}

	// Smoke-check notes are preserved
	notes := make(map[string]bool)
	for _, inv := range list {
		notes[inv.Note] = true
	}
	for _, want := range []string{"open", "expired", "consumed"} {
		if !notes[want] {
			t.Errorf("note %q missing from ListInvitations result", want)
		}
	}

	_ = fmt.Sprintf // suppress unused import if needed
}
