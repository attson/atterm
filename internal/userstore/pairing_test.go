package userstore

import (
	"bytes"
	"context"
	"database/sql"
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

	secret, row, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)
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
	pairSecret, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	got, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose())
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
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); !errors.Is(err, ErrPairingConsumed) {
		t.Fatalf("second consume: got %v want ErrPairingConsumed", err)
	}
}

func TestConsumePairingToken_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "alice@example.com")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, -1*time.Second, nil)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("expired consume: got %v want ErrPairingExpired", err)
	}
}

func TestConsumePairingToken_UnknownTokenString(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, _, err := s.ConsumePairingToken(ctx, "pair_NOTAREALTOKENVALUE"); !errors.Is(err, ErrPairingNotFound) {
		t.Fatalf("garbage consume: got %v want ErrPairingNotFound", err)
	}
}

func TestConsumePairingToken_ConcurrentExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "alice@example.com")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)
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

func TestPairingWrap_ColumnExists(t *testing.T) {
	s := newTestStore(t)
	var name string
	err := s.db.QueryRow("SELECT name FROM pragma_table_info('pairing_tokens') WHERE name = 'wrapped_account_key'").Scan(&name)
	if err != nil {
		t.Fatalf("column not present: %v", err)
	}
	if name != "wrapped_account_key" {
		t.Fatalf("got %q, want wrapped_account_key", name)
	}
}

func TestCreatePairingToken_StoresWrap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	wrap := []byte{0x01, 0x02, 0x03, 0x04}
	_, row, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, wrap)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	var got []byte
	if err := s.db.QueryRow("SELECT wrapped_account_key FROM pairing_tokens WHERE token_hash = ?", row.Hash).Scan(&got); err != nil {
		t.Fatalf("read wrap: %v", err)
	}
	if !bytes.Equal(got, wrap) {
		t.Fatalf("wrap: got %x, want %x", got, wrap)
	}
}

func TestCreatePairingToken_NilWrap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	_, row, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	var got sql.NullString
	if err := s.db.QueryRow("SELECT wrapped_account_key FROM pairing_tokens WHERE token_hash = ?", row.Hash).Scan(&got); err != nil {
		t.Fatalf("read wrap: %v", err)
	}
	if got.Valid {
		t.Fatalf("expected NULL wrap, got %q", got.String)
	}
}

func TestConsumePairingToken_ReturnsWrap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	wrap := []byte("wrap-bytes")
	sec, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, wrap)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	_, got, err := s.ConsumePairingToken(ctx, sec.Expose())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !bytes.Equal(got, wrap) {
		t.Fatalf("wrap: got %x, want %x", got, wrap)
	}
}

func TestConsumePairingToken_NoWrapReturnsNil(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	sec, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute, nil)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	_, got, err := s.ConsumePairingToken(ctx, sec.Expose())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != nil {
		t.Fatalf("wrap: expected nil, got %x", got)
	}
}
