package main

import "testing"

// TestSetProfiles_ValidRoundTrips confirms a well-formed profile list is
// persisted and read back unchanged, matching TestSetShortcutBindings_ValidRoundTrips'
// shape for the sibling syncable-write binding.
func TestSetProfiles_ValidRoundTrips(t *testing.T) {
	a := newRelayTestApp(t)

	valid := []SessionProfile{
		{ID: "p1", Name: "Work", Shell: "/bin/zsh"},
		{ID: "p2", Name: "Personal", Cwd: "/tmp"},
	}
	if err := a.SetProfiles(valid); err != nil {
		t.Fatalf("SetProfiles(valid): %v", err)
	}

	got := a.GetProfiles()
	if len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("GetProfiles() = %+v; want %+v", got, valid)
	}
}

// TestSetProfiles_RejectsEmptyID is the outbound counterpart of
// filterValidProfiles' inbound "empty id" drop (profiles.go) — SetProfiles
// is the only thing between a malformed local edit and a payload pushed to
// every device, so it must reject rather than silently store one. A
// rejected call must leave the previously stored profiles untouched (same
// discipline as SetShortcutBindings).
func TestSetProfiles_RejectsEmptyID(t *testing.T) {
	a := newRelayTestApp(t)

	seed := []SessionProfile{{ID: "p1", Name: "Work"}}
	if err := a.SetProfiles(seed); err != nil {
		t.Fatalf("seed SetProfiles: %v", err)
	}

	err := a.SetProfiles([]SessionProfile{{ID: "", Name: "No id"}})
	if err == nil {
		t.Fatal("expected an error for an empty profile id, got nil")
	}

	got := a.GetProfiles()
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("rejected call must leave stored profiles untouched: got %+v, want %+v", got, seed)
	}
}

// TestSetProfiles_RejectsDuplicateID mirrors filterValidProfiles' inbound
// duplicate-id drop on the outbound side. Deleting SetProfiles' validation
// loop breaks no other test — this is what would catch it.
func TestSetProfiles_RejectsDuplicateID(t *testing.T) {
	a := newRelayTestApp(t)

	seed := []SessionProfile{{ID: "p1", Name: "Work"}}
	if err := a.SetProfiles(seed); err != nil {
		t.Fatalf("seed SetProfiles: %v", err)
	}

	err := a.SetProfiles([]SessionProfile{
		{ID: "a", Name: "First"},
		{ID: "a", Name: "Second"},
	})
	if err == nil {
		t.Fatal("expected an error for a duplicate profile id, got nil")
	}

	got := a.GetProfiles()
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("rejected call must leave stored profiles untouched: got %+v, want %+v", got, seed)
	}
}

// TestSetProfiles_RejectsEmptyName covers the third rule in SetProfiles'
// validation loop.
func TestSetProfiles_RejectsEmptyName(t *testing.T) {
	a := newRelayTestApp(t)

	seed := []SessionProfile{{ID: "p1", Name: "Work"}}
	if err := a.SetProfiles(seed); err != nil {
		t.Fatalf("seed SetProfiles: %v", err)
	}

	err := a.SetProfiles([]SessionProfile{{ID: "p2", Name: "   "}})
	if err == nil {
		t.Fatal("expected an error for a whitespace-only profile name, got nil")
	}

	got := a.GetProfiles()
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("rejected call must leave stored profiles untouched: got %+v, want %+v", got, seed)
	}
}

// TestGetDefaultProfileID_SetDefaultProfileID_RoundTrip covers the other two
// of the four profile bindings, which also had zero test coverage.
func TestGetDefaultProfileID_SetDefaultProfileID_RoundTrip(t *testing.T) {
	a := newRelayTestApp(t)

	if got := a.GetDefaultProfileID(); got != "" {
		t.Fatalf("GetDefaultProfileID() on a fresh store = %q, want empty", got)
	}

	if err := a.SetProfiles([]SessionProfile{{ID: "p1", Name: "Work"}}); err != nil {
		t.Fatalf("SetProfiles: %v", err)
	}
	if err := a.SetDefaultProfileID("p1"); err != nil {
		t.Fatalf("SetDefaultProfileID: %v", err)
	}
	if got := a.GetDefaultProfileID(); got != "p1" {
		t.Fatalf("GetDefaultProfileID() = %q, want %q", got, "p1")
	}
}

// TestSetProfiles_StrandedDefaultIsClearedStructurally is finding H: removing
// the profile that DefaultProfileID currently names via SetProfiles must not
// leave DefaultProfileID dangling. Not reachable through the shipped UI
// today (SettingsProfiles.vue's deleteProfile() already compensates before
// calling persist()), but SetProfiles itself must not rely on every future
// caller remembering to do the same thing — same reasoning as env stripping
// living inside sealProfiles rather than at each call site.
func TestSetProfiles_StrandedDefaultIsClearedStructurally(t *testing.T) {
	a := newRelayTestApp(t)

	if err := a.SetProfiles([]SessionProfile{
		{ID: "p1", Name: "Work"},
		{ID: "p2", Name: "Personal"},
	}); err != nil {
		t.Fatalf("seed SetProfiles: %v", err)
	}
	if err := a.SetDefaultProfileID("p1"); err != nil {
		t.Fatalf("SetDefaultProfileID: %v", err)
	}

	// Remove p1 (the default) without going through SettingsProfiles.vue's
	// own compensating logic — a raw SetProfiles call, as any other current
	// or future caller might make.
	if err := a.SetProfiles([]SessionProfile{{ID: "p2", Name: "Personal"}}); err != nil {
		t.Fatalf("SetProfiles (drop default): %v", err)
	}

	if got := a.GetDefaultProfileID(); got != "" {
		t.Fatalf("DefaultProfileID left dangling at %q after its profile was removed via SetProfiles; want it cleared", got)
	}
}
