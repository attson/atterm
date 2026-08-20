package userstore

import (
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

// expectedSyncedKeyKinds is the JSON shape each prefssync-synced key is
// actually marshaled as by desktop/prefssync_adapter.go's ReadValue switch.
// internal/userstore cannot import the desktop package (it lives outside
// internal/), so this table is maintained by hand — whoever changes a
// field's Go type on the adapter side must update the matching entry here in
// the same review.
var expectedSyncedKeyKinds = map[string]preferenceKind{
	"locale_preference":                preferenceKindString,
	"quick_templates":                  preferenceKindArray,
	"notifications_enabled":            preferenceKindBool,
	"ai_notifications_only":            preferenceKindBool,
	"command_notify_threshold_seconds": preferenceKindInt,
	"shell_integration_enabled":        preferenceKindBool,
	"pinned_session_ids":               preferenceKindArray,
	"ssh_hosts_encrypted":              preferenceKindString, // base64 ciphertext string, see desktop/ssh_sync.go openSSHHosts
	"terminal_theme":                   preferenceKindString,
	"terminal_font_head":               preferenceKindString,
	"terminal_font_size":               preferenceKindInt,
	"terminal_line_height":             preferenceKindNumber,
	"terminal_cursor_style":            preferenceKindString,
	"terminal_cursor_blink":            preferenceKindBool,
	"terminal_scrollback":              preferenceKindInt,
	"default_shell":                    preferenceKindString,
	"shortcut_bindings":                preferenceKindObject,
	"profiles_encrypted":               preferenceKindString, // base64 ciphertext string, see desktop/profiles.go sealProfiles
}

// TestAllowedPreferenceKeys_CoversSyncedKeys is the structural test the
// 2026-08-17 prefs-sync-l1 final review asked for (C2): nothing previously
// bound prefssync.SyncedKeys() (the client's idea of what syncs) to
// allowedPreferenceKeys (the server's idea of what's legal). That gap is why
// C1 happened — twice, first for ssh_hosts_encrypted, then for all nine L1
// keys — with the relay silently 400ing every PUT that carried one of the
// missing keys, poisoning every other dirty key riding along in the same
// batch.
//
// If this test fails naming a key: that key is synced by the desktop client
// but the relay will reject it. Add it to allowedPreferenceKeys in
// internal/userstore/preferences.go with the JSON-shape kind that matches
// what desktop/prefssync_adapter.go's ReadValue marshals for it (see
// expectedSyncedKeyKinds above for the shape), then add/update the matching
// entry in expectedSyncedKeyKinds in this file so the test keeps checking it.
func TestAllowedPreferenceKeys_CoversSyncedKeys(t *testing.T) {
	for _, key := range prefssync.SyncedKeys() {
		kind, ok := allowedPreferenceKeys[key]
		if !ok {
			t.Errorf(
				"prefssync.SyncedKeys() includes %q but allowedPreferenceKeys "+
					"(internal/userstore/preferences.go) does not — every PUT of "+
					"this key will 400 unknown_key, and because SetUserPreferences "+
					"rejects the whole batch on the first unknown key, every other "+
					"dirty key syncing at the same time silently stops syncing too. "+
					"Add %q to allowedPreferenceKeys with the kind matching what "+
					"desktop/prefssync_adapter.go's ReadValue marshals for it, and "+
					"add a matching entry to expectedSyncedKeyKinds in this test file.",
				key, key,
			)
			continue
		}
		want, ok := expectedSyncedKeyKinds[key]
		if !ok {
			t.Errorf(
				"key %q is in both prefssync.SyncedKeys() and allowedPreferenceKeys "+
					"but has no entry in expectedSyncedKeyKinds (this test file) — "+
					"add one describing the JSON shape desktop/prefssync_adapter.go "+
					"marshals for it, so this test actually verifies the kind and "+
					"not just presence.",
				key,
			)
			continue
		}
		if kind != want {
			t.Errorf(
				"key %q: allowedPreferenceKeys kind = %v, but expectedSyncedKeyKinds "+
					"(mirroring desktop/prefssync_adapter.go's ReadValue) says %v — "+
					"fix allowedPreferenceKeys in internal/userstore/preferences.go "+
					"to match the adapter's actual marshaled shape, or the relay will "+
					"400 invalid_value on every PUT of this key.",
				key, kind, want,
			)
		}
	}
}
