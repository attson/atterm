package main

import (
	"context"
	"testing"
)

// TestAgentSealAccountKey_DisableE2EEMakesItNil checks the core
// invariant: when the user flips the DisableE2EE config flag, the
// closure handed to newUplink returns nil even though the underlying
// account_key is still loaded in memory. Every seal site in uplink.go
// guards on len(ak) > 0 or `accountKey != nil`, so a nil return is
// the existing unsealed code path — no additional branching needed.
//
// The JS-side accountKeySnapshot is UNAFFECTED so cross-desktop
// decrypt (account_key loaded on this device, sealed frames coming
// in from another device) keeps working.
func TestAgentSealAccountKey_DisableE2EEMakesItNil(t *testing.T) {
	a := newRelayTestApp(t)

	// Seed an account_key so the test exercises the "key present"
	// branch rather than the "no key" trivial case.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	a.setAccountKey(key)

	// E2EE on by default — sealing should be live.
	if got := a.agentSealAccountKey(); len(got) != 32 {
		t.Fatalf("with toggle off: agentSealAccountKey len = %d; want 32", len(got))
	}
	if got := a.accountKeySnapshot(); len(got) != 32 {
		t.Fatalf("with toggle off: accountKeySnapshot len = %d; want 32", len(got))
	}

	// Flip the toggle. Agent side goes nil; JS side still sees the key.
	if err := a.SetRelayDisableE2EE(true); err != nil {
		t.Fatalf("SetRelayDisableE2EE(true): %v", err)
	}
	if got := a.agentSealAccountKey(); got != nil {
		t.Fatalf("with toggle on: agentSealAccountKey = %x; want nil", got)
	}
	if got := a.accountKeySnapshot(); len(got) != 32 {
		t.Fatalf("with toggle on: accountKeySnapshot len = %d; want 32 (JS path unaffected)", len(got))
	}

	// Flip back — sealing comes alive again, no restart needed.
	if err := a.SetRelayDisableE2EE(false); err != nil {
		t.Fatalf("SetRelayDisableE2EE(false): %v", err)
	}
	if got := a.agentSealAccountKey(); len(got) != 32 {
		t.Fatalf("after un-flip: agentSealAccountKey len = %d; want 32", len(got))
	}

	// RelayConfig must reflect the toggle state so the Settings UI
	// can drive its checkbox off it.
	if err := a.SetRelayDisableE2EE(true); err != nil {
		t.Fatalf("SetRelayDisableE2EE(true) #2: %v", err)
	}
	rc := a.GetRelayConfig()
	if !rc.DisableE2EE {
		t.Fatalf("GetRelayConfig().DisableE2EE = false; want true after toggle")
	}
}

// TestSetRelayDisableE2EE_EmitsEvent: every flip must push an
// e2ee-mode-changed event so the TitleBar chip + Settings checkbox
// stay in sync without polling. No-op flips (same value) skip the
// event to avoid spurious UI churn.
func TestSetRelayDisableE2EE_EmitsEvent(t *testing.T) {
	a := newRelayTestApp(t)
	var events []map[string]any
	a.eventsEmitter = func(_ context.Context, name string, data ...interface{}) {
		if name != "e2ee-mode-changed" {
			return
		}
		if len(data) != 1 {
			t.Fatalf("event %q payload count = %d; want 1", name, len(data))
		}
		payload, ok := data[0].(map[string]any)
		if !ok {
			t.Fatalf("event %q payload type = %T; want map[string]any", name, data[0])
		}
		events = append(events, payload)
	}

	if err := a.SetRelayDisableE2EE(true); err != nil {
		t.Fatalf("flip 1: %v", err)
	}
	if err := a.SetRelayDisableE2EE(true); err != nil {
		t.Fatalf("no-op flip: %v", err)
	}
	if err := a.SetRelayDisableE2EE(false); err != nil {
		t.Fatalf("flip 2: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("event count = %d; want 2 (no-op flip suppresses event): %+v", len(events), events)
	}
	if events[0]["disabled"] != true {
		t.Fatalf("first event disabled = %v; want true", events[0]["disabled"])
	}
	if events[1]["disabled"] != false {
		t.Fatalf("second event disabled = %v; want false", events[1]["disabled"])
	}
}
