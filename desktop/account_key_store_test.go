package main

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/zalando/go-keyring"
)

// setupMockKeyring swaps in the in-memory provider go-keyring ships
// with so the tests don't talk to the host's real Keychain / Wincred /
// libsecret. MockInit replaces the package global; subsequent
// keyring.Set/Get/Delete are routed in-memory until process exit.
func setupMockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
}

func TestSaveLoadAccountKey_RoundTrip(t *testing.T) {
	setupMockKeyring(t)

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	const (
		relay = "https://relay.example.com"
		uid   = "01H8XYZ123"
	)
	if err := saveAccountKey(relay, uid, key); err != nil {
		t.Fatalf("saveAccountKey: %v", err)
	}
	got, err := loadAccountKey(relay, uid)
	if err != nil {
		t.Fatalf("loadAccountKey: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(key))
	}
}

func TestLoadAccountKey_Missing_ReturnsNil(t *testing.T) {
	setupMockKeyring(t)
	got, err := loadAccountKey("https://nowhere.example.com", "no-such-user")
	if err != nil {
		t.Fatalf("expected nil err for missing entry, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil bytes, got %x", got)
	}
}

func TestSaveAccountKey_NilKey_Clears(t *testing.T) {
	setupMockKeyring(t)
	const (
		relay = "https://r"
		uid   = "u"
	)
	if err := saveAccountKey(relay, uid, []byte("seed")); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	if err := saveAccountKey(relay, uid, nil); err != nil {
		t.Fatalf("nil save: %v", err)
	}
	got, _ := loadAccountKey(relay, uid)
	if got != nil {
		t.Fatalf("expected nil after nil-save, got %x", got)
	}
}

func TestClearAccountKeyFor_MissingIsNoOp(t *testing.T) {
	setupMockKeyring(t)
	if err := clearAccountKeyFor("https://r", "u"); err != nil {
		t.Fatalf("clear on empty: %v", err)
	}
}

func TestAccountKeyAccount_NamespacesByRealmAndUser(t *testing.T) {
	a := accountKeyAccount("realm-1", "user-1")
	b := accountKeyAccount("realm-2", "user-1")
	c := accountKeyAccount("realm-1", "user-2")
	if a == b || a == c || b == c {
		t.Fatalf("expected distinct account names, got %q %q %q", a, b, c)
	}
	if a != "realm-1|user-1" {
		t.Fatalf("account name = %q, want realm-1|user-1", a)
	}
}

func TestAccountKeyAccount_EmptyInputs_Skip(t *testing.T) {
	setupMockKeyring(t)
	// Empty inputs must not call into keyring at all (no errors, no
	// stored entry).
	if err := saveAccountKey("", "u", []byte("x")); err != nil {
		t.Fatalf("empty relay save returned %v", err)
	}
	if err := saveAccountKey("https://r", "", []byte("x")); err != nil {
		t.Fatalf("empty user save returned %v", err)
	}
	got, err := loadAccountKey("", "u")
	if err != nil || got != nil {
		t.Fatalf("empty relay load: got=%x err=%v", got, err)
	}
	got, err = loadAccountKey("https://r", "")
	if err != nil || got != nil {
		t.Fatalf("empty user load: got=%x err=%v", got, err)
	}
}
