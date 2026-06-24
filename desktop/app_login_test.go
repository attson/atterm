package main

import (
	"bytes"
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
	relay.NewOpaqueAuthHandler(store, opaqueSrv, "", "").Register(mux)
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
	// Simulate a relaunch / migrated config that lost the persisted user id
	// (and in-memory key) but kept the relay URL — the state in which the
	// account_key persist-ordering bug dropped the key (persistAccountKey ran
	// before RelaySessionUserID was committed, so it saved under an empty id
	// and the next launch booted locked → remote E2EE sessions undecryptable).
	cfg := a.cfgStore.Get()
	cfg.RelaySessionUserID = ""
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("clear user id: %v", err)
	}
	a.setAccountKey(nil) // simulate fresh session: no in-memory key yet

	if err := a.LoginRemoteRelay(ts.URL, "u@example.com", "pw-correct", false); err != nil {
		t.Fatalf("LoginRemoteRelay: %v", err)
	}

	rc := a.GetRelayConfig()
	if rc.Token == "" {
		t.Fatalf("session token not persisted")
	}
	if rc.LastEmail != "u@example.com" {
		t.Fatalf("email not persisted: got %q", rc.LastEmail)
	}
	wantURL := "ws://" + strings.TrimPrefix(ts.URL, "http://")
	if rc.URL != wantURL {
		t.Fatalf("url: got %q want %q", rc.URL, wantURL)
	}
	if !a.HasAccountKey() {
		t.Fatalf("account_key not unlocked into App memory after login")
	}
	// The key the next launch reads back: loadAccountKey(persisted URL, user id).
	// Must be non-empty — i.e. persisted under the identity boot looks up.
	persisted := a.cfgStore.Get()
	if k, err := loadAccountKey(persisted.RelayURL, persisted.RelaySessionUserID); err != nil {
		t.Fatalf("loadAccountKey: %v", err)
	} else if len(k) == 0 {
		t.Fatal("account_key not persisted under the current (URL, user id) — would be lost on next launch")
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

// TestSetRelayConfig_MigratesAccountKeyOnURLChange covers the user's actual
// failure: the relay was first reached at one address (e.g. wss://ip:port) and
// later by another (e.g. wss://domain) for the SAME relay+account, without a
// re-login. Because the keychain entry is origin-scoped, the key would orphan
// under the old origin. SetRelayConfig must move it to the new origin while an
// account_key is unlocked.
func TestSetRelayConfig_MigratesAccountKeyOnURLChange(t *testing.T) {
	a := newRelayTestApp(t)
	// Stop the background uplink on cleanup so its retry loop doesn't starve
	// the timing-sensitive uplink E2E test later in the package.
	t.Cleanup(func() { _ = a.SetRelayConfig(RelayConfig{URL: "", RemotePermission: "full"}) })

	// Seed the "already logged in at oldURL" state directly — no OPAQUE round
	// trip needed, the migration logic only reads (RelayURL, user id) + the
	// in-memory key. Both URLs are loopback so neither uplink dials externally.
	const oldURL = "ws://127.0.0.1:59370"
	const newURL = "ws://127.0.0.1:59371"
	const uid = "user-abc"
	key := bytes.Repeat([]byte{0xAB}, 32)
	cfg := a.cfgStore.Get()
	cfg.RelayURL = oldURL
	cfg.RelaySessionToken = "tok"
	cfg.RelaySessionUserID = uid
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	a.setAccountKeyInMemory(key)
	if err := saveAccountKey(oldURL, uid, key); err != nil {
		t.Fatalf("seed account_key: %v", err)
	}

	// User edits the relay address (same relay+account) without re-login.
	if err := a.SetRelayConfig(RelayConfig{
		URL:              newURL,
		Token:            "tok",
		RemotePermission: "full",
	}); err != nil {
		t.Fatalf("SetRelayConfig: %v", err)
	}

	if k, _ := loadAccountKey(newURL, uid); len(k) == 0 {
		t.Fatal("account_key not migrated to the new relay origin")
	}
	if k, _ := loadAccountKey(oldURL, uid); len(k) != 0 {
		t.Fatal("stale account_key under old origin not cleared")
	}
}

// TestRememberRelayPassword_PersistsWithoutLogin: the user clicks Connect
// with a non-empty password, but probe/login fails. RememberRelayPassword
// should still persist the password so the next launch can prefill it.
func TestRememberRelayPassword_PersistsWithoutLogin(t *testing.T) {
	a := newRelayTestApp(t)
	// Simulate the SettingsRelay flow's setRelayConfig call so the cfg has
	// the URL + email the user typed (but no token — login never succeeded).
	if err := a.SetRelayConfig(RelayConfig{
		URL:                "wss://r.example.com",
		LastEmail:          "u@example.com",
		RemotePermission:   "full",
		AllowInsecureRelay: false,
	}); err != nil {
		t.Fatalf("SetRelayConfig: %v", err)
	}

	if err := a.RememberRelayPassword("typed-pw"); err != nil {
		t.Fatalf("RememberRelayPassword: %v", err)
	}

	got, _ := loadRelayPassword("wss://r.example.com", "u@example.com")
	if got != "typed-pw" {
		t.Fatalf("slot: got %q want %q", got, "typed-pw")
	}
}

// TestRememberRelayPassword_EmptyPasswordIsNoop: an empty password must not
// clear an existing stored value (guards a click-Connect-with-blank-field
// regression).
func TestRememberRelayPassword_EmptyPasswordIsNoop(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.SetRelayConfig(RelayConfig{
		URL:              "wss://r.example.com",
		LastEmail:        "u@example.com",
		RemotePermission: "full",
	}); err != nil {
		t.Fatalf("SetRelayConfig: %v", err)
	}
	// Seed a stored password.
	if err := saveRelayPassword("wss://r.example.com", "u@example.com", "keeper"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := a.RememberRelayPassword(""); err != nil {
		t.Fatalf("RememberRelayPassword(empty): %v", err)
	}

	got, _ := loadRelayPassword("wss://r.example.com", "u@example.com")
	if got != "keeper" {
		t.Fatalf("empty-password call wiped the slot: got %q want %q", got, "keeper")
	}
}

// TestRememberRelayPassword_NoEmailNoop: with no email cached in cfg, the
// underlying key is empty so the call is a no-op (does not panic, does not
// error).
func TestRememberRelayPassword_NoEmailNoop(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.SetRelayConfig(RelayConfig{
		URL:              "wss://r.example.com",
		RemotePermission: "full",
	}); err != nil {
		t.Fatalf("SetRelayConfig: %v", err)
	}
	if err := a.RememberRelayPassword("anything"); err != nil {
		t.Fatalf("RememberRelayPassword: %v", err)
	}
}
