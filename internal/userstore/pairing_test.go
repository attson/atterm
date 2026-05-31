package userstore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCreatePairingToken_ReturnsPlaintextOnceAndStoresHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, row, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	plain := secret.Expose()
	if !strings.HasPrefix(plain, "pair_") {
		t.Fatalf("plaintext does not start with pair_: %q", plain)
	}
	if len(plain) < 30 {
		t.Fatalf("plaintext too short: %d", len(plain))
	}
	if strings.Contains(row.Hash, plain[5:]) {
		t.Fatalf("hash leaks plaintext body: %s", row.Hash)
	}
	if row.UserID != u.ID {
		t.Fatalf("UserID: got %q want %q", row.UserID, u.ID)
	}
	if row.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Fatalf("ExpiresAt too soon: %v", row.ExpiresAt)
	}
}
