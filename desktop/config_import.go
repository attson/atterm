package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/attson/atterm/internal/prefssync"
)

// ImportChange describes what applying the import would do to a single
// preference key or list entry. Action is one of "add" (nothing here
// locally), "replace" (a local entry with the same ID/key differs), or
// "unchanged" (present locally and byte-for-byte the same). There is no
// "remove"/"delete" action — see PreviewConfigImport's comment for why that
// omission is structural, not an oversight.
type ImportChange struct {
	Key    string `json:"key"`
	Action string `json:"action"` // "add" | "replace" | "unchanged"
	Detail string `json:"detail,omitempty"`
}

// ImportPreview is what PreviewConfigImport returns: what WOULD happen if
// the file were applied, computed without touching the config store.
// Changes is built in a deterministic order — top-level preference keys
// sorted lexically, list entries (hosts/keys/profiles) within a key in the
// order the file's own JSON arrays list them — so running Preview twice on
// the same file byte-for-byte always produces the same slice, and a test
// (or a UI list) never needs set gymnastics to compare two previews.
//
// Both fields are always non-nil, even when there is nothing to report —
// "nothing to change" is the ordinary success path a file re-imported onto
// its own source machine hits every time, not an edge case. A nil slice
// marshals to JSON null; the sibling PreviewSSHConfigImport
// (ssh_config_import.go) makes() both of its slices for the same reason,
// so a frontend reading preview.changes.length never has to special-case
// null first.
type ImportPreview struct {
	Changes []ImportChange `json:"changes"`
	Skipped []string       `json:"skipped"` // malformed entries, with a reason
}

// newImportPreview returns an ImportPreview with both slices already
// allocated empty rather than nil — see ImportPreview's doc comment for why
// every return path from PreviewConfigImport, including its error returns,
// uses this instead of the zero value.
func newImportPreview() ImportPreview {
	return ImportPreview{Changes: []ImportChange{}, Skipped: []string{}}
}

// ImportReport is what ApplyConfigImport returns: what it actually wrote,
// mirroring ImportPreview's shape (Applied plays the role Changes plays for
// Preview) but naming the field for what already happened rather than what
// would happen. "unchanged" entries never appear in Applied — there was
// nothing to write — matching ImportPreview.Changes only ever counting as a
// "change" what's already add/replace.
type ImportReport struct {
	Applied []ImportChange `json:"applied"`
	Skipped []string       `json:"skipped"`
}

// scalarPrefTarget returns a pointer to a fresh zero value of the Go type
// appConfigAdapter.WriteValue expects for key, or nil for a key WriteValue
// doesn't handle. It exists purely to type-check an inbound scalar value
// during preview — json.Unmarshal into the wrong shape (e.g.
// terminal_font_size as a string) is exactly the "malformed individual
// entry" case bullet 4 of the brief asks for, and it has to be caught here,
// before Apply, because Preview promises not to touch the store at all.
//
// The switch's cases and target types are kept in lockstep with
// appConfigAdapter.WriteValue by hand (see prefssync_adapter.go) rather than
// shared code, because WriteValue's job is "unmarshal AND assign into c",
// inseparable from a live store; duplicating just the shape here is what
// keeps Preview from calling WriteValue (and thus Set) at all.
func scalarPrefTarget(key string) any {
	switch key {
	case "locale_preference", "terminal_theme", "terminal_font_head",
		"terminal_cursor_style", "default_shell":
		return new(string)
	case "quick_templates":
		return new([]QuickTemplate)
	case "notifications_enabled", "ai_notifications_only", "shell_integration_enabled":
		return new(bool)
	case "command_notify_threshold_seconds":
		return new(int)
	case "pinned_session_ids":
		return new([]string)
	case "terminal_font_size", "terminal_scrollback":
		return new(int)
	case "terminal_line_height":
		return new(float64)
	case "terminal_cursor_blink":
		return new(*bool)
	case "shortcut_bindings":
		return new(map[string]string)
	}
	return nil
}

// validateScalarPrefValue runs the same acceptance check the real setter
// would, for the two scalar keys that have one beyond "does it unmarshal
// into the right Go type": default_shell (SetDefaultShell rejects a shell
// missing from PATH) and shortcut_bindings (SetShortcutBindings rejects the
// WHOLE map on a single malformed binding). Every other scalar key's setter
// accepts whatever unmarshals into its type — see appConfigAdapter.WriteValue,
// whose other cases have no such extra guard.
//
// Without this, Preview would report "replace" for a file that
// ApplyConfigImport (task 3, expected to call the real setters per spec §4)
// then refuses outright — the same class of dishonesty MAJOR 2's Env fix
// addresses for profiles, just from a validation angle instead of a merge
// one. validateDefaultShell/validateShortcutBindings live in app.go and are
// the exact functions SetDefaultShell/SetShortcutBindings call, so Preview
// and Apply can never independently drift on what counts as acceptable.
func validateScalarPrefValue(key string, target any) error {
	switch key {
	case "default_shell":
		if _, err := validateDefaultShell(*target.(*string)); err != nil {
			return err
		}
	case "shortcut_bindings":
		if err := validateShortcutBindings(*target.(*map[string]string)); err != nil {
			return err
		}
	}
	return nil
}

// jsonEqual reports whether a and b decode to the same value, ignoring
// incidental formatting (whitespace, key order, 5 vs 5.0). It underlies
// "unchanged" vs "replace": a byte-diff would report every re-export of an
// identical value as changed just because MarshalIndent's spacing differs.
func jsonEqual(a, b json.RawMessage) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// previewScalarPref classifies one non-list preference key. customized
// mirrors BuildConfigExport's own gate (isPrefCustomized): a key the local
// store never explicitly set is reported as "add" even if the zero value it
// would compare against happens to equal the imported one, for the same
// reason isPrefCustomized exists on the export side — "unset" and "set to
// the zero value" are different facts, and only isPrefCustomized can tell
// them apart.
func previewScalarPref(current func(string) (json.RawMessage, bool), customized func(string) bool, key string, raw json.RawMessage) (ImportChange, error) {
	target := scalarPrefTarget(key)
	if target == nil {
		return ImportChange{}, fmt.Errorf("unknown preference key")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return ImportChange{}, err
	}
	if err := validateScalarPrefValue(key, target); err != nil {
		return ImportChange{}, err
	}

	if !customized(key) {
		return ImportChange{Key: key, Action: "add"}, nil
	}
	if curRaw, ok := current(key); ok && jsonEqual(curRaw, raw) {
		return ImportChange{Key: key, Action: "unchanged"}, nil
	}
	return ImportChange{Key: key, Action: "replace"}, nil
}

// validateSSHHostsPayload parses raw as an sshHostsExportPayload and applies
// the "id and the id-appropriate name field must be non-empty; a duplicate
// id within the file keeps the first occurrence" validity rule, trimming
// whitespace from every id it keeps.
//
// Factored out of previewSSHHosts (rather than left inline) so
// ApplyConfigImport (task 3) can call the exact same function instead of
// re-deriving the rule: a second, hand-copied version of "what counts as a
// malformed host/key entry" is exactly how Preview and Apply drift into
// disagreeing about the same file — see the brief's item 1.
func validateSSHHostsPayload(raw json.RawMessage) (hosts []SSHHost, keys []SSHKey, skipped []string, err error) {
	var payload sshHostsExportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, nil, err
	}

	seenHosts := make(map[string]bool, len(payload.Hosts))
	for _, h := range payload.Hosts {
		id := strings.TrimSpace(h.ID)
		host := strings.TrimSpace(h.Host)
		switch {
		case id == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: host %q missing id", h.Alias))
			continue
		case host == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: host id=%s missing host", id))
			continue
		case seenHosts[id]:
			// Mirrors filterValidProfiles' duplicate-in-payload rule (see
			// profiles.go): first occurrence wins, later ones are malformed
			// input rather than a legitimate second entry with the same ID.
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: host id=%s duplicate in file, first occurrence kept", id))
			continue
		}
		seenHosts[id] = true
		// Use the trimmed id in the compared value itself, not just as the
		// map key: leaving h.ID untrimmed would compare a whitespace-padded
		// ID against the local record's clean one, and reflect.DeepEqual
		// would call every such entry "replace" even when nothing else
		// differs.
		h.ID = id
		hosts = append(hosts, h)
	}

	seenKeys := make(map[string]bool, len(payload.Keys))
	for _, k := range payload.Keys {
		id := strings.TrimSpace(k.ID)
		name := strings.TrimSpace(k.Name)
		switch {
		case id == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key %q missing id", k.Name))
			continue
		case name == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key id=%s missing name", id))
			continue
		case seenKeys[id]:
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key id=%s duplicate in file, first occurrence kept", id))
			continue
		}
		seenKeys[id] = true
		k.ID = id
		keys = append(keys, k)
	}

	return hosts, keys, skipped, nil
}

// previewSSHHosts diffs an incoming "ssh_hosts" payload (sshHostsExportPayload)
// against cfg.SSHHosts/cfg.SSHKeys by ID.
//
// A local host/key whose ID does not appear in the file produces no change
// at all — it is neither "unchanged" (it was never compared) nor removed.
// That silence is the point: import only ever adds or replaces entries the
// file actually mentions, so PreviewConfigImport never has to special-case
// "this host wasn't in the file" as a kind of change, and ApplyConfigImport
// (task 3) has nothing here suggesting a delete is ever appropriate.
//
// Hosts and keys are exported verbatim (unlike profiles — see
// previewProfiles for why that one needs a merge-aware comparison), so a
// straight reflect.DeepEqual against the local record is honest here.
func previewSSHHosts(cfg appConfig, raw json.RawMessage) ([]ImportChange, []string) {
	hosts, keys, skipped, err := validateSSHHostsPayload(raw)
	if err != nil {
		return nil, []string{fmt.Sprintf("ssh_hosts: %v", err)}
	}

	localHosts := make(map[string]SSHHost, len(cfg.SSHHosts))
	for _, h := range cfg.SSHHosts {
		localHosts[h.ID] = h
	}
	localKeys := make(map[string]SSHKey, len(cfg.SSHKeys))
	for _, k := range cfg.SSHKeys {
		localKeys[k.ID] = k
	}

	var changes []ImportChange
	for _, h := range hosts {
		changes = append(changes, ImportChange{
			Key:    "ssh_host:" + h.ID,
			Action: listAction(localHosts, h.ID, h),
			Detail: hostDetail(h),
		})
	}
	for _, k := range keys {
		changes = append(changes, ImportChange{
			Key:    "ssh_key:" + k.ID,
			Action: listAction(localKeys, k.ID, k),
			Detail: k.Name,
		})
	}

	return changes, skipped
}

// profileMergeCandidate returns what incoming would look like after the
// Env-preservation rule ApplyConfigImport is expected to apply (mirroring
// mergeProfiles's rule in profiles.go, minus its delete-if-absent half,
// which does not apply here — see previewProfiles). If incoming has no Env
// and did not opt into syncing it, local's Env survives; otherwise
// incoming's Env (possibly empty) is authoritative.
//
// This exists because BuildConfigExport strips Env for SyncEnv==false
// profiles by default (stripUnsyncedEnv), so the raw file entry for such a
// profile looks like it has no Env at all — comparing that raw entry
// against local directly would report "replace" (env wiped) for a profile
// where env-preserving apply would in fact leave Env untouched. A "replace"
// there reads to a user as "my API tokens get overwritten", which is the
// opposite of what happens.
func profileMergeCandidate(local, incoming SessionProfile) SessionProfile {
	if len(incoming.Env) == 0 && !incoming.SyncEnv {
		incoming.Env = local.Env
	}
	return incoming
}

// profileDiffFields lists the SessionProfile fields that differ between
// local and merged (the profileMergeCandidate result), by JSON field name,
// for ImportChange.Detail on a "replace" — so a UI (or a human reading
// Skipped/Changes) can tell "just the shell changed" from "everything did"
// without a full struct dump.
func profileDiffFields(local, merged SessionProfile) []string {
	var diffs []string
	if local.Name != merged.Name {
		diffs = append(diffs, "name")
	}
	if local.Shell != merged.Shell {
		diffs = append(diffs, "shell")
	}
	if local.Cwd != merged.Cwd {
		diffs = append(diffs, "cwd")
	}
	if local.StartupCmd != merged.StartupCmd {
		diffs = append(diffs, "startup_cmd")
	}
	if local.SyncEnv != merged.SyncEnv {
		diffs = append(diffs, "sync_env")
	}
	if !reflect.DeepEqual(local.Env, merged.Env) {
		diffs = append(diffs, "env")
	}
	return diffs
}

// validateProfilesPayload parses raw as a profilesExportPayload and applies
// the same "empty id/name and duplicate id are malformed" rule
// filterValidProfiles (profiles.go) applies to an inbound sync payload —
// same rule, reimplemented rather than called, because filterValidProfiles
// logs and returns a filtered slice while callers here need a per-entry
// reason string for ImportPreview.Skipped. DefaultProfileID travels back
// unfiltered: it is validated against the merged profile list downstream
// (resolveDefaultProfileID), not here.
//
// Factored out of previewProfiles so ApplyConfigImport (task 3) validates
// and dedupes the exact same way Preview classified the file — see
// validateSSHHostsPayload's comment for why a second copy of this rule is
// the wrong move.
func validateProfilesPayload(raw json.RawMessage) (profiles []SessionProfile, defaultProfileID string, skipped []string, err error) {
	var payload profilesExportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", nil, err
	}

	seen := make(map[string]bool, len(payload.Profiles))
	for _, p := range payload.Profiles {
		id := strings.TrimSpace(p.ID)
		name := strings.TrimSpace(p.Name)
		switch {
		case id == "":
			skipped = append(skipped, fmt.Sprintf("profiles: profile %q missing id", p.Name))
			continue
		case name == "":
			skipped = append(skipped, fmt.Sprintf("profiles: profile id=%s missing name", id))
			continue
		case seen[id]:
			skipped = append(skipped, fmt.Sprintf("profiles: profile id=%s duplicate in file, first occurrence kept", id))
			continue
		}
		seen[id] = true
		p.ID = id
		profiles = append(profiles, p)
	}

	return profiles, payload.DefaultProfileID, skipped, nil
}

// previewProfiles diffs an incoming "profiles" payload (profilesExportPayload)
// against cfg.Profiles by ID, applying the same "absent from file == kept,
// not removed" rule previewSSHHosts does.
//
// Comparison goes through profileMergeCandidate rather than a literal
// reflect.DeepEqual against the file entry: BuildConfigExport strips Env
// for SyncEnv==false profiles by default, so the file's own content for
// such a profile is not what apply would actually produce (see
// profileMergeCandidate's comment). Preview reports what apply DOES, not
// what the file happens to say.
func previewProfiles(cfg appConfig, raw json.RawMessage) ([]ImportChange, []string) {
	profiles, defaultProfileID, skipped, err := validateProfilesPayload(raw)
	if err != nil {
		return nil, []string{fmt.Sprintf("profiles: %v", err)}
	}

	localByID := make(map[string]SessionProfile, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		localByID[p.ID] = p
	}

	var changes []ImportChange
	for _, p := range profiles {
		if existing, ok := localByID[p.ID]; ok {
			merged := profileMergeCandidate(existing, p)
			if reflect.DeepEqual(existing, merged) {
				changes = append(changes, ImportChange{Key: "profile:" + p.ID, Action: "unchanged", Detail: p.Name})
			} else {
				changes = append(changes, ImportChange{
					Key:    "profile:" + p.ID,
					Action: "replace",
					Detail: strings.Join(profileDiffFields(existing, merged), ", "),
				})
			}
		} else {
			changes = append(changes, ImportChange{Key: "profile:" + p.ID, Action: "add", Detail: p.Name})
		}
	}

	if defaultProfileID != "" {
		action := "unchanged"
		switch {
		case cfg.DefaultProfileID == "":
			action = "add"
		case cfg.DefaultProfileID != defaultProfileID:
			action = "replace"
		}
		changes = append(changes, ImportChange{
			Key:    "profiles:default_profile_id",
			Action: action,
			Detail: defaultProfileID,
		})
	}

	return changes, skipped
}

// listAction is the shared "add vs replace vs unchanged" rule for a
// by-ID list entry: absent locally -> add, present and identical ->
// unchanged, present and different -> replace. T is compared via
// reflect.DeepEqual (SSHHost, SSHKey are plain structs of comparable/slice
// fields, no funcs or channels), so a shallow == can't be used here — a
// struct with a slice field isn't comparable with ==. Profiles do NOT use
// this — see previewProfiles/profileMergeCandidate for why their comparison
// needs an extra merge step first.
func listAction[T any](local map[string]T, id string, incoming T) string {
	existing, ok := local[id]
	if !ok {
		return "add"
	}
	if reflect.DeepEqual(existing, incoming) {
		return "unchanged"
	}
	return "replace"
}

// hostDetail renders a one-line human label for an ImportChange on an SSH
// host, preferring the alias the user gave it over the bare hostname.
func hostDetail(h SSHHost) string {
	if h.Alias != "" {
		return h.Alias
	}
	return h.Host
}

// knownImportKeys lists every top-level key PreviewConfigImport can
// classify: every prefssync synced key except the two sealed ones (which
// never appear in a plaintext export — see BuildConfigExport), plus the two
// unsealed re-homed names ("ssh_hosts", "profiles") that replace them on
// disk.
//
// Derived from prefssync.SyncedKeys() rather than a second hand-written
// list: a hand-written copy (matching BuildConfigExport's own `handled`
// list, guarded only by TestBuildConfigExport_HandlesEverySyncedKey) can
// drift silently — adding a key to prefssync.syncedKeys would leave this
// list stale with every existing test still green, since a key this
// function doesn't recognize simply lands in Skipped as "unknown
// preference key" instead of failing loudly. Deriving instead of copying
// removes the possibility of that drift outright, rather than adding a
// test to guard against it.
func knownImportKeys() map[string]bool {
	keys := map[string]bool{
		"ssh_hosts": true,
		"profiles":  true,
	}
	for _, k := range prefssync.SyncedKeys() {
		if k == "ssh_hosts_encrypted" || k == "profiles_encrypted" {
			continue
		}
		keys[k] = true
	}
	return keys
}

// PreviewConfigImport parses jsonText as a ConfigExport (see
// config_export.go for the on-disk shape) and reports what applying it
// WOULD change, without changing anything: it never calls a.cfgStore.Set,
// directly or indirectly, and every read goes through a.cfgStore.Get()
// snapshots or the read-only halves of newAppConfigAdapter. A future
// ApplyConfigImport (task 3) is expected to call this function first and
// reuse its Changes to decide what to write — Preview computing the same
// answer twice, once here and once silently inside Apply, is how the two
// would drift apart.
//
// An unknown Version is refused outright rather than parsed best-effort.
// configExportVersion travels with the file precisely so a future format
// change can be detected (see its comment in config_export.go); silently
// parsing a Preferences shape this build doesn't understand as if it were
// the current one is how an import would drop fields it never recognized,
// with no error to say so.
//
// Every preference key is handled independently: a malformed key (bad
// version aside) is recorded in Skipped and the rest of the file is still
// previewed. This mirrors prefssync.Engine.Pull's per-key rule (see its
// doc comment) — Pull used to abort the whole pull on one bad key and
// silently skip everything after it in the response; the same shape of bug
// is just as possible here across preference keys in one file.
func (a *App) PreviewConfigImport(jsonText string) (ImportPreview, error) {
	if a.cfgStore == nil {
		return newImportPreview(), fmt.Errorf("config store not ready")
	}

	var export ConfigExport
	if err := json.Unmarshal([]byte(jsonText), &export); err != nil {
		return newImportPreview(), fmt.Errorf("parse config export: %w", err)
	}
	if export.Version != configExportVersion {
		return newImportPreview(), fmt.Errorf("unsupported config export version %d (this build reads version %d)", export.Version, configExportVersion)
	}

	cfg := a.cfgStore.Get()
	customized := isPrefCustomized(cfg)
	// accountKey is fixed to nil for the same reason BuildConfigExport fixes
	// it to nil (see its comment): a plaintext export never carries
	// ssh_hosts_encrypted/profiles_encrypted, only their unsealed
	// replacements "ssh_hosts"/"profiles", handled below by their own
	// preview functions instead of this adapter.
	adapter := newAppConfigAdapter(a.cfgStore, func() []byte { return nil })
	known := knownImportKeys()

	keys := make([]string, 0, len(export.Preferences))
	for k := range export.Preferences {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order: see ImportPreview's doc comment

	preview := newImportPreview()
	for _, key := range keys {
		raw := export.Preferences[key]
		if !known[key] {
			preview.Skipped = append(preview.Skipped, fmt.Sprintf("%s: unknown preference key", key))
			continue
		}
		switch key {
		case "ssh_hosts":
			changes, skipped := previewSSHHosts(cfg, raw)
			preview.Changes = append(preview.Changes, changes...)
			preview.Skipped = append(preview.Skipped, skipped...)
		case "profiles":
			changes, skipped := previewProfiles(cfg, raw)
			preview.Changes = append(preview.Changes, changes...)
			preview.Skipped = append(preview.Skipped, skipped...)
		default:
			change, err := previewScalarPref(adapter.ReadValue, customized, key, raw)
			if err != nil {
				preview.Skipped = append(preview.Skipped, fmt.Sprintf("%s: %v", key, err))
				continue
			}
			preview.Changes = append(preview.Changes, change)
		}
	}

	return preview, nil
}

// newImportReport returns an ImportReport with both slices already
// allocated empty rather than nil — same reasoning as newImportPreview.
func newImportReport() ImportReport {
	return ImportReport{Applied: []ImportChange{}, Skipped: []string{}}
}

// applyScalarPref unmarshals raw into the Go type key's setter expects and
// calls that setter. It is the write-side twin of scalarPrefTarget +
// previewScalarPref: same key set, same shapes, but assigning through the
// real App method instead of a throwaway target — so the value actually
// lands in the store, marks dirty, and enqueues a push (spec §4), rather
// than only being type-checked.
//
// This function does not re-validate default_shell or shortcut_bindings
// itself — SetDefaultShell/SetShortcutBindings do that internally (they are
// literally what validateScalarPrefValue calls), and ApplyConfigImport only
// ever reaches this function for a key PreviewConfigImport already ran
// through validateScalarPrefValue and accepted. A setter refusing a value
// that got this far would mean Preview and Apply independently disagreed
// about what "valid" means for that key — surfaced as an error here rather
// than silently dropped, so that disagreement cannot pass silently.
func applyScalarPref(a *App, key string, raw json.RawMessage) error {
	switch key {
	case "locale_preference":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetLocalePreference(v)
	case "terminal_theme":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalTheme(v)
	case "terminal_font_head":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalFontHead(v)
	case "terminal_font_size":
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalFontSize(v)
	case "terminal_line_height":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalLineHeight(v)
	case "terminal_cursor_style":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalCursorStyle(v)
	case "terminal_cursor_blink":
		var v *bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		if v == nil {
			return fmt.Errorf("terminal_cursor_blink: value is null")
		}
		return a.SetTerminalCursorBlink(*v)
	case "terminal_scrollback":
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetTerminalScrollback(v)
	case "default_shell":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetDefaultShell(v)
	case "shortcut_bindings":
		var v map[string]string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetShortcutBindings(v)
	case "quick_templates":
		var v []QuickTemplate
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetQuickTemplates(v)
	case "notifications_enabled":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetNotificationsEnabled(v)
	case "ai_notifications_only":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetAINotificationsOnly(v)
	case "command_notify_threshold_seconds":
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetCommandNotifyThresholdSeconds(v)
	case "shell_integration_enabled":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetShellIntegrationEnabled(v)
	case "pinned_session_ids":
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return a.SetPinnedSessionIds(v)
	}
	return fmt.Errorf("unknown preference key")
}

// applySSHHostsImport writes the add/replace entries validateSSHHostsPayload
// finds in raw into cfg.SSHHosts/cfg.SSHKeys by ID, then marks the shared
// "ssh_hosts_encrypted" sync key dirty exactly once for both lists together
// via markSSHHostsDirty — the same helper AddSSHHost / UpdateSSHHost /
// AddSSHKey / UpdateSSHKey / DeleteSSHHost / DeleteSSHKey all call for this
// key, so this still "goes through the app's existing dirty-marking/sync
// machinery" even though it does not call any of those six methods
// individually. That is a deliberate deviation, not an oversight:
//
//   - AddSSHHost/AddSSHKey always mint a fresh ID (ulid.Make()) for the
//     record they're given. Import needs the OPPOSITE: the file's own ID
//     preserved verbatim, because that ID is what lets a second import of
//     the same (or an updated) file recognize "this is the same host" on a
//     later run. Calling AddSSHHost/AddSSHKey here would silently turn
//     every import into a duplicate-add instead of the add/replace merge
//     PreviewConfigImport already promised the caller.
//   - UpdateSSHHost intentionally keeps the CURRENTLY STORED
//     IdentityFile/ProxyJump/ProxyCommand regardless of what the caller
//     passes (see its own doc comment: the UI drawer never edits those
//     three fields, only ssh_config import does). That rule is correct for
//     the drawer's save button; it is wrong for a "replace" import, whose
//     file carries another machine's complete record including those three
//     fields — silently reverting them would contradict what Preview's
//     plain reflect.DeepEqual over the whole struct already told the
//     caller "replace" meant.
//
// Neither existing per-item setter's field-ownership rules fit a bulk,
// full-record, ID-preserving import, so this writes cfg.SSHHosts/SSHKeys
// directly (matching the exact add/replace/keep-if-absent contract
// previewSSHHosts already established) and then reuses markSSHHostsDirty —
// the one piece of the setters' behavior that import DOES need unchanged.
//
// Never touches the OS keyring: a plaintext export carries no credential
// field for a host or key to begin with (see sshHostsExportPayload's
// comment in config_export.go), so there is no credential here to write or
// roll back.
func (a *App) applySSHHostsImport(raw json.RawMessage, actionable map[string]ImportChange) error {
	hosts, keys, _, err := validateSSHHostsPayload(raw)
	if err != nil {
		// PreviewConfigImport already reported this as Skipped (it runs the
		// exact same parse); nothing new to apply.
		return nil
	}

	cfg := a.cfgStore.Get()
	touched := false

	hostIdx := make(map[string]int, len(cfg.SSHHosts))
	for i, h := range cfg.SSHHosts {
		hostIdx[h.ID] = i
	}
	for _, h := range hosts {
		c, ok := actionable["ssh_host:"+h.ID]
		if !ok || c.Action == "unchanged" {
			continue
		}
		if idx, exists := hostIdx[h.ID]; exists {
			cfg.SSHHosts[idx] = h
		} else {
			cfg.SSHHosts = append(cfg.SSHHosts, h)
			hostIdx[h.ID] = len(cfg.SSHHosts) - 1
		}
		touched = true
	}

	keyIdx := make(map[string]int, len(cfg.SSHKeys))
	for i, k := range cfg.SSHKeys {
		keyIdx[k.ID] = i
	}
	for _, k := range keys {
		c, ok := actionable["ssh_key:"+k.ID]
		if !ok || c.Action == "unchanged" {
			continue
		}
		if idx, exists := keyIdx[k.ID]; exists {
			cfg.SSHKeys[idx] = k
		} else {
			cfg.SSHKeys = append(cfg.SSHKeys, k)
			keyIdx[k.ID] = len(cfg.SSHKeys) - 1
		}
		touched = true
	}

	if !touched {
		return nil
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markSSHHostsDirty()
	return nil
}

// applyProfilesImport writes the add/replace entries validateProfilesPayload
// finds in raw through mergeProfiles (profiles.go) — the SAME function the
// real cross-device pull path (appConfigAdapter.WriteValue's
// "profiles_encrypted" case) uses, so a local profile's Env, for a profile
// with SyncEnv == false, survives an import exactly the way it survives a
// pull (see profileMergeCandidate's comment for why the raw file entry is
// not what apply should compare against or write).
//
// mergeProfiles's own doc comment states a rule that looks like it
// contradicts import's contract: "ID absent from incoming -> the profile
// was deleted on the other machine; delete it here too." That is correct
// for a PULL, where "incoming" is the complete, authoritative profile list
// for the account (openProfiles decodes the full sealed list every time,
// nothing is ever partial). It is wrong for IMPORT: previewProfiles's own
// contract (design doc §3) is "a local entry absent from the file is KEPT,
// never deleted" — an imported file is a merge input, not assumed to be a
// complete inventory of every profile the user owns.
//
// The reconciliation: this function does not hand mergeProfiles the file's
// list directly as "incoming". It builds that argument as the file's
// validated profiles PLUS every local profile whose ID the file does not
// mention, carried over verbatim. Every local ID is then present in
// "incoming" one way or another, so mergeProfiles's delete-if-absent branch
// never actually triggers — nothing is ever missing from incoming — while
// its Env-preservation branch still runs exactly as it does for a pull, for
// the ids the file DOES mention.
func (a *App) applyProfilesImport(raw json.RawMessage, actionable map[string]ImportChange) error {
	fileProfiles, defaultProfileID, _, err := validateProfilesPayload(raw)
	if err != nil {
		// PreviewConfigImport already reported this as Skipped.
		return nil
	}

	fileByID := make(map[string]bool, len(fileProfiles))
	anyProfileChange := false
	for _, p := range fileProfiles {
		fileByID[p.ID] = true
		if c, ok := actionable["profile:"+p.ID]; ok && c.Action != "unchanged" {
			anyProfileChange = true
		}
	}
	defaultChange, hasDefaultChange := actionable["profiles:default_profile_id"]
	if hasDefaultChange && defaultChange.Action == "unchanged" {
		hasDefaultChange = false
	}
	if !anyProfileChange && !hasDefaultChange {
		return nil
	}

	cfg := a.cfgStore.Get()

	// incoming = every file-derived profile, plus every local-only profile
	// (id not mentioned by the file) carried over verbatim — see the
	// reconciliation note above.
	incoming := make([]SessionProfile, 0, len(fileProfiles)+len(cfg.Profiles))
	incoming = append(incoming, fileProfiles...)
	for _, local := range cfg.Profiles {
		if !fileByID[local.ID] {
			incoming = append(incoming, local)
		}
	}
	merged := mergeProfiles(cfg.Profiles, incoming)

	if err := a.SetProfiles(merged); err != nil {
		return fmt.Errorf("apply profiles: %w", err)
	}

	if hasDefaultChange {
		resolved := resolveDefaultProfileID(defaultProfileID, merged)
		if err := a.SetDefaultProfileID(resolved); err != nil {
			return fmt.Errorf("apply default profile id: %w", err)
		}
	}
	return nil
}

// ApplyConfigImport parses jsonText independently (see below) and writes
// every add/replace entry PreviewConfigImport reports through the same App
// setters a user action would use — never into a.cfgStore directly (spec
// §4) — so an imported value lands in the store, marks its sync key dirty,
// and rides the existing enqueueSync coalescing (prefs_sync_loop.go) out to
// other devices, exactly like any other settings change.
//
// Apply calls PreviewConfigImport first and reuses its Changes to decide
// what to write, rather than re-deriving the add/replace/unchanged/skip
// classification itself — the two must never independently disagree about
// the same file (see PreviewConfigImport's own doc comment). It then parses
// jsonText a SECOND time, on its own, into a fresh ConfigExport: Apply is
// never handed a cached/parsed handle from a caller-side Preview call, so
// "what the user was shown" and "what gets written" are always two parses
// of the exact same bytes, never a struct passed between two calls that
// could drift onto different bytes in between (spec §5).
//
// "unchanged" entries are never written — same store, same value, nothing
// to do. Every other entry is dispatched to its write path below: scalar
// keys each go through their own named setter (applyScalarPref); every
// ssh_host:*/ssh_key:* entry is batched into one applySSHHostsImport call;
// every profile:*/profiles:default_profile_id entry is batched into one
// applyProfilesImport call. That batching is what keeps this function from
// producing more markPrefDirtyAndPush/markSSHHostsDirty calls than
// necessary on its own — but ApplyConfigImport does NOT attempt to reduce
// that count to a single push itself. It still calls one setter per changed
// scalar key, same as a user changing N settings in the UI one at a time
// would. What actually collapses that burst into (at most) one extra sync
// round trip is enqueueSync's existing coalescing (prefs_sync_loop.go) —
// Apply relies on that machinery rather than reimplementing batching, so a
// regression in enqueueSync's coalescing shows up here too (see
// TestApplyConfigImport_CoalescesIntoOnePush in config_import_test.go).
//
// includeLocalEnv mirrors BuildConfigExport/ExportConfig's parameter of the
// same name for signature symmetry, but Apply does not branch on it: the
// Env a profile ends up with is fully determined by mergeProfiles from what
// the FILE actually contains (was it exported with includeLocalEnv=true) and
// the profile's own SyncEnv, exactly as profileMergeCandidate already
// predicted during Preview — and Preview takes no includeLocalEnv parameter
// at all. Giving Apply a second lever that could pick a different outcome
// than the one Preview already promised the caller is exactly the
// Preview/Apply drift this whole feature is designed to avoid, so this
// parameter is accepted (for API symmetry with the export half) and
// otherwise ignored.
func (a *App) ApplyConfigImport(jsonText string, includeLocalEnv bool) (ImportReport, error) {
	_ = includeLocalEnv // see doc comment: intentionally not used, kept for signature symmetry with ExportConfig
	if a.cfgStore == nil {
		return newImportReport(), fmt.Errorf("config store not ready")
	}

	preview, err := a.PreviewConfigImport(jsonText)
	if err != nil {
		return newImportReport(), err
	}

	// Apply's own, independent parse of the raw text — see the doc comment
	// above for why this is not the same ConfigExport PreviewConfigImport
	// decoded inside its own call above.
	var export ConfigExport
	if err := json.Unmarshal([]byte(jsonText), &export); err != nil {
		return newImportReport(), fmt.Errorf("parse config export: %w", err)
	}

	report := newImportReport()
	report.Skipped = preview.Skipped

	actionable := make(map[string]ImportChange, len(preview.Changes))
	needSSH := false
	needProfiles := false
	for _, c := range preview.Changes {
		if c.Action == "unchanged" {
			continue
		}
		actionable[c.Key] = c
		switch {
		case strings.HasPrefix(c.Key, "ssh_host:"), strings.HasPrefix(c.Key, "ssh_key:"):
			needSSH = true
		case strings.HasPrefix(c.Key, "profile:"), c.Key == "profiles:default_profile_id":
			needProfiles = true
		}
	}

	if needSSH {
		if raw, ok := export.Preferences["ssh_hosts"]; ok {
			if err := a.applySSHHostsImport(raw, actionable); err != nil {
				return report, err
			}
		}
	}
	if needProfiles {
		if raw, ok := export.Preferences["profiles"]; ok {
			if err := a.applyProfilesImport(raw, actionable); err != nil {
				return report, err
			}
		}
	}

	for _, c := range preview.Changes {
		if c.Action == "unchanged" {
			continue
		}
		switch {
		case strings.HasPrefix(c.Key, "ssh_host:"), strings.HasPrefix(c.Key, "ssh_key:"),
			strings.HasPrefix(c.Key, "profile:"), c.Key == "profiles:default_profile_id":
			report.Applied = append(report.Applied, c)
			continue
		}
		raw, ok := export.Preferences[c.Key]
		if !ok {
			continue
		}
		if err := applyScalarPref(a, c.Key, raw); err != nil {
			return report, fmt.Errorf("%s: %w", c.Key, err)
		}
		report.Applied = append(report.Applied, c)
	}

	return report, nil
}
