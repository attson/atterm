package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
)

// newOPAQUERelay spins up an in-process atterm-relay HTTP server with the
// OPAQUE endpoints wired. The desktop's LoginRemoteRelay calls into this
// server via the e2eeclient SDK.
func newOPAQUERelay(t *testing.T) (*httptest.Server, *userstore.SQLiteStore) {
	t.Helper()
	store := userstore.NewInMemory(t)
	opaqueSrv, err := relay.LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	mux := http.NewServeMux()
	relay.NewOpaqueAuthHandler(store, opaqueSrv, "").Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, store
}

// TestLoginRemoteRelay_PersistsSessionToken: register a user against an
// in-process relay, then call LoginRemoteRelay with the same credentials
// and assert URL/token/email landed in the persisted config and that the
// account_key was unlocked into App memory.
func TestLoginRemoteRelay_PersistsSessionToken(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	// Register first so login has a record to verify against.
	if err := a.RegisterRemoteRelay(ts.URL, "u@example.com", "pw-correct", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}
	a.setAccountKey(nil) // simulate fresh session: no in-memory key yet

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "pw-correct", false); err != nil {
		t.Fatalf("LoginRemoteRelay: %v", err)
	}

	cfg := a.GetRelayConfig()
	if cfg.Token == "" {
		t.Fatalf("session token not persisted")
	}
	if cfg.LastEmail != "u@example.com" {
		t.Fatalf("email not persisted: got %q", cfg.LastEmail)
	}
	wantURL := "ws://" + strings.TrimPrefix(ts.URL, "http://")
	if cfg.URL != wantURL {
		t.Fatalf("url: got %q want %q", cfg.URL, wantURL)
	}
	if !a.HasAccountKey() {
		t.Fatalf("account_key not unlocked into App memory after login")
	}
}

// TestLoginRemoteRelay_WrongPasswordReturnsError verifies a wrong password
// surfaces an error and does NOT overwrite an existing persisted config
// or unlock an account_key.
func TestLoginRemoteRelay_WrongPasswordReturnsError(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	// Register once with the correct password so the relay has the
	// envelope to compare against on the next login attempt.
	if err := a.RegisterRemoteRelay(ts.URL, "u@example.com", "correct-pw", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}
	prevCfg := a.GetRelayConfig()
	a.setAccountKey(nil)

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "wrong-pw", false); err == nil {
		t.Fatal("expected error on wrong password")
	}

	cfg := a.GetRelayConfig()
	if cfg.Token != prevCfg.Token {
		t.Fatalf("token mutated on failed login: was %q now %q", prevCfg.Token, cfg.Token)
	}
	if a.HasAccountKey() {
		t.Fatalf("account_key was unlocked despite failed login")
	}
}

// TestRegisterRemoteRelay_PersistsAndUnlocksKey covers the happy register
// path: URL + email + token persisted, account_key unlocked into memory.
func TestRegisterRemoteRelay_PersistsAndUnlocksKey(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	if err := a.RegisterRemoteRelay(ts.URL, "fresh@example.com", "pw", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}
	cfg := a.GetRelayConfig()
	if cfg.Token == "" {
		t.Fatalf("session token not persisted")
	}
	if cfg.LastEmail != "fresh@example.com" {
		t.Fatalf("email not persisted: got %q", cfg.LastEmail)
	}
	if !a.HasAccountKey() {
		t.Fatalf("account_key not unlocked into App memory after register")
	}
}

// TestLoginRemoteRelay_PersistsPassword: a successful login writes the
// password to the safekeyring slot keyed by the persisted relay URL + email,
// readable back through loadRelayPassword.
func TestLoginRemoteRelay_PersistsPassword(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	if err := a.RegisterRemoteRelay(ts.URL, "u@example.com", "first-pw", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}
	a.setAccountKey(nil)

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "first-pw", false); err != nil {
		t.Fatalf("LoginRemoteRelay: %v", err)
	}

	cfg := a.GetRelayConfig()
	got, err := loadRelayPassword(cfg.URL, cfg.LastEmail)
	if err != nil {
		t.Fatalf("loadRelayPassword: %v", err)
	}
	if got != "first-pw" {
		t.Fatalf("password slot: got %q want %q", got, "first-pw")
	}
}

// TestLoginRemoteRelay_OverwritesPassword: a successful login overwrites a
// stale password previously sitting in the same (relay-URL, email) slot. We
// pre-seed the slot with a wrong value, then log in successfully — the slot
// must reflect the password the login was actually performed with, not the
// stale seed.
func TestLoginRemoteRelay_OverwritesPassword(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	if err := a.RegisterRemoteRelay(ts.URL, "u@example.com", "real-pw", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}

	// Pre-seed the slot with a stale value, using the exact same key the
	// login path writes (cfg.RelayURL holds the wsURL that LoginRemoteRelay
	// normalizes to). LoginRemoteRelay must overwrite this seed.
	cfg := a.GetRelayConfig()
	if err := saveRelayPassword(cfg.URL, "u@example.com", "stale-seed"); err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	a.setAccountKey(nil)

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "real-pw", false); err != nil {
		t.Fatalf("LoginRemoteRelay: %v", err)
	}

	got, err := loadRelayPassword(cfg.URL, "u@example.com")
	if err != nil {
		t.Fatalf("loadRelayPassword: %v", err)
	}
	if got != "real-pw" {
		t.Fatalf("slot not overwritten: got %q want %q", got, "real-pw")
	}
}

// TestLoginRemoteRelay_WrongPassword_PreservesStoredPassword: a failed
// login (wrong password) must not corrupt the previously stored password.
func TestLoginRemoteRelay_WrongPassword_PreservesStoredPassword(t *testing.T) {
	ts, _ := newOPAQUERelay(t)
	a := newRelayTestApp(t)

	if err := a.RegisterRemoteRelay(ts.URL, "u@example.com", "correct-pw", "", false); err != nil {
		t.Fatalf("RegisterRemoteRelay: %v", err)
	}
	cfg := a.GetRelayConfig()

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "wrong-pw", false); err == nil {
		t.Fatalf("expected wrong-password error, got nil")
	}

	got, _ := loadRelayPassword(cfg.URL, "u@example.com")
	if got != "correct-pw" {
		t.Fatalf("stored password corrupted by failed login: got %q want %q", got, "correct-pw")
	}
}
