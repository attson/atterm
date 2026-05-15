package webpush

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileGeneratesFresh(t *testing.T) {
	dir := t.TempDir()
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey == "" || state.PublicKey == "" {
		t.Fatal("fresh state missing VAPID keys")
	}
	if len(state.Subscriptions) != 0 {
		t.Fatalf("fresh state has %d subs; want 0", len(state.Subscriptions))
	}
	// File should now exist on disk.
	if _, err := os.Stat(filepath.Join(dir, "web-push.json")); err != nil {
		t.Fatalf("web-push.json not created: %v", err)
	}
}

func TestLoadValidFileRoundTrips(t *testing.T) {
	dir := t.TempDir()
	original := persistedState{
		PrivateKey: "priv-abc",
		PublicKey:  "pub-xyz",
		Subscriptions: map[string][]Subscription{
			"tok1": {{Endpoint: "https://push.example/abc"}},
		},
	}
	data, _ := json.Marshal(original)
	if err := os.WriteFile(filepath.Join(dir, "web-push.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey != "priv-abc" {
		t.Fatalf("priv = %q; want priv-abc", state.PrivateKey)
	}
	if state.PublicKey != "pub-xyz" {
		t.Fatalf("pub = %q; want pub-xyz", state.PublicKey)
	}
	if len(state.Subscriptions["tok1"]) != 1 {
		t.Fatal("sub not loaded")
	}
}

func TestLoadCorruptFileBacksUpAndRegenerates(t *testing.T) {
	dir := t.TempDir()
	original := []byte("not json at all {{{ broken")
	path := filepath.Join(dir, "web-push.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey == "" {
		t.Fatal("did not regenerate after corrupt")
	}
	// Old file should be renamed with .corrupt-* suffix.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hasCorrupt := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "web-push.json.corrupt-") {
			hasCorrupt = true
		}
	}
	if !hasCorrupt {
		t.Fatalf("no .corrupt-* backup found; entries=%v", entries)
	}
}

func TestSaveStateWriteTempRename(t *testing.T) {
	dir := t.TempDir()
	state := persistedState{PrivateKey: "p", PublicKey: "q"}
	if err := saveState(dir, state); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "web-push.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got persistedState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.PrivateKey != "p" {
		t.Fatalf("priv = %q; want p", got.PrivateKey)
	}
}

func TestLoadEmptyDirIsAllowedWhenWritable(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadOrInitState(dir); err != nil {
		t.Fatalf("loadOrInitState(writable empty dir): %v", err)
	}
}
