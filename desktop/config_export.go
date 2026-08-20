package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/attson/atterm/internal/prefssync"
)

// saveDialogFunc is the function ExportConfig uses to prompt for a save
// path. Held as a field on App (mirroring writeFile) so tests can substitute
// a stub instead of driving wailsruntime.SaveFileDialog for real —
// SaveFileDialog calls log.Fatalf when a.ctx has no frontend bound (see
// getFrontend in wails/v2/pkg/runtime), which kills the whole test binary
// rather than failing the one test. Defaults to wailsruntime.SaveFileDialog
// in production.
type saveDialogFunc func(ctx context.Context, options wailsruntime.SaveDialogOptions) (string, error)

// configExportVersion travels inside the exported file (not inferred from
// AppVersion) so a future import can recognize and refuse — or migrate — a
// file whose Preferences shape no longer matches what this version of
// BuildConfigExport produces, independent of which app release wrote it.
const configExportVersion = 1

// ConfigExport is the on-disk shape written by ExportConfig. It is plain
// JSON meant to be read by a human or mailed between the user's own
// machines — never the relay's sealed wire format. See BuildConfigExport
// for why the two sealed synced keys are re-homed here under different
// names instead of round-tripping through their sealers.
type ConfigExport struct {
	Version     int                        `json:"atterm_export"`
	ExportedAt  string                     `json:"exported_at"`
	AppVersion  string                     `json:"app_version"`
	Preferences map[string]json.RawMessage `json:"preferences"`
}

// sshHostsExportPayload is the plaintext export shape for the "ssh_hosts"
// preference key. It mirrors sshSyncPayload's Hosts/Keys shape but has no
// Cred or PrivateKey/Passphrase field at all — see BuildConfigExport for why
// that omission has to be structural, not a value left empty.
type sshHostsExportPayload struct {
	Hosts []SSHHost `json:"hosts"`
	Keys  []SSHKey  `json:"keys"`
}

// profilesExportPayload is the plaintext export shape for the "profiles"
// preference key, mirroring profilesSyncPayload. DefaultProfileID rides
// along for the same reason it does there (see sealProfiles): it is one
// user-facing preference together with the profile list, not two.
type profilesExportPayload struct {
	Profiles         []SessionProfile `json:"profiles"`
	DefaultProfileID string           `json:"default_profile_id,omitempty"`
}

// BuildConfigExport assembles the plaintext export payload from the live
// config, without ever touching the OS keyring.
//
// sealSSHHosts (the relay path for the same host/key data) loads
// sshCredential and sshKeySecret — the actual password and private key —
// from the keyring via sshCredentialSlot/sshKeySecretSlot and bundles them
// into the sealed blob, because two desktops the same user controls are
// allowed to see each other's secrets. A plaintext file on disk has no such
// guarantee: it can be mailed, synced to cloud storage, or opened in an
// editor. So this function never calls sealSSHHosts, sealProfiles, or
// either credential loader — it reads c.SSHHosts/c.SSHKeys directly, and
// those config-resident structs have no secret field to begin with
// (SSHHost carries no credential; SSHKey is only {ID, Name, KeyType}).
// That makes "forgot to strip a secret" structurally impossible instead of
// a step this function has to remember.
//
// includeLocalEnv controls whether profile Env survives for profiles with
// SyncEnv == false. false (the default a user should pick before sharing a
// file) runs every profile through stripUnsyncedEnv, the same guard the
// relay path uses, so an export doesn't carry env vars the user never
// opted to sync.
func (a *App) BuildConfigExport(includeLocalEnv bool) (ConfigExport, error) {
	cfg := a.cfgStore.Get()

	prefs := make(map[string]json.RawMessage)

	// accountKey is fixed to nil here so adapter.ReadValue treats
	// ssh_hosts_encrypted/profiles_encrypted as "E2EE inactive" and skips
	// them (see their case in ReadValue) — which is exactly what's wanted,
	// since those two keys are populated below from cfg directly instead of
	// through their sealers. Every other synced key has no secret-bearing
	// path, so reading it through the same adapter the relay uses keeps this
	// function from silently drifting out of sync with SyncedKeys().
	adapter := newAppConfigAdapter(a.cfgStore, func() []byte { return nil })
	// isPrefCustomized, not "ReadValue returned ok" or "not the zero value",
	// gates inclusion: ReadValue's ok only means "ReadValue chose to marshal
	// something" — for a plain string/int field it is true even when the
	// field is "", 0, or a nil slice (which marshals to literal JSON null).
	// isPrefCustomized is the one predicate in this codebase that already
	// answers "did the user explicitly set this", the same question
	// SeedFromLocal asks before trusting a value — reusing it here means an
	// export of an untouched preference is absent, not present as "" / 0 /
	// null, matching what a real import should treat as "nothing to apply".
	customized := isPrefCustomized(cfg)
	for _, key := range prefssync.SyncedKeys() {
		if key == "ssh_hosts_encrypted" || key == "profiles_encrypted" {
			continue
		}
		if !customized(key) {
			continue
		}
		if raw, ok := adapter.ReadValue(key); ok {
			prefs[key] = raw
		}
	}

	if len(cfg.SSHHosts) > 0 || len(cfg.SSHKeys) > 0 {
		raw, err := json.Marshal(sshHostsExportPayload{Hosts: cfg.SSHHosts, Keys: cfg.SSHKeys})
		if err != nil {
			return ConfigExport{}, err
		}
		prefs["ssh_hosts"] = raw
	}

	profiles := cfg.Profiles
	if !includeLocalEnv {
		profiles = stripUnsyncedEnv(profiles)
	}
	if len(profiles) > 0 || cfg.DefaultProfileID != "" {
		raw, err := json.Marshal(profilesExportPayload{Profiles: profiles, DefaultProfileID: cfg.DefaultProfileID})
		if err != nil {
			return ConfigExport{}, err
		}
		prefs["profiles"] = raw
	}

	return ConfigExport{
		Version:     configExportVersion,
		ExportedAt:  time.Now().UTC().Format(time.RFC3339),
		AppVersion:  Version,
		Preferences: prefs,
	}, nil
}

// ExportConfig opens a native save dialog (default filename
// "atterm-config-<ts>.json") and writes the exported config to the chosen
// path. Mirrors ExportDiagnostics's cancel semantics (returns "" with a nil
// error when the user dismisses the dialog without picking a path) and its
// injectable a.writeFile seam, plus a.saveDialog: unlike ExportDiagnostics,
// this function needs its own dialog seam to be unit-testable at all, since
// the whole point of a test here is to prove the cancel path returns
// ("", nil) — impossible to assert on without controlling what the dialog
// returns (see saveDialogFunc for why the real dialog can't run in tests).
func (a *App) ExportConfig(includeLocalEnv bool) (string, error) {
	export, err := a.BuildConfigExport(includeLocalEnv)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", err
	}

	defaultName := "atterm-config-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ".json"
	sd := a.saveDialog
	if sd == nil {
		sd = wailsruntime.SaveFileDialog
	}
	path, err := sd(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export config",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	wf := a.writeFile
	if wf == nil {
		wf = os.WriteFile
	}
	if err := wf(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
