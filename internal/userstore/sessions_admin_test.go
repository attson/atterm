package userstore

import (
	"context"
	"testing"
	"time"
)

func TestListSessions_OrderAndFields(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u, _ := s.CreateOpaqueUser(ctx, "a@example.com")

	_, _, _ = s.CreateSession(ctx, u.ID, "ua/firefox", "203.0.113.0/24", DefaultSessionTTL)
	time.Sleep(2 * time.Millisecond) // ensure created_at ordering is deterministic
	_, _, _ = s.CreateSession(ctx, u.ID, "ua/chrome", "203.0.113.0/24", DefaultSessionTTL)

	list, err := s.ListSessions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d; want 2", len(list))
	}
	if list[0].UserAgent != "ua/chrome" || list[1].UserAgent != "ua/firefox" {
		t.Errorf("ordering: got %q,%q; want newest first (ua/chrome,ua/firefox)",
			list[0].UserAgent, list[1].UserAgent)
	}
	if list[0].IDHash == "" || list[1].IDHash == "" {
		t.Error("IDHash empty")
	}
	if list[0].IPPrefix != "203.0.113.0/24" {
		t.Errorf("IPPrefix not populated: %q", list[0].IPPrefix)
	}
	if list[0].ExpiresAt.Before(time.Now().Add(29 * 24 * time.Hour)) {
		t.Errorf("ExpiresAt too soon: %v", list[0].ExpiresAt)
	}
}

func TestListSessions_ScopedToUser(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u1, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "b@example.com")
	_, _, _ = s.CreateSession(ctx, u1.ID, "ua-u1", "1.2.3.0/24", DefaultSessionTTL)
	_, _, _ = s.CreateSession(ctx, u2.ID, "ua-u2", "5.6.7.0/24", DefaultSessionTTL)

	list1, _ := s.ListSessions(ctx, u1.ID)
	if len(list1) != 1 || list1[0].UserAgent != "ua-u1" {
		t.Errorf("u1 list leaked u2 rows or missing own: %+v", list1)
	}
}

func TestDeleteSessionByIDHash_Success(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	_, _, _ = s.CreateSession(ctx, u.ID, "ua1", "", DefaultSessionTTL)
	list, _ := s.ListSessions(ctx, u.ID)
	if len(list) != 1 {
		t.Fatalf("setup: list len %d; want 1", len(list))
	}
	deleted, err := s.DeleteSessionByIDHash(ctx, u.ID, list[0].IDHash)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("deleted=false; want true")
	}
	after, _ := s.ListSessions(ctx, u.ID)
	if len(after) != 0 {
		t.Errorf("session still listed after delete: %+v", after)
	}
}

func TestDeleteSessionByIDHash_CrossUserIsNoop(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u1, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "b@example.com")
	_, _, _ = s.CreateSession(ctx, u2.ID, "ua-u2", "", DefaultSessionTTL)
	list2, _ := s.ListSessions(ctx, u2.ID)
	// Attempt to delete u2's session as u1.
	deleted, err := s.DeleteSessionByIDHash(ctx, u1.ID, list2[0].IDHash)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Error("deleted=true for cross-user delete; want false")
	}
	after, _ := s.ListSessions(ctx, u2.ID)
	if len(after) != 1 {
		t.Errorf("u2's session was wrongly deleted: %+v", after)
	}
}

func TestDeleteSessionByIDHash_UnknownIDHashNoop(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	deleted, err := s.DeleteSessionByIDHash(ctx, u.ID, "deadbeef00")
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Error("deleted=true for unknown id_hash; want false")
	}
}

func TestDeleteOtherSessionsForUser_KeepsExceptOnly(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	_, _, _ = s.CreateSession(ctx, u.ID, "ua1", "", DefaultSessionTTL)
	_, _, _ = s.CreateSession(ctx, u.ID, "ua2", "", DefaultSessionTTL)
	_, _, _ = s.CreateSession(ctx, u.ID, "ua3", "", DefaultSessionTTL)
	list, _ := s.ListSessions(ctx, u.ID)
	if len(list) != 3 {
		t.Fatalf("setup: %d; want 3", len(list))
	}
	keep := list[1].IDHash // arbitrary middle row
	n, err := s.DeleteOtherSessionsForUser(ctx, u.ID, keep)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted=%d; want 2", n)
	}
	after, _ := s.ListSessions(ctx, u.ID)
	if len(after) != 1 || after[0].IDHash != keep {
		t.Errorf("after: %+v; want only %q", after, keep)
	}
}

func TestDeleteOtherSessionsForUser_ExceptUnknownDeletesAll(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	_, _, _ = s.CreateSession(ctx, u.ID, "ua1", "", DefaultSessionTTL)
	_, _, _ = s.CreateSession(ctx, u.ID, "ua2", "", DefaultSessionTTL)
	n, err := s.DeleteOtherSessionsForUser(ctx, u.ID, "no-such-hash")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted=%d; want 2 (unknown exceptIDHash drops nothing)", n)
	}
	after, _ := s.ListSessions(ctx, u.ID)
	if len(after) != 0 {
		t.Errorf("after: %+v; want empty", after)
	}
}
