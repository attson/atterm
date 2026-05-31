package userstore

import (
	"context"
	"strings"
	"sync"
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

func TestConsumePairingToken_HappyPath_MintsTokenWithSourcePairing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	pairSecret, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	apiSecret, userID, err := s.ConsumePairingToken(ctx, pairSecret.Expose())
	if err != nil {
		t.Fatalf("ConsumePairingToken: %v", err)
	}
	if userID != u.ID {
		t.Fatalf("userID: got %q want %q", userID, u.ID)
	}
	plain := apiSecret.Expose()
	if !strings.HasPrefix(plain, "atk_") {
		t.Fatalf("api token does not start with atk_: %q", plain)
	}

	// Verify source column was set to 'pairing'.
	var source string
	if err := s.db.QueryRowContext(ctx,
		`SELECT source FROM api_tokens WHERE token_hash = ?`,
		tokenHash(plain),
	).Scan(&source); err != nil {
		t.Fatalf("select source: %v", err)
	}
	if source != "pairing" {
		t.Fatalf("source: got %q want %q", source, "pairing")
	}

	// The minted token must authenticate as the original user.
	gotTokenID, gotUserID, err := s.LookupAPIToken(ctx, plain)
	if err != nil {
		t.Fatalf("LookupAPIToken: %v", err)
	}
	if gotUserID != u.ID {
		t.Fatalf("LookupAPIToken userID: got %q want %q", gotUserID, u.ID)
	}
	if gotTokenID == "" {
		t.Fatalf("LookupAPIToken: empty tokenID")
	}
}

func TestConsumePairingToken_SecondCallFails(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != ErrPairingInvalid {
		t.Fatalf("second consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, -1*time.Second)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != ErrPairingInvalid {
		t.Fatalf("expired consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_UnknownTokenString(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, _, err := s.ConsumePairingToken(ctx, "pair_NOTAREALTOKENVALUE"); err != ErrPairingInvalid {
		t.Fatalf("garbage consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_ConcurrentExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	code := pairSecret.Expose()

	const n = 50
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.ConsumePairingToken(ctx, code)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var wins, losses int
	for err := range results {
		if err == nil {
			wins++
		} else if err == ErrPairingInvalid {
			losses++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || losses != n-1 {
		t.Fatalf("wins=%d losses=%d, want 1 / %d", wins, losses, n-1)
	}
}
