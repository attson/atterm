package userstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreatePairingToken_ReturnsPlaintextOnceAndStoresHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
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

func TestConsumePairingToken_HappyPath_ReturnsUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	pairSecret, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	got, err := s.ConsumePairingToken(ctx, pairSecret.Expose())
	if err != nil {
		t.Fatalf("ConsumePairingToken: %v", err)
	}
	if got == nil {
		t.Fatal("ConsumePairingToken returned nil user")
	}
	if got.ID != u.ID {
		t.Fatalf("userID: got %q want %q", got.ID, u.ID)
	}
	if got.Email != u.Email {
		t.Fatalf("email: got %q want %q", got.Email, u.Email)
	}
}

func TestConsumePairingToken_SecondCallFails(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "alice@example.com")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)

	if _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); !errors.Is(err, ErrPairingConsumed) {
		t.Fatalf("second consume: got %v want ErrPairingConsumed", err)
	}
}

func TestConsumePairingToken_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "alice@example.com")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, -1*time.Second)

	if _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("expired consume: got %v want ErrPairingExpired", err)
	}
}

func TestConsumePairingToken_UnknownTokenString(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.ConsumePairingToken(ctx, "pair_NOTAREALTOKENVALUE"); !errors.Is(err, ErrPairingNotFound) {
		t.Fatalf("garbage consume: got %v want ErrPairingNotFound", err)
	}
}

func TestConsumePairingToken_ConcurrentExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "alice@example.com")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	code := pairSecret.Expose()

	const n = 50
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ConsumePairingToken(ctx, code)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var wins, losses int
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrPairingConsumed) {
			losses++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || losses != n-1 {
		t.Fatalf("wins=%d losses=%d, want 1 / %d", wins, losses, n-1)
	}
}
