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
