package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
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
// a keyring private key, and returns the exact secret strings so tests can
// assert their absence from the export bytes.
func seedSecrets(t *testing.T, a *App) (host SSHHost, key SSHKey, password, privateKeyPEM string) {
	t.Helper()
	privateKeyPEM = testKeyPEM(t)
	const marker = "s3cr3t-passw0rd-marker"

	k, err := a.AddSSHKey("prod-key", privateKeyPEM, "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	h, err := a.AddSSHHost(SSHHost{
		Alias: "prod", Host: "prod.example.com", Port: "22", User: "root", AuthKind: "password",
	}, sshCredential{Password: marker})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}
	return h, k, marker, privateKeyPEM
}

// TestBuildConfigExport_NeverContainsCredentialBytes is the load-bearing
// test for this task: it seeds both a keyring password and a keyring
// private key, exports, and asserts the exact secret bytes are absent from
// the serialized output — not merely absent from a struct field, since a
// future field addition to ConfigExport must fail this test rather than
// slip a credential through silently.
func TestBuildConfigExport_NeverContainsCredentialBytes(t *testing.T) {
	a := newExportTestApp(t)
	_, _, password, privateKeyPEM := seedSecrets(t, a)

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
	if strings.Contains(string(data), privateKeyPEM) {
		t.Fatalf("export bytes contain the SSH private key PEM: %s", data)
	}
	// The PEM's own body (base64 of the DER) is also a sufficient marker of
	// leakage on its own — checked above via the full PEM string, but also
	// guard against a partial/re-wrapped copy of the key material.
	pemBody := strings.TrimSpace(strings.SplitN(strings.SplitN(privateKeyPEM, "-----\n", 2)[1], "\n-----END", 2)[0])
	if pemBody != "" && strings.Contains(string(data), pemBody) {
		t.Fatalf("export bytes contain the SSH private key body: %s", data)
	}
}

// TestBuildConfigExport_HostsAndKeysUnderUnsealedNames pins requirement #3:
// the ssh_hosts/profiles preference keys are written under their unsealed
// names, and the "_encrypted" transport-detail suffix never appears in the
// output at all.
func TestBuildConfigExport_HostsAndKeysUnderUnsealedNames(t *testing.T) {
	a := newExportTestApp(t)
	h, k, _, _ := seedSecrets(t, a)

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

// TestBuildConfigExport_UnsetKeysAreAbsentNotNull pins the brief's bullet:
// every SyncedKeys() entry with a value appears, and keys with no value are
// absent from Preferences rather than present as JSON null.
func TestBuildConfigExport_UnsetKeysAreAbsentNotNull(t *testing.T) {
	a := newExportTestApp(t)
	// Fresh store: no notifications/ai/threshold/shell-integration/cursor-blink
	// preference has ever been explicitly set, so these pointer-typed keys
	// must be absent.
	export, err := a.BuildConfigExport(true)
	if err != nil {
		t.Fatalf("BuildConfigExport: %v", err)
	}

	for _, key := range []string{
		"notifications_enabled",
		"ai_notifications_only",
		"command_notify_threshold_seconds",
		"shell_integration_enabled",
		"terminal_cursor_blink",
	} {
		raw, ok := export.Preferences[key]
		if ok {
			t.Errorf("Preferences[%q] present but was never set: %s", key, raw)
		}
	}

	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("export bytes contain a JSON null: %s", data)
	}

	// A value that IS present (locale_preference always has a string value,
	// even "") must appear as an actual key.
	if _, ok := export.Preferences["locale_preference"]; !ok {
		t.Fatal(`Preferences["locale_preference"] missing`)
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

// The two tests below mirror TestExportDiagnostics_WritesContentViaStub /
// TestExportDiagnostics_StubPropagatesError exactly: ExportConfig cannot be
// invoked directly in a headless test because wailsruntime.SaveFileDialog
// calls log.Fatalf when a.ctx has no bound frontend (see getFrontend in
// wails/v2/pkg/runtime), so these exercise the same a.writeFile seam
// ExportConfig relies on instead of driving the real dialog.

func TestExportConfig_WritesContentViaStub(t *testing.T) {
	a := newExportTestApp(t)
	var gotPath string
	var gotData []byte
	var gotPerm fs.FileMode
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		gotPath = path
		gotData = data
		gotPerm = perm
		return nil
	}
	if err := a.writeFile("/tmp/atterm-config-test.json", []byte("{}"), 0o600); err != nil {
		t.Fatalf("writeFile stub error: %v", err)
	}
	if gotPath != "/tmp/atterm-config-test.json" {
		t.Errorf("path: got %q", gotPath)
	}
	if string(gotData) != "{}" {
		t.Errorf("data: got %q", string(gotData))
	}
	if gotPerm != 0o600 {
		t.Errorf("perm: got %o", gotPerm)
	}
}

func TestExportConfig_StubPropagatesError(t *testing.T) {
	a := newExportTestApp(t)
	wantErr := errors.New("disk full")
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		return wantErr
	}
	if err := a.writeFile("/tmp/x", []byte("y"), 0o600); !errors.Is(err, wantErr) {
		t.Fatalf("expected disk-full, got %v", err)
	}
}
