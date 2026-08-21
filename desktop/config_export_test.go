package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/attson/atterm/internal/prefssync"
)

// newExportTestApp builds an App backed by an isolated config store and an
// isolated (file-backed) keyring, so keychainSlot.Save/Load in this test
// never touch the real OS keychain.
func newExportTestApp(t *testing.T) *App {
	t.Helper()
	useIsolatedKeyring(t)
	return &App{cfgStore: newTestConfigStore(t)}
}

// seedSecrets adds one SSH host with a keyring password and one SSH key with
// a keyring private key + passphrase, and returns the exact secret strings
// so tests can assert their absence from the export bytes.
func seedSecrets(t *testing.T, a *App) (host SSHHost, key SSHKey, password, privateKeyPEM, passphrase string) {
	t.Helper()
	privateKeyPEM = testKeyPEM(t)
	const marker = "s3cr3t-passw0rd-marker"
	const passphraseMarker = "s3cr3t-passphrase-marker"

	k, err := a.AddSSHKey("prod-key", privateKeyPEM, "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	// AddSSHKey's own parser (parseKeyType -> ssh.ParseRawPrivateKeyWithPassphrase)
	// refuses a non-empty passphrase against an unencrypted PEM ("ssh: not an
	// encrypted key"), and testKeyPEM's key is unencrypted. The passphrase
	// this test needs to prove never leaks is written directly to the same
	// keyring slot AddSSHKey uses — sshKeySecretSlot(k.ID).Load() reads it
	// back exactly the same way regardless of how it got there, which is all
	// that matters for a leak test.
	if err := sshKeySecretSlot(k.ID).Save(sshKeySecret{PrivateKey: privateKeyPEM, Passphrase: passphraseMarker}); err != nil {
		t.Fatalf("seed passphrase: %v", err)
	}
	h, err := a.AddSSHHost(SSHHost{
		Alias: "prod", Host: "prod.example.com", Port: "22", User: "root", AuthKind: "password",
	}, sshCredential{Password: marker})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}
	return h, k, marker, privateKeyPEM, passphraseMarker
}

// pemBodyLine returns one full-width base64 line from the middle of a PEM's
// body — a marker that survives JSON escaping unchanged, unlike the PEM
// string as a whole. json.Marshal renders every newline in a Go string as
// the two bytes `\n`, so a multi-line PEM compared verbatim (or a slice of
// it spanning a newline) against JSON-encoded output can never match even
// when the key has leaked; a single line has no newline in it to escape.
func pemBodyLine(t *testing.T, pemStr string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(pemStr), "\n")
	if len(lines) < 5 {
		t.Fatalf("PEM too short to extract a reliable body line: %d lines", len(lines))
	}
	// Line 0 is "-----BEGIN ...-----" and the last is "-----END ...-----";
	// both can be short. The line halfway through the body is guaranteed to
	// be a full 64-char base64 row for any RSA key this test generates.
	return lines[len(lines)/2]
}

// TestBuildConfigExport_NeverContainsCredentialBytes is the load-bearing
// test for this task: it seeds a keyring password, a keyring private key,
// and a keyring passphrase, exports, and asserts the exact secret bytes are
// absent from the serialized output — not merely absent from a struct
// field, since a future field addition to ConfigExport must fail this test
// rather than slip a credential through silently.
//
// The private-key assertion is intentionally NOT "does data contain the raw
// multi-line PEM" or a slice of it spanning a newline — json.Marshal escapes
// every `\n` to the two bytes `\`+`n`, so that comparison is vacuously true
// (never matches) whether or not the key leaked, and would let a real leak
// through silently. Assert on a single newline-free PEM body line, which
// survives JSON escaping unchanged, and on the exact JSON-escaped form of
// the whole PEM, which is what a real leak looks like in this file.
func TestBuildConfigExport_NeverContainsCredentialBytes(t *testing.T) {
	a := newExportTestApp(t)
	_, _, password, privateKeyPEM, passphrase := seedSecrets(t, a)

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	if strings.Contains(string(data), password) {
		t.Fatalf("export bytes contain the SSH host password: %s", data)
	}
	if strings.Contains(string(data), passphrase) {
		t.Fatalf("export bytes contain the SSH key passphrase: %s", data)
	}

	bodyLine := pemBodyLine(t, privateKeyPEM)
	if strings.Contains(string(data), bodyLine) {
		t.Fatalf("export bytes contain a line of the SSH private key body: %s", data)
	}

	escapedPEM, err := json.Marshal(privateKeyPEM)
	if err != nil {
		t.Fatalf("marshal privateKeyPEM: %v", err)
	}
	escapedInner := string(escapedPEM[1 : len(escapedPEM)-1]) // strip the surrounding quotes json.Marshal adds
	if strings.Contains(string(data), escapedInner) {
		t.Fatalf("export bytes contain the JSON-escaped SSH private key: %s", data)
	}
}

// TestBuildConfigExport_HostsAndKeysUnderUnsealedNames pins requirement #3:
// the ssh_hosts/profiles preference keys are written under their unsealed
// names, and the "_encrypted" transport-detail suffix never appears in the
// output at all.
func TestBuildConfigExport_HostsAndKeysUnderUnsealedNames(t *testing.T) {
	a := newExportTestApp(t)
	h, k, _, _, _ := seedSecrets(t, a)

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}

	rawHosts, ok := export.Preferences["ssh_hosts"]
	if !ok {
		t.Fatal(`Preferences["ssh_hosts"] missing`)
	}
	if _, bad := export.Preferences["ssh_hosts_encrypted"]; bad {
		t.Fatal(`Preferences contains "ssh_hosts_encrypted"; export must use the unsealed name`)
	}
	var hostsPayload sshHostsExportPayload
	if err := json.Unmarshal(rawHosts, &hostsPayload); err != nil {
		t.Fatalf("unmarshal ssh_hosts: %v", err)
	}
	if len(hostsPayload.Hosts) != 1 || hostsPayload.Hosts[0].ID != h.ID {
		t.Fatalf("unexpected hosts payload: %+v", hostsPayload)
	}
	if len(hostsPayload.Keys) != 1 || hostsPayload.Keys[0].ID != k.ID {
		t.Fatalf("unexpected keys payload: %+v", hostsPayload)
	}

	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if strings.Contains(string(data), "_encrypted") {
		t.Fatalf("export bytes contain the transport-detail suffix _encrypted: %s", data)
	}
	if _, bad := export.Preferences["profiles_encrypted"]; bad {
		t.Fatal(`Preferences contains "profiles_encrypted"; export must use the unsealed name`)
	}
}

// TestBuildConfigExport_StripsUnsyncedEnvByDefault pins requirement/brief
// bullet: Env is stripped for profiles with SyncEnv: false via
// stripUnsyncedEnv, and restored when includeLocalEnv is true.
func TestBuildConfigExport_StripsUnsyncedEnvByDefault(t *testing.T) {
	a := newExportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "local-only", SyncEnv: false, Env: map[string]string{"SECRET_TOKEN": "abc123"}},
		{ID: "p2", Name: "synced", SyncEnv: true, Env: map[string]string{"PATH_EXTRA": "/opt/bin"}},
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// includeLocalEnv=false: unsynced profile's env must not appear anywhere
	// in the output bytes.
	exportStripped, err := a.BuildConfigExport(false)
	if err != nil {
		t.Fatalf("BuildConfigExport(false): %v", err)
	}
	strippedBytes, _ := json.Marshal(exportStripped)
	if strings.Contains(string(strippedBytes), "abc123") {
		t.Fatalf("includeLocalEnv=false: unsynced Env leaked: %s", strippedBytes)
	}
	if !strings.Contains(string(strippedBytes), "/opt/bin") {
		t.Fatalf("includeLocalEnv=false: synced Env missing: %s", strippedBytes)
	}

	// includeLocalEnv=true: both profiles' Env are present verbatim.
	exportFull, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport(true): %v", err)
	}
	fullBytes, _ := json.Marshal(exportFull)
	if !strings.Contains(string(fullBytes), "abc123") {
		t.Fatalf("includeLocalEnv=true: unsynced Env missing: %s", fullBytes)
	}
	if !strings.Contains(string(fullBytes), "/opt/bin") {
		t.Fatalf("includeLocalEnv=true: synced Env missing: %s", fullBytes)
	}
}

// TestBuildConfigExport_FreshStoreHasNoPreferences pins the brief's bullet
// against the strong reading MAJOR 2 established: "no value" isn't just a
// nil pointer or an empty slice, it's also a scalar sitting at its zero
// value. isPrefCustomized's own comment says "zero/empty is the raw-field
// signal for never set" — on a fresh store, EVERY synced key (not just the
// pointer/slice-typed ones) must be absent, including the always-non-nil
// string/int ones like locale_preference and terminal_font_size that a
// naive "ok" or "not null" check would let through as "" / 0.
func TestBuildConfigExport_FreshStoreHasNoPreferences(t *testing.T) {
	a := newExportTestApp(t)
	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	if len(export.Preferences) != 0 {
		t.Fatalf("Preferences on a fresh store = %v, want empty", export.Preferences)
	}
}

// TestBuildConfigExport_CustomizedScalarsAppear is the positive half of
// TestBuildConfigExport_FreshStoreHasNoPreferences: once a scalar preference
// is explicitly set — including to a value indistinguishable from "unset" by
// type alone, like terminal_font_size:0 chosen on purpose vs. never touched
// — isPrefCustomized can't tell the difference and the field is exported.
// What it CAN tell apart, and what this pins, is string/int fields moved off
// their true zero value.
func TestBuildConfigExport_CustomizedScalarsAppear(t *testing.T) {
	a := newExportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.LocalePreference = "zh-CN"
	cfg.TerminalTheme = "dark"
	cfg.TerminalFontSize = 14
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	for _, key := range []string{"locale_preference", "terminal_theme", "terminal_font_size"} {
		if _, ok := export.Preferences[key]; !ok {
			t.Errorf("Preferences[%q] missing after being explicitly set", key)
		}
	}
	// Untouched keys stay absent alongside the customized ones.
	if _, ok := export.Preferences["terminal_line_height"]; ok {
		t.Error(`Preferences["terminal_line_height"] present but was never set`)
	}
}

// TestBuildConfigExport_PreferencesNeverContainNull is the narrower
// (MINOR 5) form of the null check: scoped to the Preferences map's own
// bytes rather than the whole export, so a legitimate value that happens to
// contain the substring "null" (a shell path, a template body, a host note)
// can't fail it.
func TestBuildConfigExport_PreferencesNeverContainNull(t *testing.T) {
	a := newExportTestApp(t)
	cfg := a.cfgStore.Get()
	cfg.QuickTemplates = []QuickTemplate{{ID: "t1", Label: "n", Text: "echo hi"}}
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "default"}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	prefsBytes, err := json.Marshal(export.Preferences)
	if err != nil {
		t.Fatalf("marshal Preferences: %v", err)
	}
	if strings.Contains(string(prefsBytes), "null") {
		t.Fatalf("Preferences bytes contain a JSON null: %s", prefsBytes)
	}
}

// TestBuildConfigExport_Metadata pins the envelope fields the brief's
// struct literal specifies (version/timestamp/app version).
func TestBuildConfigExport_Metadata(t *testing.T) {
	a := newExportTestApp(t)
	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}
	if export.Version != configExportVersion {
		t.Errorf("Version = %d, want %d", export.Version, configExportVersion)
	}
	if export.AppVersion != Version {
		t.Errorf("AppVersion = %q, want %q", export.AppVersion, Version)
	}
	if export.ExportedAt == "" {
		t.Error("ExportedAt is empty")
	}
}

// TestBuildConfigExport_HandlesEverySyncedKey (MAJOR 4) pins the set of keys
// BuildConfigExport knows how to classify against prefssync.SyncedKeys().
// Without this, a future synced key backed by a secret field (imagine
// appConfig.LocalAdminPassword riding a hypothetical "local_admin_password"
// synced key) would fall through the adapter loop silently — isPrefCustomized
// defaults an unlisted key to "not customized" (see its switch's default
// case), so it would simply never be classified as secret-bearing or safe,
// one way or the other, and no test would notice. Failing loudly here forces
// a human to add the key to `handled` (and to sshHostsExportPayload-style
// scrutiny) before it can ship.
func TestBuildConfigExport_HandlesEverySyncedKey(t *testing.T) {
	handled := []string{
		"locale_preference",
		"quick_templates",
		"notifications_enabled",
		"ai_notifications_only",
		"command_notify_threshold_seconds",
		"shell_integration_enabled",
		"pinned_session_ids",
		"terminal_theme",
		"terminal_font_head",
		"terminal_font_size",
		"terminal_line_height",
		"terminal_cursor_style",
		"terminal_cursor_blink",
		"terminal_scrollback",
		"default_shell",
		"shortcut_bindings",
		"ssh_hosts_encrypted", // re-homed as "ssh_hosts", never sealed
		"profiles_encrypted",  // re-homed as "profiles", never sealed
	}
	synced := prefssync.SyncedKeys()
	for _, key := range synced {
		if !slices.Contains(handled, key) {
			t.Errorf("prefssync.SyncedKeys() has an unclassified key %q — BuildConfigExport must be taught whether it carries a secret before this test passes", key)
		}
	}
	for _, key := range handled {
		if !slices.Contains(synced, key) {
			t.Errorf("handled list has a stale key %q no longer in prefssync.SyncedKeys() — remove it", key)
		}
	}
}

// The following tests exercise ExportConfig itself through the a.saveDialog
// seam (MAJOR 3): a.saveDialog defaults to wailsruntime.SaveFileDialog,
// which calls log.Fatalf when a.ctx has no bound frontend (see
// getFrontend in wails/v2/pkg/runtime) — fatal in a headless test, so
// a.saveDialog exists specifically so these can substitute a stub and drive
// ExportConfig's actual logic instead of only its collaborators.

func TestExportConfig_WritesExportAtChosenPathWithOwnerOnlyPerm(t *testing.T) {
	a := newExportTestApp(t)
	const chosenPath = "/tmp/atterm-config-test.json"
	a.saveDialog = func(ctx context.Context, opts wailsruntime.SaveDialogOptions) (string, error) {
		return chosenPath, nil
	}
	var gotPath string
	var gotData []byte
	var gotPerm fs.FileMode
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		gotPath, gotData, gotPerm = path, data, perm
		return nil
	}

	got, err := a.ExportConfig(true)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if got != chosenPath {
		t.Errorf("ExportConfig returned %q, want %q", got, chosenPath)
	}
	if gotPath != chosenPath {
		t.Errorf("writeFile path = %q, want %q", gotPath, chosenPath)
	}
	if gotPerm != 0o600 {
		t.Errorf("writeFile perm = %o, want 0600", gotPerm)
	}
	var export ConfigExport
	if err := json.Unmarshal(gotData, &export); err != nil {
		t.Fatalf("written data isn't a ConfigExport: %v", err)
	}
	if export.Version != configExportVersion {
		t.Errorf("written Version = %d, want %d", export.Version, configExportVersion)
	}
}

func TestExportConfig_CancelReturnsEmptyPathNoError(t *testing.T) {
	a := newExportTestApp(t)
	a.saveDialog = func(ctx context.Context, opts wailsruntime.SaveDialogOptions) (string, error) {
		return "", nil // user dismissed the dialog without picking a path
	}
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		t.Fatal("writeFile must not be called when the dialog is cancelled")
		return nil
	}

	got, err := a.ExportConfig(true)
	if err != nil {
		t.Fatalf("ExportConfig on cancel: err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ExportConfig on cancel: path = %q, want \"\"", got)
	}
}

func TestExportConfig_WriteFailurePropagates(t *testing.T) {
	a := newExportTestApp(t)
	wantErr := errors.New("disk full")
	a.saveDialog = func(ctx context.Context, opts wailsruntime.SaveDialogOptions) (string, error) {
		return "/tmp/atterm-config-test.json", nil
	}
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		return wantErr
	}

	got, err := a.ExportConfig(true)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExportConfig write error = %v, want %v", err, wantErr)
	}
	if got != "" {
		t.Fatalf("ExportConfig on write failure: path = %q, want \"\"", got)
	}
}
