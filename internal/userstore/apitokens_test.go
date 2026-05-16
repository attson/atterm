package userstore

import (
	"context"
	"strings"
	"testing"
)

// newTestStore is defined in users_test.go; reused here.

func TestCreateAPIToken_ReturnsPlaintextOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "tokenowner@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, tok, err := s.CreateAPIToken(ctx, user.ID, "MacBook Air")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	plain := secret.Expose()
	if !strings.HasPrefix(plain, "atk_") {
		t.Fatalf("plaintext does not start with atk_: %q", plain)
	}
	if len(plain) < 40 {
		t.Fatalf("plaintext too short: len=%d, val=%q", len(plain), plain)
	}

	// token_hash must not contain the plaintext body
	body := plain[len("atk_"):]
	if strings.Contains(tok.Prefix, body) {
		t.Fatalf("Prefix leaks body: %s", tok.Prefix)
	}

	// Verify hash is not the plaintext by querying the DB directly
	var storedHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT token_hash FROM api_tokens WHERE id = ?`, tok.ID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("query token_hash: %v", err)
	}
	if storedHash == plain {
		t.Fatalf("token_hash equals plaintext — not hashed!")
	}
	if strings.Contains(storedHash, body) {
		t.Fatalf("token_hash contains plaintext body: %s", storedHash)
	}
}

func TestLookupAPIToken_ValidReturnsUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "lookup@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, tok, err := s.CreateAPIToken(ctx, user.ID, "Test Device")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	tokenID, userID, err := s.LookupAPIToken(ctx, secret.Expose())
	if err != nil {
		t.Fatalf("LookupAPIToken: %v", err)
	}
	if tokenID != tok.ID {
		t.Errorf("tokenID: got %q, want %q", tokenID, tok.ID)
	}
	if userID != user.ID {
		t.Errorf("userID: got %q, want %q", userID, user.ID)
	}
}

func TestLookupAPIToken_RevokedRejected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "revoke@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, tok, err := s.CreateAPIToken(ctx, user.ID, "To Revoke")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if err := s.RevokeAPIToken(ctx, tok.ID, user.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	_, _, err = s.LookupAPIToken(ctx, secret.Expose())
	if err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid after revoke, got %v", err)
	}
}

func TestLookupAPIToken_UserDisabled(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "disabled@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, _, err := s.CreateAPIToken(ctx, user.ID, "Disabled Owner")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if err := s.DisableUser(ctx, user.ID); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	_, _, err = s.LookupAPIToken(ctx, secret.Expose())
	if err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid for disabled user, got %v", err)
	}
}

func TestLookupAPIToken_BadPlaintext(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, _, err := s.LookupAPIToken(ctx, "atk_totalgibberish1234567890")
	if err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid for bad plaintext, got %v", err)
	}
}

func TestListAPITokens_HidesPlaintext(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "list@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, _, err = s.CreateAPIToken(ctx, user.ID, "Device A")
	if err != nil {
		t.Fatalf("CreateAPIToken A: %v", err)
	}
	_, _, err = s.CreateAPIToken(ctx, user.ID, "Device B")
	if err != nil {
		t.Fatalf("CreateAPIToken B: %v", err)
	}

	tokens, err := s.ListAPITokens(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAPITokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}

	for _, tok := range tokens {
		// Prefix must be "atk_" + 8 chars = 12 chars total
		if len(tok.Prefix) != 12 {
			t.Errorf("Prefix length: got %d, want 12 (tok.Prefix=%q)", len(tok.Prefix), tok.Prefix)
		}
		if !strings.HasPrefix(tok.Prefix, "atk_") {
			t.Errorf("Prefix does not start with atk_: %q", tok.Prefix)
		}
		// APIToken struct has no plaintext field — this is compile-time enforced.
		// Runtime check: Name is set, ID is set, no raw token body exposed.
		if tok.ID == "" {
			t.Errorf("tok.ID is empty")
		}
		if tok.Name == "" {
			t.Errorf("tok.Name is empty")
		}
	}
}

func TestRevokeAPIToken_WrongUserRejected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	owner, err := s.CreateUser(ctx, "owner@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	other, err := s.CreateUser(ctx, "other@example.com", "supersecretpassword")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}

	_, tok, err := s.CreateAPIToken(ctx, owner.ID, "Owner Token")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	// other user tries to revoke owner's token — must fail
	if err := s.RevokeAPIToken(ctx, tok.ID, other.ID); err == nil {
		t.Fatal("expected error when revoking another user's token, got nil")
	}
}
