package main

import "testing"

// TestSetShortcutBindings_ValidRoundTrips confirms a well-formed map is
// persisted and read back unchanged.
func TestSetShortcutBindings_ValidRoundTrips(t *testing.T) {
	a := newRelayTestApp(t)

	valid := map[string]string{
		"tab.new":    "Mod+KeyT",
		"pane.close": "",
	}
	if err := a.SetShortcutBindings(valid); err != nil {
		t.Fatalf("SetShortcutBindings(valid): %v", err)
	}

	got := a.GetShortcutBindings()
	if len(got) != len(valid) || got["tab.new"] != "Mod+KeyT" || got["pane.close"] != "" {
		t.Fatalf("GetShortcutBindings() = %v; want %v", got, valid)
	}
}

// TestSetShortcutBindings_RejectsEmptyActionID covers the first of the two
// rules ValidatePluginConfig already enforces on the legacy
// Plugins.Shortcuts.Bindings slot (desktop/plugin_config.go). Task 3 added a
// second write path to the same data (SetShortcutBindings, syncable) that
// originally skipped validation entirely — an empty action id here would
// have synced to every other device via prefsSync.
func TestSetShortcutBindings_RejectsEmptyActionID(t *testing.T) {
	a := newRelayTestApp(t)

	// Seed a known-good value first so we can assert it survives the
	// rejected call untouched.
	seed := map[string]string{"tab.new": "Mod+KeyT"}
	if err := a.SetShortcutBindings(seed); err != nil {
		t.Fatalf("seed SetShortcutBindings: %v", err)
	}

	err := a.SetShortcutBindings(map[string]string{"": "Mod+KeyN"})
	if err == nil {
		t.Fatal("expected an error for an empty action id, got nil")
	}

	got := a.GetShortcutBindings()
	if len(got) != 1 || got["tab.new"] != "Mod+KeyT" {
		t.Fatalf("rejected call must leave the stored map untouched: got %v, want %v", got, seed)
	}
}

// TestSetShortcutBindings_RejectsMalformedBinding covers the second rule:
// a binding string that fails isValidShortcutBinding (here, no modifier
// prefix — the same "at least one modifier" rule the regex can't express
// itself, per plugin_config.go's isValidShortcutBinding comment).
func TestSetShortcutBindings_RejectsMalformedBinding(t *testing.T) {
	a := newRelayTestApp(t)

	seed := map[string]string{"tab.new": "Mod+KeyT"}
	if err := a.SetShortcutBindings(seed); err != nil {
		t.Fatalf("seed SetShortcutBindings: %v", err)
	}

	err := a.SetShortcutBindings(map[string]string{"pane.close": "KeyN"})
	if err == nil {
		t.Fatal("expected an error for a binding with no modifier, got nil")
	}

	got := a.GetShortcutBindings()
	if len(got) != 1 || got["tab.new"] != "Mod+KeyT" {
		t.Fatalf("rejected call must leave the stored map untouched: got %v, want %v", got, seed)
	}
}
