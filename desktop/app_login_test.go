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
	relay.NewOpaqueAuthHandler(store, opaqueSrv).Register(mux)
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
