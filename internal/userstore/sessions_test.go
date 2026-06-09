package userstore

import (
	"context"
	"testing"
	"time"
)

// createTestUser is a thin wrapper around (*SQLiteStore).CreateUser used by
// sessions tests. It matches the helper-shape the tests in this file expect.
func createTestUser(t *testing.T, st *SQLiteStore, email, password string) (*User, error) {
	t.Helper()
	return st.CreateUser(context.Background(), email, password)
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
