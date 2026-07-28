package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

func newTestConfigStore(t *testing.T) *configStore {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return loadConfig()
}

func TestAdapter_PinnedSessionIds_RoundTrip(t *testing.T) {
	cs := newTestConfigStore(t)
	a := newAppConfigAdapter(cs)

	want := []string{"sid-a", "sid-b"}
	raw, _ := json.Marshal(want)
	if err := a.WriteValue("pinned_session_ids", raw); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	got, ok := a.ReadValue("pinned_session_ids")
	if !ok {
		t.Fatal("ReadValue returned ok=false")
	}
	var back []string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 || back[0] != "sid-a" || back[1] != "sid-b" {
		t.Fatalf("got %v; want %v", back, want)
	}

	// Adapter must expose the new key.
	found := false
	for _, k := range a.Keys() {
		if k == "pinned_session_ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Keys() missing pinned_session_ids: %v", a.Keys())
	}

	_ = prefssync.Meta{} // silence unused import if none other
}
