package main

import "testing"

func TestDirectP2PPreferenceDefaultsOffAndControlsUplink(t *testing.T) {
	a := newRelayTestApp(t)
	if a.GetDirectP2PEnabled() {
		t.Fatal("direct P2P must default off")
	}
	if err := a.cfgStore.Set(appConfig{
		RelayURL:          "wss://relay.example",
		RelaySessionToken: "atk_direct_test",
		RemotePermission:  "full",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetDirectP2PEnabled(true); err != nil {
		t.Fatalf("SetDirectP2PEnabled(true): %v", err)
	}
	if !a.cfgStore.Get().DirectP2PEnabled || !a.GetDirectP2PEnabled() {
		t.Fatal("enabled preference was not persisted")
	}
	a.mu.Lock()
	uplink := a.uplink
	a.mu.Unlock()
	if uplink == nil || !uplink.directEnabled {
		t.Fatal("restarted uplink did not enable direct host transport")
	}
	if err := a.SetDirectP2PEnabled(false); err != nil {
		t.Fatalf("SetDirectP2PEnabled(false): %v", err)
	}
	if a.GetDirectP2PEnabled() || a.cfgStore.Get().DirectP2PEnabled {
		t.Fatal("disabled preference was not persisted")
	}
}

func TestDirectP2PEnvironmentOverrideForcesOn(t *testing.T) {
	t.Setenv("ATTERM_DIRECT_P2P", "1")
	a := newRelayTestApp(t)
	if !a.GetDirectP2PEnabled() {
		t.Fatal("ATTERM_DIRECT_P2P must force the rollout preference on")
	}
}
