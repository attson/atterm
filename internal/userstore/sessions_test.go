package userstore

import (
	"context"
	"testing"
	"time"
)

// createTestUser is a thin wrapper around (*DBStore).CreateOpaqueUser
// used by sessions tests. The password argument is ignored — sessions
// tests only need a user row to exist, not real credentials. Retaining
// the password parameter keeps callsites mechanical to migrate while the
// signature contract stays predictable.
func createTestUser(t *testing.T, st *DBStore, email, _ string) (*User, error) {
	t.Helper()
	return st.CreateOpaqueUser(context.Background(), email)
}

func TestLookupSession_HitReturnsUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, err := createTestUser(t, st, "alice@example.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	tok, sess, err := st.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if tok == "" {
		t.Fatal("expected plaintext session token")
	}
	got, gotUser, err := st.LookupSession(ctx, tok)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if got.IDHash != sess.IDHash {
		t.Fatalf("session mismatch: got %q want %q", got.IDHash, sess.IDHash)
	}
	if gotUser.ID != u.ID {
		t.Fatalf("user mismatch: got %q want %q", gotUser.ID, u.ID)
	}
	if got.LastSeenAt.IsZero() {
		t.Fatal("new session should have an initial last_seen_at")
	}
}

func TestTouchSession_UpdatesActivityAndReportsRevocation(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, err := createTestUser(t, st, "presence@example.com", "")
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	tok, sess, err := st.CreateSession(ctx, u.ID, "test-device", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	want := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	active, err := st.TouchSession(ctx, sess.IDHash, want)
	if err != nil || !active {
		t.Fatalf("TouchSession: active=%v err=%v", active, err)
	}
	got, _, err := st.LookupSession(ctx, tok)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if !got.LastSeenAt.Equal(want) {
		t.Fatalf("last_seen_at=%v want %v", got.LastSeenAt, want)
	}
	if err := st.DeleteSession(ctx, tok); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	active, err = st.TouchSession(ctx, sess.IDHash, time.Now())
	if err != nil {
		t.Fatalf("TouchSession revoked: %v", err)
	}
	if active {
		t.Fatal("revoked session reported active")
	}
}

func TestLookupSession_MissReturnsNotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_, _, err := st.LookupSession(ctx, "ses_garbage")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestLookupSession_ExpiredReturnsNotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, err := createTestUser(t, st, "x@example.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	tok, _, _ := st.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", -1*time.Second)
	if _, _, err := st.LookupSession(ctx, tok); err == nil {
		t.Fatal("expected error for expired session")
	}
}
