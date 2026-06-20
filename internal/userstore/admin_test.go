package userstore

import (
	"context"
	"testing"
)

func TestAdminExists(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if ok, err := s.AdminExists(ctx); err != nil || ok {
		t.Fatalf("empty store: want (false,nil), got (%v,%v)", ok, err)
	}
	u, _ := s.CreateOpaqueUser(ctx, "boot@example.com")
	if ok, _ := s.AdminExists(ctx); ok {
		t.Fatal("non-admin user must not count as admin")
	}
	if err := s.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.AdminExists(ctx); err != nil || !ok {
		t.Fatalf("after promote: want (true,nil), got (%v,%v)", ok, err)
	}
	// Disabling the only admin makes AdminExists false again.
	if err := s.DisableUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.AdminExists(ctx); ok {
		t.Fatal("disabled admin must not count")
	}
}

func TestSetUserAdmin_Toggle(t *testing.T) {
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
