package main

import "testing"

// newAppWithRelay creates a minimal App (via newRelayTestApp) with a relay
// URL + session token already seeded in cfgStore, pointing at relayURL.
// Shared by tests that need CreatePairingToken (and similar relay-backed
// calls) to have a configured relay without repeating the cfgStore.Set
// boilerplate.
func newAppWithRelay(t *testing.T, relayURL string) *App {
	t.Helper()
	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{RelayURL: relayURL, RelaySessionToken: "atk_localtest"}); err != nil {
		t.Fatalf("seed cfgStore: %v", err)
	}
	return a
}

// setAccountKeyForTest sets the in-memory account_key directly (bypassing
// keychain persistence) so tests can simulate an unlocked E2EE session.
func (a *App) setAccountKeyForTest(key []byte) {
	a.setAccountKeyInMemory(key)
}
