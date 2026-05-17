package userstore

import (
	"context"
	"testing"
	"time"
)

func TestListUserWebSessions_OrderAndFields(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")

	s1, _ := s.CreateWebSession(ctx, u.ID, "ua/firefox", "203.0.113.0/24")
	time.Sleep(2 * time.Millisecond) // ensure created_at ordering is deterministic
	s2, _ := s.CreateWebSession(ctx, u.ID, "ua/chrome", "203.0.113.0/24")

	list, err := s.ListUserWebSessions(ctx, u.ID)
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
	_ = s1
	_ = s2
}

func TestListUserWebSessions_ScopedToUser(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u1, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	u2, _ := s.CreateUser(ctx, "b@example.com", "passphrase-1234")
	_, _ = s.CreateWebSession(ctx, u1.ID, "ua-u1", "1.2.3.0/24")
	_, _ = s.CreateWebSession(ctx, u2.ID, "ua-u2", "5.6.7.0/24")

	list1, _ := s.ListUserWebSessions(ctx, u1.ID)
	if len(list1) != 1 || list1[0].UserAgent != "ua-u1" {
		t.Errorf("u1 list leaked u2 rows or missing own: %+v", list1)
	}
}

func TestDeleteUserWebSessionByIDHash_Success(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_, _ = s.CreateWebSession(ctx, u.ID, "ua1", "")
	list, _ := s.ListUserWebSessions(ctx, u.ID)
	if len(list) != 1 {
		t.Fatalf("setup: list len %d; want 1", len(list))
	}
	deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u.ID, list[0].IDHash)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("deleted=false; want true")
	}
	after, _ := s.ListUserWebSessions(ctx, u.ID)
	if len(after) != 0 {
		t.Errorf("session still listed after delete: %+v", after)
	}
}

func TestDeleteUserWebSessionByIDHash_CrossUserIsNoop(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u1, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	u2, _ := s.CreateUser(ctx, "b@example.com", "passphrase-1234")
	_, _ = s.CreateWebSession(ctx, u2.ID, "ua-u2", "")
	list2, _ := s.ListUserWebSessions(ctx, u2.ID)
	// Attempt to delete u2's session as u1.
	deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u1.ID, list2[0].IDHash)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Error("deleted=true for cross-user delete; want false")
	}
	after, _ := s.ListUserWebSessions(ctx, u2.ID)
	if len(after) != 1 {
		t.Errorf("u2's session was wrongly deleted: %+v", after)
	}
}

func TestDeleteUserWebSessionByIDHash_UnknownIDHashNoop(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
	deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u.ID, "deadbeef00")
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Error("deleted=true for unknown id_hash; want false")
	}
}
