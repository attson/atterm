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

// TestListUsers_OrderedByCreatedAt creates 3 users with a small delay between
// each and asserts ListUsers returns them newest-first.
func TestListUsers_OrderedByCreatedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create three users; sleep 1ms between each so created_at differs.
	emails := []string{"first@example.com", "second@example.com", "third@example.com"}
	for i, email := range emails {
		if _, err := s.CreateUser(ctx, email, "correcthorsebatterystaple"); err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
		// Bump the stored created_at by 1 second via a direct UPDATE so SQLite
		// integer timestamps differ (time.Now() resolution may collapse to same second).
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET created_at = created_at + ? WHERE email = ?`,
			i, email); err != nil {
			t.Fatalf("bump created_at: %v", err)
		}
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	// newest-first: third, second, first
	wantOrder := []string{"third@example.com", "second@example.com", "first@example.com"}
	for i, want := range wantOrder {
		if users[i].Email != want {
			t.Errorf("users[%d].Email = %q, want %q", i, users[i].Email, want)
		}
	}
}

// TestResetUserPassword verifies the method generates a tmp_ password, updates
// the hash, rotates csrf_secret, and purges web_sessions for that user.
func TestResetUserPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "alice@example.com", "oldpassword12345")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create a web session for this user.
	_, err = s.CreateWebSession(ctx, u.ID, "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	// Capture csrf_secret before reset.
	var csrfBefore []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&csrfBefore); err != nil {
		t.Fatalf("scan csrf_secret before: %v", err)
	}

	// Count sessions before.
	var sessionsBefore int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM web_sessions WHERE user_id=?`, u.ID).Scan(&sessionsBefore); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionsBefore != 1 {
		t.Fatalf("expected 1 session before, got %d", sessionsBefore)
	}

	secret, err := s.ResetUserPassword(ctx, u.ID)
	if err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	// Plaintext must start with "tmp_".
	if !strings.HasPrefix(secret.Expose(), "tmp_") {
		t.Fatalf("expected tmp_ prefix, got %q", secret.Expose())
	}

	// The new password must verify.
	verified, err := s.VerifyPassword(ctx, "alice@example.com", secret.Expose())
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if verified == nil {
		t.Fatal("new tmp password did not verify")
	}

	// Old password must no longer verify.
	old, err := s.VerifyPassword(ctx, "alice@example.com", "oldpassword12345")
	if err != nil {
		t.Fatalf("VerifyPassword old: %v", err)
	}
	if old != nil {
		t.Fatal("old password still verifies after reset")
	}

	// csrf_secret must be rotated (different from before).
	var csrfAfter []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&csrfAfter); err != nil {
		t.Fatalf("scan csrf_secret after: %v", err)
	}
	if string(csrfBefore) == string(csrfAfter) {
		t.Fatal("csrf_secret was not rotated by ResetUserPassword")
	}

	// Web sessions must be purged.
	var sessionsAfter int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM web_sessions WHERE user_id=?`, u.ID).Scan(&sessionsAfter); err != nil {
		t.Fatalf("count sessions after: %v", err)
	}
	if sessionsAfter != 0 {
		t.Fatalf("expected 0 sessions after reset, got %d", sessionsAfter)
	}
}

func TestGetUser_DefaultsToNonAdmin(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsAdmin {
		t.Errorf("freshly created user IsAdmin = true; want false (default)")
	}
}

func TestSetIsAdminColumnRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	if err != nil {
		t.Fatal(err)
	}
	// Direct SQL to bypass not-yet-existing SetUserAdmin.
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE users SET is_admin = 1 WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsAdmin {
		t.Errorf("after UPDATE is_admin=1, GetUser returned IsAdmin=false")
	}
	v, err := s.VerifyPassword(ctx, "a@example.com", "passphrase-1234")
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || !v.IsAdmin {
		t.Errorf("VerifyPassword returned IsAdmin=false after promotion")
	}
}
