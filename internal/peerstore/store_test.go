package peerstore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	path := filepath.Join(t.TempDir(), "peer-space.json")
	return New(path, func() ([]byte, error) { return append([]byte(nil), key...), nil }), key
}

func TestEncryptedStoreAndAtomicConsumption(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "genesis-secret", LocalMembership: "membership-secret", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	invitation := Invitation{InviteID: "invite-1", BatchID: "batch-1", Token: "ticket-secret", ExpiresAt: now.Add(time.Hour).Unix()}
	if err := store.AddInvitations([]Invitation{invitation}, now); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range [][]byte{[]byte("genesis-secret"), []byte("membership-secret"), []byte("ticket-secret")} {
		if bytes.Contains(blob, plaintext) {
			t.Fatalf("encrypted store contains plaintext %q", plaintext)
		}
	}
	if err := store.ConsumeInvitation("invite-1", "consumer-peer", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeInvitation("invite-1", "other-peer", now.Add(2*time.Minute)); !errors.Is(err, ErrInviteConsumed) {
		t.Fatalf("second consume error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invitations[0].ConsumedByPeerID != "consumer-peer" {
		t.Fatalf("consumed by = %q", state.Invitations[0].ConsumedByPeerID)
	}
}

func TestRevocationIsIdempotentAndDenyWins(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	invite := Invitation{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}
	if err := store.AddInvitations([]Invitation{invite}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeBatch("b", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeBatch("b", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeInvitation("i", "peer", now.Add(3*time.Minute)); !errors.Is(err, ErrInviteRevoked) {
		t.Fatalf("consume revoked error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := state.Invitations[0].RevokedAt, now.Add(time.Minute).Unix(); got != want {
		t.Fatalf("RevokedAt = %d, want first timestamp %d", got, want)
	}
}

func TestWrongKeyAndTamperFailClosed(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	wrong := New(store.path, func() ([]byte, error) { return bytes.Repeat([]byte{0x99}, 32), nil })
	if _, err := wrong.Load(); err == nil {
		t.Fatal("wrong key decrypted store")
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-3] ^= 1
	if err := os.WriteFile(store.path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("tampered store loaded")
	}
}

func TestMissingStore(t *testing.T) {
	store, _ := testStore(t)
	if _, err := store.Load(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("Load missing error = %v", err)
	}
}

func TestTwoStoreInstancesCannotConsumeOneInvitation(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddInvitations([]Invitation{{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}}, now); err != nil {
		t.Fatal(err)
	}
	other := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	stores := []*Store{store, other}
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs[index] = stores[index].ConsumeInvitation("i", "peer", now.Add(time.Minute))
		}(index)
	}
	wg.Wait()
	successes := 0
	consumed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInviteConsumed):
			consumed++
		default:
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("consume results = %#v", errs)
	}
}
