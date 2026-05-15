package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateUser_HashesPasswordAndStoresCSRFSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "Alice@Example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" || len(u.ID) != 26 {
		t.Fatalf("user id not ULID: %q", u.ID)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email not lowercased: %q", u.Email)
	}

	// password_hash must not contain plaintext substring
	var hash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM users WHERE id=?`, u.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correcthorse") {
		t.Fatalf("password_hash leaks plaintext: %s", hash)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("password_hash not argon2id: %s", hash)
	}

	// csrf_secret must be 32 random bytes
	var secret []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("csrf_secret length: got %d want 32", len(secret))
	}
}

func TestCreateUser_DuplicateEmailReturnsErr(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreateUser(ctx, "a@b.com", "pw-correcthorsestaple"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateUser(ctx, "A@B.com", "pw-correcthorsestaple")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestVerifyPassword_Success(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pw := "correcthorsebatterystaple"
	u, err := s.CreateUser(ctx, "a@b.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := s.VerifyPassword(ctx, "a@b.com", pw)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if verified == nil || verified.ID != u.ID {
		t.Fatalf("VerifyPassword returned wrong user: %+v", verified)
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, _ = s.CreateUser(ctx, "a@b.com", "correcthorsebatterystaple")
	verified, err := s.VerifyPassword(ctx, "a@b.com", "wrong-password-attempt")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if verified != nil {
		t.Fatalf("VerifyPassword should return nil user on wrong pw, got %+v", verified)
	}
}

func TestVerifyPassword_MissingEmailRunsArgon2(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// No user inserted. Should still run argon2id work (verified=nil, err=nil).
	verified, err := s.VerifyPassword(ctx, "nobody@example.com", "any")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if verified != nil {
		t.Fatalf("missing-email VerifyPassword should return nil, got %+v", verified)
	}
}
