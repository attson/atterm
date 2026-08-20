package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
	if preview.Changes != nil || preview.Skipped != nil {
		t.Fatalf("PreviewConfigImport on rejected version returned a non-empty preview: %+v", preview)
	}
	assertStoreUnchanged(t, a, before)
}

// TestPreviewConfigImport_MergeByID_AddReplaceAndKeepLocal pins brief bullet
// 2 in full: same ID -> replace, new ID -> add, and — the explicit
// assertion the brief calls out by name — a local host present ONLY
// locally (absent from the file) produces NO change entry at all. That
// silence is what makes import non-destructive: nothing in Changes ever
// suggests removing it, so applying only Changes could never wipe it.
func TestPreviewConfigImport_MergeByID_AddReplaceAndKeepLocal(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"},
		{ID: "h2", Host: "two.example.com", User: "root", AuthKind: "password"}, // local-only, kept
	}
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "default"},
		{ID: "p2", Name: "staging"}, // local-only, kept
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fileHosts := sshHostsExportPayload{Hosts: []SSHHost{
		{ID: "h1", Host: "one.example.com", User: "deploy", AuthKind: "password"}, // same ID, different data -> replace
		{ID: "h3", Host: "three.example.com", User: "root", AuthKind: "password"}, // new ID -> add
	}}
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
	if got := byKey["profile:p1"].Action; got != "unchanged" {
		t.Errorf(`Changes["profile:p1"].Action = %q, want "unchanged"`, got)
	}
	if got := byKey["profile:p4"].Action; got != "add" {
		t.Errorf(`Changes["profile:p4"].Action = %q, want "add"`, got)
	}

	// The explicit "kept" assertion: h2 and p2 exist locally, are absent
	// from the file, and must not appear in Changes under any key or
	// action — there is no action that would remove them, and nothing here
	// even names them.
	for _, c := range preview.Changes {
		if c.Key == "ssh_host:h2" {
			t.Fatalf("local-only host h2 (absent from file) produced a change entry: %+v — it must be silently kept, not touched", c)
		}
		if c.Key == "profile:p2" {
			t.Fatalf("local-only profile p2 (absent from file) produced a change entry: %+v — it must be silently kept, not touched", c)
		}
	}

	// And the store itself: still exactly what it was, h2/p2 included.
	got := a.cfgStore.Get()
	if len(got.SSHHosts) != 2 || got.SSHHosts[1].ID != "h2" {
		t.Fatalf("SSHHosts after Preview = %+v, want h2 still present untouched", got.SSHHosts)
	}
	if len(got.Profiles) != 2 || got.Profiles[1].ID != "p2" {
		t.Fatalf("Profiles after Preview = %+v, want p2 still present untouched", got.Profiles)
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

// TestPreviewConfigImport_Deterministic pins brief bullet 5: running Preview
// twice on the byte-identical file produces the byte-identical (structurally
// identical) result, in the same order, both times.
func TestPreviewConfigImport_Deterministic(t *testing.T) {
	a := newImportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "one.example.com", User: "root", AuthKind: "password"}}
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "default"}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	export.Preferences["locale_preference"], _ = json.Marshal("zh-CN")
	export.Preferences["terminal_theme"], _ = json.Marshal("dark")
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	first, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport (1st): %v", err)
	}
	second, err := a.PreviewConfigImport(string(data))
	if err != nil {
		t.Fatalf("PreviewConfigImport (2nd): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("PreviewConfigImport is not deterministic:\n1st: %+v\n2nd: %+v", first, second)
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
