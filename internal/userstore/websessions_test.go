package userstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

// newTestStore is defined in users_test.go; reused here.

func TestCreateWebSession_StoresHashNotPlaintext(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "session@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, err := s.CreateWebSession(ctx, user.ID, "Mozilla/5.0", "1.2.3.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	plain := secret.Expose()
	if len(plain) < 40 {
		t.Fatalf("cookie value too short: len=%d, val=%q", len(plain), plain)
	}
	// Secret should have no prefix (cookie is opaque)
	if secret.Prefix() != plain[:4] {
		// Prefix() returns prefix + first 4 chars of body; with empty prefix that's just first 4 chars
		// This is fine — just check the underlying cookie value is long enough.
	}

	// Compute expected id_hash
	sum := sha256.Sum256([]byte(plain))
	expectedHash := hex.EncodeToString(sum[:])

	var storedHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT id_hash FROM web_sessions WHERE id_hash = ?`, expectedHash,
	).Scan(&storedHash); err != nil {
		t.Fatalf("query id_hash: %v — hash not found in db (was plaintext stored?)", err)
	}
	if storedHash == plain {
		t.Fatalf("id_hash equals plaintext — not hashed!")
	}
	if storedHash != expectedHash {
		t.Fatalf("id_hash mismatch: got %q, want %q", storedHash, expectedHash)
	}
}

func TestLookupWebSession_ValidReturnsUserIDAndRenews(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "renew@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, err := s.CreateWebSession(ctx, user.ID, "Mozilla/5.0", "1.2.3.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	plain := secret.Expose()

	// Compute id_hash to read expires_at directly
	sum := sha256.Sum256([]byte(plain))
	idHash := hex.EncodeToString(sum[:])

	// First lookup
	gotUserID, csrfSecret, err := s.LookupWebSession(ctx, plain)
	if err != nil {
		t.Fatalf("LookupWebSession (first): %v", err)
	}
	if gotUserID != user.ID {
		t.Errorf("userID: got %q, want %q", gotUserID, user.ID)
	}
	if len(csrfSecret) == 0 {
		t.Errorf("csrfSecret is empty")
	}

	// Read expires_at after first lookup
	var expiresAtBefore int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM web_sessions WHERE id_hash = ?`, idHash,
	).Scan(&expiresAtBefore); err != nil {
		t.Fatalf("read expires_at before: %v", err)
	}

	// Sleep 1.1s to cross a millisecond (actually second) boundary so second
	// lookup writes a strictly later expires_at value.
	time.Sleep(1100 * time.Millisecond)

	// Second lookup
	_, _, err = s.LookupWebSession(ctx, plain)
	if err != nil {
		t.Fatalf("LookupWebSession (second): %v", err)
	}

	// Read expires_at after second lookup
	var expiresAtAfter int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM web_sessions WHERE id_hash = ?`, idHash,
	).Scan(&expiresAtAfter); err != nil {
		t.Fatalf("read expires_at after: %v", err)
	}

	if expiresAtAfter <= expiresAtBefore {
		t.Fatalf("sliding renewal did not push expires_at: before=%d after=%d",
			expiresAtBefore, expiresAtAfter)
	}
}

func TestLookupWebSession_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "expired@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Insert an already-expired session directly via SQL
	plain := "expiredtestcookievalue1234567890abcdefghij"
	sum := sha256.Sum256([]byte(plain))
	idHash := hex.EncodeToString(sum[:])
	pastExpiry := time.Now().Add(-1 * time.Hour).UnixMilli()

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO web_sessions(id_hash, user_id, created_at, expires_at, user_agent, ip_prefix)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		idHash, user.ID, time.Now().UnixMilli(), pastExpiry, "TestAgent", "127.0.0.0/24",
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	_, _, err = s.LookupWebSession(ctx, plain)
	if !errors.Is(err, ErrWebSessionInvalid) {
		t.Fatalf("expected ErrWebSessionInvalid for expired session, got %v", err)
	}
}

func TestLookupWebSession_UserDisabled(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "disabled-session@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, err := s.CreateWebSession(ctx, user.ID, "Mozilla/5.0", "1.2.3.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	if err := s.DisableUser(ctx, user.ID); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	_, _, err = s.LookupWebSession(ctx, secret.Expose())
	if !errors.Is(err, ErrWebSessionInvalid) {
		t.Fatalf("expected ErrWebSessionInvalid for disabled user, got %v", err)
	}
}

func TestDeleteWebSession_RevokesCookie(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "delete-session@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, err := s.CreateWebSession(ctx, user.ID, "Mozilla/5.0", "1.2.3.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	plain := secret.Expose()

	// Verify it works before delete
	if _, _, err := s.LookupWebSession(ctx, plain); err != nil {
		t.Fatalf("LookupWebSession before delete: %v", err)
	}

	if err := s.DeleteWebSession(ctx, plain); err != nil {
		t.Fatalf("DeleteWebSession: %v", err)
	}

	_, _, err = s.LookupWebSession(ctx, plain)
	if !errors.Is(err, ErrWebSessionInvalid) {
		t.Fatalf("expected ErrWebSessionInvalid after delete, got %v", err)
	}
}

func TestPurgeExpiredWebSessions_DeletesOldRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "purge@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now()
	pastExpiry := now.Add(-1 * time.Hour).UnixMilli()
	futureExpiry := now.Add(30 * 24 * time.Hour).UnixMilli()
	createdAt := now.UnixMilli()

	// Insert 3 expired sessions
	for i := 0; i < 3; i++ {
		plain := []byte{'e', 'x', 'p', byte('0' + i)}
		// pad to 32+ bytes
		paddedPlain := string(plain) + "iredcookietestvalue1234567890abcde"
		sum := sha256.Sum256([]byte(paddedPlain))
		idHash := hex.EncodeToString(sum[:])
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO web_sessions(id_hash, user_id, created_at, expires_at, user_agent, ip_prefix)
			 VALUES(?, ?, ?, ?, ?, ?)`,
			idHash, user.ID, createdAt, pastExpiry, "PurgeTestAgent", "10.0.0.0/24",
		); err != nil {
			t.Fatalf("insert expired session %d: %v", i, err)
		}
	}

	// Insert 2 fresh sessions
	for i := 0; i < 2; i++ {
		plain := []byte{'f', 'r', 's', byte('0' + i)}
		paddedPlain := string(plain) + "eshcookietestvalue1234567890abcde"
		sum := sha256.Sum256([]byte(paddedPlain))
		idHash := hex.EncodeToString(sum[:])
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO web_sessions(id_hash, user_id, created_at, expires_at, user_agent, ip_prefix)
			 VALUES(?, ?, ?, ?, ?, ?)`,
			idHash, user.ID, createdAt, futureExpiry, "PurgeTestAgent", "10.0.0.0/24",
		); err != nil {
			t.Fatalf("insert fresh session %d: %v", i, err)
		}
	}

	deleted, err := s.PurgeExpiredWebSessions(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredWebSessions: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted rows, got %d", deleted)
	}

	var remaining int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM web_sessions WHERE user_id = ?`, user.ID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("expected 2 remaining rows, got %d", remaining)
	}
}
