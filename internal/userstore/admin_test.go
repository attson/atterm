package userstore

import (
	"context"
	"testing"
)

func TestSetUserAdmin_Toggle(t *testing.T) {
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

	if err := s.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetUser(ctx, u.ID)
	if !got.IsAdmin {
		t.Fatal("after SetUserAdmin(true) IsAdmin still false")
	}

	if err := s.SetUserAdmin(ctx, u.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetUser(ctx, u.ID)
	if got.IsAdmin {
		t.Fatal("after SetUserAdmin(false) IsAdmin still true")
	}
}

func TestSetUserAdmin_UnknownUserIsNoop(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Unknown id; should not return an error (UPDATE matches 0 rows).
	if err := s.SetUserAdmin(ctx, "nope", true); err != nil {
		t.Fatalf("SetUserAdmin on missing id: %v", err)
	}
}

func TestEnsureAdminUser_ExistingUser_PromotesAndIgnoresPassword(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u, err := s.CreateUser(ctx, "a@example.com", "original-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Fatal("freshly created user already admin")
	}

	created, err := s.EnsureAdminUser(ctx, "a@example.com", "this-should-be-ignored")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("created=true for existing user; want false")
	}

	got, _ := s.GetUser(ctx, u.ID)
	if !got.IsAdmin {
		t.Error("existing user not promoted after EnsureAdminUser")
	}

	// Original password still works (password arg was ignored).
	v, _ := s.VerifyPassword(ctx, "a@example.com", "original-passphrase")
	if v == nil {
		t.Error("original password no longer verifies (EnsureAdminUser must not touch it)")
	}
	if v2, _ := s.VerifyPassword(ctx, "a@example.com", "this-should-be-ignored"); v2 != nil {
		t.Error("EnsureAdminUser silently changed the password")
	}
}
