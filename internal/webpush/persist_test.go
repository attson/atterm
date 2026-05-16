package webpush

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestPersist_RenamesLegacyTokenHashSchema verifies that when web-push.json
// contains top-level keys that look like 64-char hex tokenHash values,
// loadOrInitState renames the file to web-push.json.legacy-<ts> and returns
// an empty (fresh) state.
func TestPersist_RenamesLegacyTokenHashSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web-push.json")

	// 64-char lowercase hex string — the shape of tokenHash output.
	legacyKey := strings.Repeat("a", 64)
	legacy := map[string]interface{}{
		"private_key": "priv-legacy",
		"public_key":  "pub-legacy",
		"subscriptions": map[string]interface{}{
			legacyKey: []map[string]interface{}{
				{"endpoint": "https://push.example/legacy"},
			},
		},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}

	// A .legacy-* backup must exist.
	entries, _ := os.ReadDir(dir)
	hasLegacy := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "web-push.json.legacy-") {
			hasLegacy = true
		}
	}
	if !hasLegacy {
		t.Fatalf("no .legacy-* file found; entries=%v", entries)
	}

	// The returned state must have an empty subscriptions map (legacy subs dropped).
	if len(state.Subscriptions) != 0 {
		t.Fatalf("subscriptions not empty after legacy rename; got %v", state.Subscriptions)
	}
}

// TestPersist_AcceptsUserIDSchema verifies that when web-push.json contains
// ULID-shaped keys (26-char Crockford base32), loadOrInitState loads them
// normally without renaming the file.
func TestPersist_AcceptsUserIDSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web-push.json")

	// 26-char ULID-shaped key.
	ulidKey := "01HXABC123456789ABCDEFGHIJ"
	fresh := persistedState{
		PrivateKey: "priv-fresh",
		PublicKey:  "pub-fresh",
		Subscriptions: map[string][]Subscription{
			ulidKey: {{Endpoint: "https://push.example/ulid"}},
		},
	}
	data, _ := json.Marshal(fresh)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}

	// File must NOT be renamed.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("web-push.json was renamed unexpectedly: %v", err)
	}

	// The subscription under the ULID key must be present.
	subs, ok := state.Subscriptions[ulidKey]
	if !ok || len(subs) != 1 || subs[0].Endpoint != "https://push.example/ulid" {
		t.Fatalf("subscription not loaded; subscriptions=%v", state.Subscriptions)
	}
}

// TestPersist_CleanupLegacyAfter30Days verifies that CleanupLegacy deletes
// .legacy-* files older than 30 days and keeps newer ones.
func TestPersist_CleanupLegacyAfter30Days(t *testing.T) {
	dir := t.TempDir()

	// Create a legacy file with mtime 31 days ago.
	old := filepath.Join(dir, "web-push.json.legacy-1000")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a legacy file with mtime 29 days ago.
	recent := filepath.Join(dir, "web-push.json.legacy-2000")
	if err := os.WriteFile(recent, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	recentTime := time.Now().Add(-29 * 24 * time.Hour)
	if err := os.Chtimes(recent, recentTime, recentTime); err != nil {
		t.Fatal(err)
	}

	if err := CleanupLegacy(context.Background(), dir); err != nil {
		t.Fatalf("CleanupLegacy: %v", err)
	}

	// 31-day-old file must be deleted.
	if _, err := os.Stat(old); err == nil {
		t.Fatal("31-day-old .legacy file was not deleted")
	}

	// 29-day-old file must remain.
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("29-day-old .legacy file was incorrectly deleted: %v", err)
	}
}
