package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/prefssync"
)

// newImportTestApp mirrors newExportTestApp (config_export_test.go): an App
// backed by an isolated config store and an isolated keyring, so nothing in
// these tests can touch the real OS config dir or keychain.
func newImportTestApp(t *testing.T) *App {
	t.Helper()
	useIsolatedKeyring(t)
	return &App{cfgStore: newTestConfigStore(t)}
}

// storeSnapshot serializes the entire config store to canonical JSON bytes.
// Used to prove PreviewConfigImport touches nothing: comparing the whole
// store (not just the fields a given test happens to seed) means a future
// field PreviewConfigImport accidentally wrote would fail this comparison
// even though no test author ever thought to check that field by name.
func storeSnapshot(t *testing.T, a *App) []byte {
	t.Helper()
	data, err := json.Marshal(a.cfgStore.Get())
	if err != nil {
		t.Fatalf("marshal config snapshot: %v", err)
	}
	return data
}

// assertStoreUnchanged fails the test if the store's serialized form
// differs from before.
func assertStoreUnchanged(t *testing.T, a *App, before []byte) {
	t.Helper()
	after := storeSnapshot(t, a)
	if string(before) != string(after) {
		t.Fatalf("config store mutated by PreviewConfigImport:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestPreviewConfigImport_ChangesNothing is the load-bearing test for this
// task (brief bullet 1): it seeds a config with values in every category
// PreviewConfigImport touches — scalar prefs, ssh hosts/keys, profiles — so
// there is something for every code path to accidentally write, exports it,
// mutates the export text (different values, a brand-new host/profile,
// dropped entries) so Preview has real add/replace work to compute, and
// compares the WHOLE store's serialized bytes before and after. A Preview
// that quietly wrote even one field would fail this, whether or not a human
// remembered to assert that specific field.
func TestPreviewConfigImport_ChangesNothing(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.LocalePreference = "zh-CN"
	cfg.TerminalTheme = "dark"
	cfg.TerminalFontSize = 14
	cfg.SSHHosts = []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
		{ID: "h2", Host: "two.example.com", User: "root", AuthKind: "password"},
	}
	cfg.SSHKeys = []SSHKey{{ID: "k1", Name: "prod-key", KeyType: "rsa"}}
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default"},
		{ID: "p2", Name: "staging"},
	}
	cfg.DefaultProfileID = "p1"
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	// Give Preview real work: change h1, add a brand-new host h3, drop h2
	// from the file, change terminal_theme, and rename p1.
	var hostsPayload sshHostsExportPayload
	if err := json.Unmarshal(export.Preferences["ssh_hosts"], &hostsPayload); err != nil {
		t.Fatalf("unmarshal ssh_hosts: %v", err)
	}
	hostsPayload.Hosts[0].User = "deploy"                        // was "root" -> replace
	hostsPayload.Hosts = append(hostsPayload.Hosts[:1], SSHHost{ // drop h2, add h3
		ID: "h3", Host: "three.example.com", User: "root", AuthKind: "password",
	})
	rawHosts, _ := json.Marshal(hostsPayload)
	export.Preferences["ssh_hosts"] = rawHosts

	var profilesPayload profilesExportPayload
	if err := json.Unmarshal(export.Preferences["profiles"], &profilesPayload); err != nil {
		t.Fatalf("unmarshal profiles: %v", err)
	}
	profilesPayload.Profiles[0].Name = "renamed-default"
	rawProfiles, _ := json.Marshal(profilesPayload)
	export.Preferences["profiles"] = rawProfiles

	export.Preferences["terminal_theme"], _ = json.Marshal("light")

	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	before := storeSnapshot(t, a)
	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	assertStoreUnchanged(t, a, before)

	// Sanity: Preview actually found the work we set up, so the "nothing
	// changed" result above isn't just because there was nothing to do.
	if len(preview.Changes) == 0 {
		t.Fatal("preview.Changes is empty; test setup produced no diffs to prove immutability against")
	}
}

// TestPreviewConfigImport_RejectsUnknownVersion pins brief bullet 3: an
// unknown atterm_export version is refused outright, with a clear error,
// rather than best-effort parsed.
func TestPreviewConfigImport_RejectsUnknownVersion(t *testing.T) {
	a := newImportTestApp(t)
	export := ConfigExport{
		Version:     configExportVersion + 1,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{"locale_preference": json.RawMessage(`"zh-CN"`)},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	before := storeSnapshot(t, a)
	preview, err := a.PreviewConfigImport(string(data))
	if err == nil {
		t.Fatal("PreviewConfigImport with an unknown version: err = nil, want an error")
	}
	// Both slices must be non-nil-but-empty, not nil: ImportPreview's
	// contract (see its doc comment / MAJOR 4) is "never marshals to null",
	// on every return path including this error one — not just the success
	// path with nothing to report.
	if preview.Changes == nil || preview.Skipped == nil {
		t.Fatalf("PreviewConfigImport on rejected version returned nil slice(s): %+v, want non-nil empty", preview)
	}
	if len(preview.Changes) != 0 || len(preview.Skipped) != 0 {
		t.Fatalf("PreviewConfigImport on rejected version returned a non-empty preview: %+v", preview)
	}
	assertStoreUnchanged(t, a, before)
}

// TestPreviewConfigImport_MergeByID_AddReplaceAndKeepLocal pins brief bullet
// 2 in full: same ID -> replace, new ID -> add, and — the explicit
// assertion the brief calls out by name — a local host/key/profile present
// ONLY locally (absent from the file) produces NO change entry at all. That
// silence is what makes import non-destructive: nothing in Changes ever
// suggests removing it, so applying only Changes could never wipe it. Keys
// get the exact same treatment as hosts (structurally identical code path),
// so this test exercises SSHKeys too rather than only SSHHosts.
func TestPreviewConfigImport_MergeByID_AddReplaceAndKeepLocal(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
		{ID: "h2", Host: "two.example.com", User: "root", AuthKind: "password"}, // local-only, kept
	}
	cfg.SSHKeys = []SSHKey{
		{ID: "k1", Name: "prod-key", KeyType: "rsa"},
		{ID: "k2", Name: "staging-key", KeyType: "ed25519"}, // local-only, kept
	}
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default"},
		{ID: "p2", Name: "staging"}, // local-only, kept
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fileHosts := sshHostsExportPayload{
		Hosts: []SSHHost{
			{ID: "h1", Host: "one.example.com", User: "deploy", AuthKind: "password"}, // same ID, different data -> replace
			{ID: "h3", Host: "three.example.com", User: "root", AuthKind: "password"}, // new ID -> add
		},
		Keys: []SSHKey{
			{ID: "k1", Name: "prod-key", KeyType: "ed25519"}, // same ID, different data -> replace
			{ID: "k3", Name: "new-key", KeyType: "rsa"},      // new ID -> add
		},
	}
	fileProfiles := profilesExportPayload{Profiles: []SessionProfile{
		{ID: "p1", Name: "default"},           // same ID, identical -> unchanged
		{ID: "p4", Name: "brand-new-profile"}, // new ID -> add
	}}
	rawHosts, _ := json.Marshal(fileHosts)
	rawProfiles, _ := json.Marshal(fileProfiles)
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"ssh_hosts": rawHosts,
			"profiles":  rawProfiles,
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}

	byKey := make(map[string]ImportChange, len(preview.Changes))
	for _, c := range preview.Changes {
		if _, dup := byKey[c.Key]; dup {
			t.Fatalf("duplicate ImportChange.Key %q in preview.Changes: %+v", c.Key, preview.Changes)
		}
		byKey[c.Key] = c
	}

	if got := byKey["ssh_host:h1"].Action; got != "replace" {
		t.Errorf(`Changes["ssh_host:h1"].Action = %q, want "replace"`, got)
	}
	if got := byKey["ssh_host:h3"].Action; got != "add" {
		t.Errorf(`Changes["ssh_host:h3"].Action = %q, want "add"`, got)
	}
	if got := byKey["ssh_key:k1"].Action; got != "replace" {
		t.Errorf(`Changes["ssh_key:k1"].Action = %q, want "replace"`, got)
	}
	if got := byKey["ssh_key:k3"].Action; got != "add" {
		t.Errorf(`Changes["ssh_key:k3"].Action = %q, want "add"`, got)
	}
	if got := byKey["profile:p1"].Action; got != "unchanged" {
		t.Errorf(`Changes["profile:p1"].Action = %q, want "unchanged"`, got)
	}
	if got := byKey["profile:p4"].Action; got != "add" {
		t.Errorf(`Changes["profile:p4"].Action = %q, want "add"`, got)
	}

	// The explicit "kept" assertion: h2, k2 and p2 exist locally, are
	// absent from the file, and must not appear in Changes under any key
	// or action — there is no action that would remove them, and nothing
	// here even names them.
	for _, c := range preview.Changes {
		if c.Key == "ssh_host:h2" {
			t.Fatalf("local-only host h2 (absent from file) produced a change entry: %+v — it must be silently kept, not touched", c)
		}
		if c.Key == "ssh_key:k2" {
			t.Fatalf("local-only key k2 (absent from file) produced a change entry: %+v — it must be silently kept, not touched", c)
		}
		if c.Key == "profile:p2" {
			t.Fatalf("local-only profile p2 (absent from file) produced a change entry: %+v — it must be silently kept, not touched", c)
		}
	}

	// And the store itself: still exactly what it was, h2/k2/p2 included.
	got := a.cfgStore.Get()
	if len(got.SSHHosts) != 2 || got.SSHHosts[1].ID != "h2" {
		t.Fatalf("SSHHosts after Preview = %+v, want h2 still present untouched", got.SSHHosts)
	}
	if len(got.SSHKeys) != 2 || got.SSHKeys[1].ID != "k2" {
		t.Fatalf("SSHKeys after Preview = %+v, want k2 still present untouched", got.SSHKeys)
	}
	if len(got.Profiles) != 2 || got.Profiles[1].ID != "p2" {
		t.Fatalf("Profiles after Preview = %+v, want p2 still present untouched", got.Profiles)
	}
}

// TestPreviewConfigImport_WhitespacePaddedIDStillMatchesLocal pins a MINOR
// fix: an id is trimmed for the map lookup AND for the compared struct
// itself. Before the fix, only the lookup key was trimmed — the struct
// handed to listAction still carried the untrimmed ID, so
// reflect.DeepEqual against the clean local record always disagreed on
// that one field and reported "replace" even when nothing a user would
// call a change was different.
func TestPreviewConfigImport_WhitespacePaddedIDStillMatchesLocal(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fileHosts := sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "  h1  ", Host: "one.example.com", User: "root", AuthKind: "password"},
	}}
	rawHosts, _ := json.Marshal(fileHosts)
	export := ConfigExport{
		Version:     configExportVersion,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{"ssh_hosts": rawHosts},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Key != "ssh_host:h1" || preview.Changes[0].Action != "unchanged" {
		t.Fatalf("Changes = %+v, want a single ssh_host:h1 unchanged", preview.Changes)
	}
}

// TestKnownImportKeys_MatchesSyncedKeys pins MAJOR 1: knownImportKeys() is
// derived from prefssync.SyncedKeys() (minus the two sealed names, which
// are re-homed under their unsealed file names "ssh_hosts"/"profiles"), not
// a second hand-copied list that could silently drift from the sync
// engine's own key set — e.g. a future synced key added to sync.go but
// never added to a copy-pasted list here, which would then be silently
// reported as "unknown preference key" and skipped forever. This test
// recomputes the expected set independently from the same source
// (prefssync.SyncedKeys()) rather than pinning a static literal, so it
// exercises knownImportKeys()'s *transformation* logic, not a fixed
// snapshot of today's key names.
func TestKnownImportKeys_MatchesSyncedKeys(t *testing.T) {
	want := map[string]bool{"ssh_hosts": true, "profiles": true}
	for _, k := range prefssync.SyncedKeys() {
		if k == "ssh_hosts_encrypted" || k == "profiles_encrypted" {
			continue
		}
		want[k] = true
	}
	got := knownImportKeys()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("knownImportKeys() = %v, want %v", got, want)
	}
}

// TestScalarPrefTarget_HandlesEverySyncedScalarKey guards the one piece of
// key-completeness that remains genuinely hand-maintained after the MAJOR 1
// fix: scalarPrefTarget's switch decides what Go type each synced scalar
// key unmarshals into, and — unlike knownImportKeys — it cannot be derived
// automatically, since prefssync.SyncedKeys() carries no type information.
// A synced key present in SyncedKeys() but missing from this switch would
// pass the "known" gate and then fail unmarshaling (or worse, silently
// unmarshal into the wrong shape) with no test catching the gap. ssh_hosts
// and profiles are excluded: they route through previewSSHHosts /
// previewProfiles, never through scalarPrefTarget.
func TestScalarPrefTarget_HandlesEverySyncedScalarKey(t *testing.T) {
	for _, k := range prefssync.SyncedKeys() {
		if k == "ssh_hosts_encrypted" || k == "profiles_encrypted" {
			continue
		}
		if scalarPrefTarget(k) == nil {
			t.Errorf("scalarPrefTarget(%q) = nil; prefssync.SyncedKeys() includes it but scalarPrefTarget's switch doesn't handle it", k)
		}
	}
}

// TestPreviewConfigImport_MalformedEntrySkippedRestContinues pins brief
// bullet 4: one malformed host (missing id) is skipped and counted; the
// well-formed host, key and profile in the same file still produce changes.
// Same rule prefssync.Engine.Pull applies per key (sync.go's Pull comment):
// one bad entry must not take the rest of the import down with it.
func TestPreviewConfigImport_MalformedEntrySkippedRestContinues(t *testing.T) {
	a := newImportTestApp(t)

	rawHosts := json.RawMessage(`{
		"hosts": [
			{"host": "missing-id.example.com", "user": "root", "auth_kind": "password"},
			{"id": "hgood", "host": "good.example.com", "user": "root", "auth_kind": "password"}
		],
		"keys": [
			{"id": "", "name": "no-id-key"},
			{"id": "kgood", "name": "good-key"}
		]
	}`)
	rawProfiles := json.RawMessage(`{
		"profiles": [
			{"id": "pbad", "name": ""},
			{"id": "pgood", "name": "Good Profile"}
		]
	}`)
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"ssh_hosts": rawHosts,
			"profiles":  rawProfiles,
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}

	if len(preview.Skipped) != 3 {
		t.Fatalf("preview.Skipped = %v, want 3 entries (bad host, bad key, bad profile)", preview.Skipped)
	}

	byKey := make(map[string]ImportChange, len(preview.Changes))
	for _, c := range preview.Changes {
		byKey[c.Key] = c
	}
	if got := byKey["ssh_host:hgood"].Action; got != "add" {
		t.Errorf(`Changes["ssh_host:hgood"].Action = %q, want "add" (rest of file must still import)`, got)
	}
	if got := byKey["ssh_key:kgood"].Action; got != "add" {
		t.Errorf(`Changes["ssh_key:kgood"].Action = %q, want "add"`, got)
	}
	if got := byKey["profile:pgood"].Action; got != "add" {
		t.Errorf(`Changes["profile:pgood"].Action = %q, want "add"`, got)
	}
	if _, ok := byKey["ssh_host:"]; ok {
		t.Fatal("malformed host (missing id) produced a change entry; it must be skipped, not applied")
	}
}

// TestPreviewConfigImport_MalformedScalarSkippedRestContinues extends the
// same "skip and continue" rule to a scalar preference whose value doesn't
// match its expected type — e.g. terminal_font_size as a string instead of
// a number, which would panic or silently coerce downstream if it reached
// ApplyConfigImport unchecked.
func TestPreviewConfigImport_MalformedScalarSkippedRestContinues(t *testing.T) {
	a := newImportTestApp(t)
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"terminal_font_size": json.RawMessage(`"not-a-number"`),
			"terminal_theme":     json.RawMessage(`"dark"`),
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	if len(preview.Skipped) != 1 || !strings.Contains(preview.Skipped[0], "terminal_font_size") {
		t.Fatalf("preview.Skipped = %v, want exactly one entry mentioning terminal_font_size", preview.Skipped)
	}
	found := false
	for _, c := range preview.Changes {
		if c.Key == "terminal_theme" {
			found = true
			if c.Action != "add" {
				t.Errorf("terminal_theme change action = %q, want %q", c.Action, "add")
			}
		}
	}
	if !found {
		t.Fatal("terminal_theme change missing; a malformed sibling key must not block it")
	}
}

// TestPreviewConfigImport_UnknownPreferenceKeySkipped pins that a
// preference key this build doesn't recognize (e.g. a hand-edited file, or
// one from a hypothetical future version-1 revision) is reported through
// Skipped rather than silently ignored or applied as-is.
func TestPreviewConfigImport_UnknownPreferenceKeySkipped(t *testing.T) {
	a := newImportTestApp(t)
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"some_future_key": json.RawMessage(`"whatever"`),
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	if len(preview.Changes) != 0 {
		t.Fatalf("preview.Changes = %v, want none", preview.Changes)
	}
	if len(preview.Skipped) != 1 || !strings.Contains(preview.Skipped[0], "some_future_key") {
		t.Fatalf("preview.Skipped = %v, want exactly one entry naming some_future_key", preview.Skipped)
	}
}

// TestPreviewConfigImport_Deterministic pins brief bullet 5: Preview's
// output order does not depend on Go's map iteration order.
//
// An earlier version of this test ran Preview twice on the same input and
// compared the two ImportPreview values with reflect.DeepEqual. That is a
// much weaker check than it looks: export.Preferences is a small map (a
// handful of keys), and for small maps Go's runtime iterates in a rotation
// of one fixed-per-call order rather than a full random permutation — so
// two range loops over the same small map agree far more often than chance
// would suggest. Measured against a build with the `sort.Strings(keys)`
// call deleted from PreviewConfigImport, the two-run comparison caught the
// regression only ~21/30 times.
//
// Pinning a single run's key order against a hand-computed expected
// sequence is better (it stops "two wrong runs that happen to agree" from
// passing) but is still not reliable enough on its own: measured against
// the same sort-removed build, a single PreviewConfigImport call already
// happens to come out in the correct order often enough (~14/30 = 47% miss
// rate across independent process runs) that one assertion is still a coin
// flip. Go's small-map iteration only rotates a fixed per-process order
// rather than fully shuffling it, so for a handful of keys "already sorted"
// is a disturbingly common rotation.
//
// The fix is to call PreviewConfigImport many times in a loop and require
// every single call to match — each call draws its own independent random
// starting rotation (confirmed by the two-run comparison above disagreeing
// most of the time), so requiring N independent draws to all coincidentally
// land on the correct order drives the miss probability down to roughly
// 0.53^N, which is negligible well before N=20.
func TestPreviewConfigImport_Deterministic(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"}}
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "default"}}
	cfg.DefaultProfileID = "p1"
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rawHosts, _ := json.Marshal(sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},   // unchanged
		{ID: "h3", Host: "three.example.com", User: "root", AuthKind: "password"}, // add
	}})
	rawProfiles, _ := json.Marshal(profilesExportPayload{
		Profiles: []SessionProfile{
			{ID: "p1", Name: "default"},           // unchanged
			{ID: "p4", Name: "brand-new-profile"}, // add
		},
		DefaultProfileID: "p1", // unchanged
	})
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"locale_preference": json.RawMessage(`"zh-CN"`),
			"terminal_theme":    json.RawMessage(`"dark"`),
			"profiles":          rawProfiles,
			"ssh_hosts":         rawHosts,
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	wantKeys := []string{
		"locale_preference",
		"profile:p1",
		"profile:p4",
		"profiles:default_profile_id",
		"ssh_host:h1",
		"ssh_host:h3",
		"terminal_theme",
	}

	// 20 independent calls, each must land on exactly wantKeys. See the
	// doc comment above for why a single call isn't a reliable enough
	// witness on its own.
	for i := 0; i < 20; i++ {
		preview, err := a.PreviewConfigImport(string(data))
		if err != nil {
			t.Fatalf("PreviewConfigImport (call %d): %v", i, err)
		}
		gotKeys := make([]string, len(preview.Changes))
		for j, c := range preview.Changes {
			gotKeys[j] = c.Key
		}
		if !reflect.DeepEqual(gotKeys, wantKeys) {
			t.Fatalf("call %d: preview.Changes key order = %v, want exactly %v", i, gotKeys, wantKeys)
		}
	}
}

// TestPreviewConfigImport_EmptyPreviewNeverMarshalsNull pins MAJOR 4: "no
// changes, nothing skipped" is the ordinary, most common outcome (import an
// export you just took from this same machine), not an edge case — so both
// slices must be non-nil so they marshal to `[]`, not `null`. A frontend
// that does `preview.changes.map(...)` on a null value throws; this is the
// same convention PreviewSSHConfigImport (ssh_config_import.go) already
// follows for the same reason.
func TestPreviewConfigImport_EmptyPreviewNeverMarshalsNull(t *testing.T) {
	a := newImportTestApp(t)
	export := ConfigExport{
		Version:     configExportVersion,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	if preview.Changes == nil || preview.Skipped == nil {
		t.Fatalf("PreviewConfigImport on an empty file returned nil slice(s): %+v, want non-nil empty", preview)
	}

	out, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	if strings.Contains(string(out), "null") {
		t.Fatalf("empty ImportPreview marshaled with a null field: %s", out)
	}
	want := `{"changes":[],"skipped":[]}`
	if string(out) != want {
		t.Fatalf("empty ImportPreview marshaled as %s, want %s", out, want)
	}
}

// TestPreviewConfigImport_ProfileEnvPreservedWhenLocalOnly pins MAJOR 2: the
// default export (includeLocalEnv=false, BuildConfigExport's stripUnsyncedEnv)
// strips Env from any SyncEnv==false profile before it ever leaves the
// machine. Comparing that stripped file entry against the local profile
// verbatim would report "replace" for exactly the profiles holding
// local-only data (e.g. secrets in env vars) — implying Preview would wipe
// them, when in fact mergeProfiles/ApplyConfigImport carries the local Env
// forward untouched for SyncEnv==false profiles. Preview must predict that
// outcome, not the raw file diff: same name/shell/etc, Env stripped from
// the file, SyncEnv==false -> "unchanged".
func TestPreviewConfigImport_ProfileEnvPreservedWhenLocalOnly(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default", Shell: "/bin/zsh", SyncEnv: false, Env: map[string]string{"TOKEN": "secret-local-value"}},
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(false) // includeLocalEnv=false: default, strips Env for SyncEnv==false profiles
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	var got *ImportChange
	for i := range preview.Changes {
		if preview.Changes[i].Key == "profile:p1" {
			got = &preview.Changes[i]
		}
	}
	if got == nil {
		t.Fatal("no ImportChange for profile:p1")
	}
	if got.Action != "unchanged" {
		t.Fatalf(`profile:p1 Action = %q, Detail = %q, want "unchanged" — stripped Env on a SyncEnv==false profile must not be reported as a data-losing replace`, got.Action, got.Detail)
	}
}

// TestPreviewConfigImport_ProfileEnvReplaceWhenGenuinelyDifferent is the
// companion to the test above: when the incoming Env for a SyncEnv==false
// profile is not merely absent but genuinely different from local (e.g. the
// export was taken with includeLocalEnv=true from a different machine),
// Preview must still report "replace" and name "env" in Detail — the
// MAJOR 2 fix narrows the false positive, it does not silence real diffs.
func TestPreviewConfigImport_ProfileEnvReplaceWhenGenuinelyDifferent(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default", Shell: "/bin/zsh", SyncEnv: false, Env: map[string]string{"TOKEN": "local-value"}},
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rawProfiles, _ := json.Marshal(profilesExportPayload{Profiles: []SessionProfile{
		{ID: "p1", Name: "default", Shell: "/bin/zsh", SyncEnv: false, Env: map[string]string{"TOKEN": "different-value-from-another-machine"}},
	}})
	export := ConfigExport{
		Version:     configExportVersion,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{"profiles": rawProfiles},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	var got *ImportChange
	for i := range preview.Changes {
		if preview.Changes[i].Key == "profile:p1" {
			got = &preview.Changes[i]
		}
	}
	if got == nil {
		t.Fatal("no ImportChange for profile:p1")
	}
	if got.Action != "replace" {
		t.Fatalf(`profile:p1 Action = %q, want "replace" (Env genuinely differs)`, got.Action)
	}
	if !strings.Contains(got.Detail, "env") {
		t.Fatalf("profile:p1 Detail = %q, want it to name \"env\" as a differing field", got.Detail)
	}
}

// TestPreviewConfigImport_InvalidDefaultShellSkipped pins MAJOR 5: Preview
// must not promise a "replace" that ApplyConfigImport would then refuse.
// SetDefaultShell rejects a shell that isn't on PATH (validateDefaultShell);
// Preview runs that exact same check and reports the offending entry
// through Skipped instead of Changes.
func TestPreviewConfigImport_InvalidDefaultShellSkipped(t *testing.T) {
	a := newImportTestApp(t)
	export := ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"default_shell": json.RawMessage(`"/definitely/not/a/real/shell/on/this/machine"`),
		},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	for _, c := range preview.Changes {
		if c.Key == "default_shell" {
			t.Fatalf("default_shell reported as a change (%+v) for a shell not on PATH; ApplyConfigImport would refuse this", c)
		}
	}
	if len(preview.Skipped) != 1 || !strings.Contains(preview.Skipped[0], "default_shell") {
		t.Fatalf("preview.Skipped = %v, want exactly one entry naming default_shell", preview.Skipped)
	}
}

// TestPreviewConfigImport_InvalidShortcutBindingSkipsWholeMap pins the other
// half of MAJOR 5: SetShortcutBindings rejects the ENTIRE incoming map on
// one malformed binding (not a per-entry filter), so Preview must mirror
// that all-or-nothing behavior — one bad binding must skip the whole
// shortcut_bindings key, not just the bad entry within it.
func TestPreviewConfigImport_InvalidShortcutBindingSkipsWholeMap(t *testing.T) {
	a := newImportTestApp(t)
	rawBindings, _ := json.Marshal(map[string]string{
		"copy":  "Mod+KeyC",
		"paste": "not-a-valid-binding",
	})
	export := ConfigExport{
		Version:     configExportVersion,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{"shortcut_bindings": rawBindings},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	preview, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	for _, c := range preview.Changes {
		if c.Key == "shortcut_bindings" {
			t.Fatalf("shortcut_bindings reported as a change (%+v) despite containing a malformed binding; ApplyConfigImport would refuse the whole map", c)
		}
	}
	if len(preview.Skipped) != 1 || !strings.Contains(preview.Skipped[0], "shortcut_bindings") {
		t.Fatalf("preview.Skipped = %v, want exactly one entry naming shortcut_bindings", preview.Skipped)
	}
}

// TestPreviewConfigImport_MalformedJSONRefused pins the base case: text that
// isn't even valid JSON is refused with an error, not treated as an empty
// file.
func TestPreviewConfigImport_MalformedJSONRefused(t *testing.T) {
	a := newImportTestApp(t)
	before := storeSnapshot(t, a)
	_, err := a.PreviewConfigImport("{not json")
	if err == nil {
		t.Fatal("PreviewConfigImport(\"{not json\"): err = nil, want an error")
	}
	assertStoreUnchanged(t, a, before)
}

// --- ApplyConfigImport (task 3) ---
//
// Everything below needs a real *prefssync.Engine wired to the isolated
// config store, not the bare cfgStore-only App newImportTestApp builds:
// task 3's whole point is that ApplyConfigImport writes through the App
// setters, which mark PrefsMeta dirty via engine.MarkDirty and enqueue a
// push via enqueueSync -- neither of which happens with a.prefsSync == nil.
// applyPushRelay stands in for the network so no test here makes a real
// HTTP call, while still exercising the real Engine.MarkDirty/Push/
// enqueueSync/coalescing code paths exactly as production does.

// applyPushRelay is a prefssync.RelayClient double that never talks to a
// real relay. Put always fails (returns an error, no items): this is
// deliberate, not an oversight -- if Put "succeeded" here, Engine.Push would
// clear the very Dirty flags TestApplyConfigImport_WritesThroughSettersMarksDirty
// asserts on, racing that assertion against the async sync loop for no
// reason relevant to what these tests check. Put still counts every call it
// receives, which is all TestApplyConfigImport_CoalescesIntoOnePush needs.
type applyPushRelay struct {
	mu    sync.Mutex
	calls int

	// delay holds each Put call up before it returns, mirroring
	// fakePrefsSyncEngine.callDelay in prefs_sync_loop_test.go -- it exists
	// so a test can deterministically keep one push "in flight" while more
	// work is enqueued behind it.
	delay time.Duration
	// started, if non-nil, receives once per Put call right as it begins
	// (before delay), so a test can know a push has actually started
	// instead of guessing with a sleep.
	started chan struct{}
}

func (f *applyPushRelay) Get(ctx context.Context) ([]prefssync.ServerItem, error) {
	return nil, nil
}

func (f *applyPushRelay) Put(ctx context.Context, items []prefssync.ClientItem) ([]prefssync.ServerItem, error) {
	f.mu.Lock()
	f.calls++
	delay := f.delay
	f.mu.Unlock()

	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil, errors.New("applyPushRelay: Put refused (test double, no real relay)")
}

func (f *applyPushRelay) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newApplyTestApp builds an App wired to a REAL *prefssync.Engine (the same
// constructor production code uses -- see app.go's startup) over an
// isolated config store and applyPushRelay, with the serial sync loop
// actually running. This is what lets these tests assert the real
// consequences of going through the App setters: PrefsMeta actually flips
// Dirty (via the real appConfigAdapter/Engine.MarkDirty), and a real push is
// actually enqueued and coalesced by the real enqueueSync.
func newApplyTestApp(t *testing.T) (*App, *applyPushRelay) {
	t.Helper()
	useIsolatedKeyring(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfgStore := newTestConfigStore(t)
	relay := &applyPushRelay{}
	adapter := newAppConfigAdapter(cfgStore, func() []byte { return nil })
	a := &App{ctx: ctx, cfgStore: cfgStore, prefsSync: prefssync.NewEngine(adapter, relay)}
	a.startPrefsSyncLoop()
	t.Cleanup(func() {
		cancel()
		select {
		case <-a.prefsSyncLoopDone:
		case <-time.After(time.Second):
		}
	})
	return a, relay
}

// mustMarshalExport is a small json.Marshal-and-check helper shared by the
// Apply tests below, to keep each test's own export construction readable.
func mustMarshalExport(t *testing.T, export ConfigExport) string {
	t.Helper()
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	return string(data)
}

// TestApplyConfigImport_WritesThroughSettersMarksDirty pins spec §4 /
// brief bullet 1 directly: every category ApplyConfigImport can write
// (a scalar pref, an ssh host, a profile + its default) ends up in the
// store AND with its sync key's PrefsMeta marked Dirty. Writing straight to
// cfgStore (bypassing the setters) would change the store's value while
// leaving Dirty false forever -- see
// TestApplyConfigImport_DirectCfgStoreWriteFailsDirtyAssertion below for the
// mutation that proves this test actually catches that.
func TestApplyConfigImport_WritesThroughSettersMarksDirty(t *testing.T) {
	a, _ := newApplyTestApp(t)

	fileHosts := sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
	}}
	fileProfiles := profilesExportPayload{
		Profiles:         []SessionProfile{{ID: "p1", Name: "default"}},
		DefaultProfileID: "p1",
	}
	rawHosts, _ := json.Marshal(fileHosts)
	rawProfiles, _ := json.Marshal(fileProfiles)
	jsonText := mustMarshalExport(t, ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"terminal_theme": json.RawMessage(`"nord"`),
			"ssh_hosts":      rawHosts,
			"profiles":       rawProfiles,
		},
	})

	report, err := a.ApplyConfigImport(jsonText, false)
	if err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}
	if len(report.Applied) == 0 {
		t.Fatal("report.Applied is empty; test setup produced no diffs to prove anything against")
	}

	cfg := a.cfgStore.Get()

	if cfg.TerminalTheme != "nord" {
		t.Fatalf("cfg.TerminalTheme = %q, want %q", cfg.TerminalTheme, "nord")
	}
	if m := cfg.PrefsMeta["terminal_theme"]; !m.Dirty {
		t.Fatalf("PrefsMeta[%q].Dirty = false after ApplyConfigImport, want true -- SetTerminalTheme must mark it dirty", "terminal_theme")
	}

	if len(cfg.SSHHosts) != 1 || cfg.SSHHosts[0].ID != "h1" {
		t.Fatalf("cfg.SSHHosts = %+v, want [h1]", cfg.SSHHosts)
	}
	if m := cfg.PrefsMeta["ssh_hosts_encrypted"]; !m.Dirty {
		t.Fatalf("PrefsMeta[%q].Dirty = false after ApplyConfigImport, want true -- markSSHHostsDirty must run", "ssh_hosts_encrypted")
	}

	if len(cfg.Profiles) != 1 || cfg.Profiles[0].ID != "p1" || cfg.DefaultProfileID != "p1" {
		t.Fatalf("cfg.Profiles/DefaultProfileID = %+v/%q, want [p1]/p1", cfg.Profiles, cfg.DefaultProfileID)
	}
	if m := cfg.PrefsMeta["profiles_encrypted"]; !m.Dirty {
		t.Fatalf("PrefsMeta[%q].Dirty = false after ApplyConfigImport, want true -- SetProfiles/SetDefaultProfileID must mark it dirty", "profiles_encrypted")
	}
}

// TestApplyConfigImport_ReparsesRawTextSameInputSameDecisions pins brief
// bullet 5 / spec §5: Apply does its own parse of jsonText rather than
// reusing a handle from a prior Preview call, so calling Preview and then
// Apply on the *same bytes* must reach the same add/replace decisions --
// there is no shared cached parse for the two to silently drift apart on.
// Run twice on fresh, independently-built apps against the exact same
// jsonText to show the decisions (and their order) are a pure function of
// the input, not of some hidden handle state.
func TestApplyConfigImport_ReparsesRawTextSameInputSameDecisions(t *testing.T) {
	buildJSON := func(t *testing.T) (string, *App) {
		a, _ := newApplyTestApp(t)
		cfg := a.cfgStore.Get()
		cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"}}
		cfg.Profiles = []SessionProfile{{ID: "p1", Name: "default"}}
		if err := a.cfgStore.Set(cfg); err != nil {
			t.Fatalf("Set: %v", err)
		}
		rawHosts, _ := json.Marshal(sshHostsExportPayload{Hosts: []SSHHost{
			{ID: "h1", Host: "one.example.com", User: "deploy", AuthKind: "password"}, // replace
			{ID: "h2", Host: "two.example.com", User: "root", AuthKind: "password"},   // add
		}})
		rawProfiles, _ := json.Marshal(profilesExportPayload{Profiles: []SessionProfile{
			{ID: "p1", Name: "default"},   // unchanged
			{ID: "p2", Name: "brand-new"}, // add
		}})
		jsonText := mustMarshalExport(t, ConfigExport{
			Version:    configExportVersion,
			ExportedAt: "2026-08-21T00:00:00Z",
			AppVersion: "0.4.0",
			Preferences: map[string]json.RawMessage{
				"locale_preference": json.RawMessage(`"zh-CN"`),
				"ssh_hosts":         rawHosts,
				"profiles":          rawProfiles,
			},
		})
		return jsonText, a
	}

	// Run 1: Preview only, capture the decisions it promises.
	jsonText1, previewApp := buildJSON(t)
	preview, err := previewApp.PreviewConfigImport(jsonText1)
	if err != nil {
		t.Fatalf("PreviewConfigImport: %v", err)
	}
	wantByKey := make(map[string]string, len(preview.Changes))
	for _, c := range preview.Changes {
		wantByKey[c.Key] = c.Action
	}

	// Run 2: a completely independent app, seeded identically, jsonText
	// built the same way (a fresh string, not the same Go value as run 1's)
	// -- Apply must reach the exact same per-key decisions Preview promised
	// in run 1, proving it is re-deriving from the bytes, not trusting
	// anything cached from a prior Preview call it never even sees here.
	jsonText2, applyApp := buildJSON(t)
	if jsonText1 != jsonText2 {
		t.Fatalf("test setup non-deterministic: jsonText1 != jsonText2")
	}
	report, err := applyApp.ApplyConfigImport(jsonText2, false)
	if err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}

	gotByKey := make(map[string]string, len(report.Applied))
	for _, c := range report.Applied {
		gotByKey[c.Key] = c.Action
	}
	for key, wantAction := range wantByKey {
		if wantAction == "unchanged" {
			continue // Apply never writes (or reports) unchanged entries
		}
		if gotByKey[key] != wantAction {
			t.Errorf("report.Applied[%q].Action = %q, want %q (Preview's own decision for the same bytes)", key, gotByKey[key], wantAction)
		}
	}
	for key := range gotByKey {
		if _, ok := wantByKey[key]; !ok {
			t.Errorf("report.Applied contains key %q that Preview never classified", key)
		}
	}

	// And the actual written state matches what was previewed: h1 replaced
	// with the file's data, h2 added, p1 unchanged (still "default"), p2
	// added.
	cfg := applyApp.cfgStore.Get()
	hostsByID := make(map[string]SSHHost, len(cfg.SSHHosts))
	for _, h := range cfg.SSHHosts {
		hostsByID[h.ID] = h
	}
	if hostsByID["h1"].User != "deploy" {
		t.Fatalf("h1.User = %q, want %q (replace)", hostsByID["h1"].User, "deploy")
	}
	if _, ok := hostsByID["h2"]; !ok {
		t.Fatal("h2 missing after apply, want it added")
	}
}

// TestApplyConfigImport_CoalescesIntoOnePush pins brief bullet 3 / spec §4's
// coalescing half directly: ApplyConfigImport touches several distinct
// synced keys (several scalar prefs, an ssh host, a profile+default) in one
// call, each through its own setter -- and relies on enqueueSync's existing
// coalescing (prefs_sync_loop.go) to collapse that burst into at most one
// extra push round trip, rather than pushing once per key or implementing
// its own batching.
//
// To make "at most one extra push" a deterministic assertion instead of a
// scheduler-dependent one, this test primes a push that is ALREADY in
// flight (delayed inside applyPushRelay.Put) before calling
// ApplyConfigImport at all -- the exact technique
// TestSyncLoopCoalescesWhileInFlight (prefs_sync_loop_test.go) uses to pin
// the underlying mechanism. Because the primed push is guaranteed in flight
// first, every one of ApplyConfigImport's own setter-driven enqueueSync
// calls is guaranteed to land in the coalescing window, not race the sync
// loop goroutine's own scheduling. Total Put calls once everything settles
// must be exactly 2: the one primed call, plus ApplyConfigImport's five (or
// more) setter calls all coalesced into the single pending slot behind it --
// not 6. If ApplyConfigImport ever stopped relying on that coalescing --
// say, by calling into the sync engine directly once per key instead of
// going through enqueueSync/markPrefDirtyAndPush -- this test fails for a
// reason that has nothing to do with import's own logic, which is the
// point: coalescing is load-bearing here, not incidental.
func TestApplyConfigImport_CoalescesIntoOnePush(t *testing.T) {
	a, relay := newApplyTestApp(t)
	relay.delay = 150 * time.Millisecond
	relay.started = make(chan struct{}, 1)

	// Prime an in-flight push on a key ApplyConfigImport's own file below
	// never touches, so the two are cleanly attributable.
	if err := a.SetTerminalFontHead("Priming Font"); err != nil {
		t.Fatalf("SetTerminalFontHead (priming): %v", err)
	}
	select {
	case <-relay.started:
	case <-time.After(2 * time.Second):
		t.Fatal("primed push never started")
	}

	fileHosts := sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
	}}
	fileProfiles := profilesExportPayload{
		Profiles:         []SessionProfile{{ID: "p1", Name: "default"}},
		DefaultProfileID: "p1",
	}
	rawHosts, _ := json.Marshal(fileHosts)
	rawProfiles, _ := json.Marshal(fileProfiles)
	jsonText := mustMarshalExport(t, ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"locale_preference":     json.RawMessage(`"zh-CN"`),
			"terminal_theme":        json.RawMessage(`"nord"`),
			"notifications_enabled": json.RawMessage(`true`),
			"ai_notifications_only": json.RawMessage(`true`),
			"ssh_hosts":             rawHosts,
			"profiles":              rawProfiles,
		},
	})

	report, err := a.ApplyConfigImport(jsonText, false)
	if err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}
	// Sanity: this import really did touch several distinct keys -- if it
	// only changed one, "not one push per key" would be trivially true for
	// the wrong reason.
	if len(report.Applied) < 5 {
		t.Fatalf("report.Applied = %+v, want at least 5 entries (test setup should give Apply real batching work)", report.Applied)
	}

	waitFor(t, 3*time.Second, "primed + coalesced pushes to both finish", func() bool {
		return relay.callCount() >= 2
	})
	// Settle: give a wrongly-uncoalesced extra Put call time to land before
	// the final count assertion below.
	time.Sleep(300 * time.Millisecond)

	if got := relay.callCount(); got != 2 {
		t.Fatalf("relay Put calls = %d, want exactly 2 (1 primed + 1 coalesced covering all %d applied keys) -- ApplyConfigImport pushed once per key instead of relying on enqueueSync's coalescing", got, len(report.Applied))
	}
}

// TestApplyConfigImport_LocalOnlyKept mirrors
// TestPreviewConfigImport_MergeByID_AddReplaceAndKeepLocal for the write
// side: a host/key/profile that exists only locally (absent from the file)
// must survive ApplyConfigImport untouched -- Preview reports no change for
// it (nothing in Changes even names it), and Apply, driven off exactly
// those Changes, has no path that could delete it.
func TestApplyConfigImport_LocalOnlyKept(t *testing.T) {
	a, _ := newApplyTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
		{ID: "h2", Host: "two.example.com", User: "root", AuthKind: "password"}, // local-only
	}
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default"},
		{ID: "p2", Name: "staging"}, // local-only
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fileHosts := sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "deploy", AuthKind: "password"}, // replace
		{ID: "h3", Host: "three.example.com", User: "root", AuthKind: "password"}, // add
	}}
	fileProfiles := profilesExportPayload{Profiles: []SessionProfile{
		{ID: "p1", Name: "default"},           // unchanged
		{ID: "p4", Name: "brand-new-profile"}, // add
	}}
	rawHosts, _ := json.Marshal(fileHosts)
	rawProfiles, _ := json.Marshal(fileProfiles)
	jsonText := mustMarshalExport(t, ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"ssh_hosts": rawHosts,
			"profiles":  rawProfiles,
		},
	})

	if _, err := a.ApplyConfigImport(jsonText, false); err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}

	cfg = a.cfgStore.Get()

	hostsByID := make(map[string]SSHHost, len(cfg.SSHHosts))
	for _, h := range cfg.SSHHosts {
		hostsByID[h.ID] = h
	}
	if h, ok := hostsByID["h2"]; !ok || h.Host != "two.example.com" {
		t.Fatalf("local-only host h2 missing or changed after apply: %+v", hostsByID)
	}
	if h, ok := hostsByID["h1"]; !ok || h.User != "deploy" {
		t.Fatalf("h1 not replaced with file data: %+v", hostsByID)
	}
	if _, ok := hostsByID["h3"]; !ok {
		t.Fatal("new host h3 not added")
	}

	profilesByID := make(map[string]SessionProfile, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profilesByID[p.ID] = p
	}
	if p, ok := profilesByID["p2"]; !ok || p.Name != "staging" {
		t.Fatalf("local-only profile p2 missing or changed after apply: %+v", profilesByID)
	}
	if _, ok := profilesByID["p4"]; !ok {
		t.Fatal("new profile p4 not added")
	}
}

// TestApplyConfigImport_ProfileEnvPreservedWhenLocalOnly is the Apply-side
// companion to the Preview test of the same name: a SyncEnv==false
// profile's local Env must survive an apply whose file entry has no Env at
// all (the ordinary shape of an includeLocalEnv=false export), because
// ApplyConfigImport routes profiles through mergeProfiles exactly like the
// real cross-device pull path does.
func TestApplyConfigImport_ProfileEnvPreservedWhenLocalOnly(t *testing.T) {
	a, _ := newApplyTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default", Shell: "/bin/zsh", SyncEnv: false, Env: map[string]string{"TOKEN": "secret-local-value"}},
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(false) // includeLocalEnv=false strips Env for SyncEnv==false profiles
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	// Give Preview/Apply something else to do too, so this isn't a no-op
	// import that happens to leave Env alone because nothing ran at all.
	export.Preferences["terminal_theme"], _ = json.Marshal("daylight")
	jsonText := mustMarshalExport(t, export)

	if _, err := a.ApplyConfigImport(jsonText, false); err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}

	got := a.cfgStore.Get()
	if len(got.Profiles) != 1 {
		t.Fatalf("Profiles = %+v, want exactly 1", got.Profiles)
	}
	if got.Profiles[0].Env["TOKEN"] != "secret-local-value" {
		t.Fatalf("p1.Env = %+v, want TOKEN=secret-local-value preserved (SyncEnv==false, file carried no Env)", got.Profiles[0].Env)
	}
}

// TestApplyConfigImport_InvalidDefaultShellSkippedNeverPartiallyWrites is
// the Apply-side companion to
// TestPreviewConfigImport_InvalidDefaultShellSkipped: a default_shell value
// SetDefaultShell would refuse must never be written, and -- because Apply
// classifies via the very same Preview call -- it never reaches
// applyScalarPref at all (it is not in preview.Changes to iterate over).
func TestApplyConfigImport_InvalidDefaultShellSkippedNeverPartiallyWrites(t *testing.T) {
	a, _ := newApplyTestApp(t)
	before := a.cfgStore.Get().DefaultShell

	jsonText := mustMarshalExport(t, ConfigExport{
		Version:    configExportVersion,
		ExportedAt: "2026-08-21T00:00:00Z",
		AppVersion: "0.4.0",
		Preferences: map[string]json.RawMessage{
			"default_shell": json.RawMessage(`"/definitely/not/a/real/shell/on/this/machine"`),
		},
	})

	report, err := a.ApplyConfigImport(jsonText, false)
	if err != nil {
		t.Fatalf("ApplyConfigImport: %v", err)
	}
	for _, c := range report.Applied {
		if c.Key == "default_shell" {
			t.Fatalf("default_shell reported as applied: %+v; SetDefaultShell would have refused this value", c)
		}
	}
	if len(report.Skipped) != 1 || !strings.Contains(report.Skipped[0], "default_shell") {
		t.Fatalf("report.Skipped = %v, want exactly one entry naming default_shell", report.Skipped)
	}
	if got := a.cfgStore.Get().DefaultShell; got != before {
		t.Fatalf("cfg.DefaultShell = %q, want unchanged %q", got, before)
	}
}

// TestApplyConfigImport_UnknownVersionRefusedNothingWritten mirrors
// TestPreviewConfigImport_RejectsUnknownVersion on the Apply side: Apply
// calls PreviewConfigImport internally, which refuses an unknown
// atterm_export version outright -- Apply must propagate that refusal
// rather than falling through to writing whatever it could parse.
func TestApplyConfigImport_UnknownVersionRefusedNothingWritten(t *testing.T) {
	a, _ := newApplyTestApp(t)
	before := storeSnapshot(t, a)

	jsonText := mustMarshalExport(t, ConfigExport{
		Version:     configExportVersion + 1,
		ExportedAt:  "2026-08-21T00:00:00Z",
		AppVersion:  "0.4.0",
		Preferences: map[string]json.RawMessage{"locale_preference": json.RawMessage(`"zh-CN"`)},
	})

	report, err := a.ApplyConfigImport(jsonText, false)
	if err == nil {
		t.Fatal("ApplyConfigImport with an unknown version: err = nil, want an error")
	}
	if len(report.Applied) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("ApplyConfigImport on rejected version returned a non-empty report: %+v", report)
	}
	assertStoreUnchanged(t, a, before)
}
