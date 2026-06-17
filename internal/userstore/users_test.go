package userstore

import (
	"context"
	"errors"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:", WithSecretCipher(testCipher))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateOpaqueUser_LowercasesEmail(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateOpaqueUser(ctx, "Alice@Example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	if u.ID == "" || len(u.ID) != 26 {
		t.Fatalf("user id not ULID: %q", u.ID)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email not lowercased: %q", u.Email)
	}
	if u.AuthMode != "opaque" {
		t.Fatalf("auth_mode = %q; want \"opaque\"", u.AuthMode)
	}
}

func TestCreateOpaqueUser_DuplicateEmailReturnsErr(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreateOpaqueUser(ctx, "a@b.com"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateOpaqueUser(ctx, "A@B.com")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// TestListUsers_OrderedByCreatedAt creates 3 users and asserts ListUsers
// returns them newest-first.
func TestListUsers_OrderedByCreatedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	emails := []string{"first@example.com", "second@example.com", "third@example.com"}
	for i, email := range emails {
		if _, err := s.CreateOpaqueUser(ctx, email); err != nil {
			t.Fatalf("CreateOpaqueUser %d: %v", i, err)
		}
		// Bump the stored created_at by i seconds via a direct UPDATE so SQLite
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

func TestGetUser_DefaultsToNonAdmin(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateOpaqueUser(ctx, "a@example.com")
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
	u, err := s.CreateOpaqueUser(ctx, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
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
}
