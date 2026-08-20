package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
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
type ImportPreview struct {
	Changes []ImportChange `json:"changes"`
	Skipped []string       `json:"skipped"` // malformed entries, with a reason
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
func previewScalarPref(cfg appConfig, current func(string) (json.RawMessage, bool), customized func(string) bool, key string, raw json.RawMessage) (ImportChange, error) {
	target := scalarPrefTarget(key)
	if target == nil {
		return ImportChange{}, fmt.Errorf("unknown preference key")
	}
	if err := json.Unmarshal(raw, target); err != nil {
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

// previewSSHHosts diffs an incoming "ssh_hosts" payload (sshHostsExportPayload)
// against cfg.SSHHosts/cfg.SSHKeys by ID.
//
// A local host/key whose ID does not appear in the file produces no change
// at all — it is neither "unchanged" (it was never compared) nor removed.
// That silence is the point: import only ever adds or replaces entries the
// file actually mentions, so PreviewConfigImport never has to special-case
// "this host wasn't in the file" as a kind of change, and ApplyConfigImport
// (task 3) has nothing here suggesting a delete is ever appropriate.
func previewSSHHosts(cfg appConfig, raw json.RawMessage) ([]ImportChange, []string) {
	var payload sshHostsExportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
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
	var skipped []string

	seenHosts := make(map[string]bool, len(payload.Hosts))
	for _, h := range payload.Hosts {
		id := strings.TrimSpace(h.ID)
		switch {
		case id == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: host %q missing id", h.Alias))
			continue
		case h.Host == "":
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
		changes = append(changes, ImportChange{
			Key:    "ssh_host:" + id,
			Action: listAction(localHosts, id, h),
			Detail: hostDetail(h),
		})
	}

	seenKeys := make(map[string]bool, len(payload.Keys))
	for _, k := range payload.Keys {
		id := strings.TrimSpace(k.ID)
		switch {
		case id == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key %q missing id", k.Name))
			continue
		case k.Name == "":
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key id=%s missing name", id))
			continue
		case seenKeys[id]:
			skipped = append(skipped, fmt.Sprintf("ssh_hosts: key id=%s duplicate in file, first occurrence kept", id))
			continue
		}
		seenKeys[id] = true
		changes = append(changes, ImportChange{
			Key:    "ssh_key:" + id,
			Action: listAction(localKeys, id, k),
			Detail: k.Name,
		})
	}

	return changes, skipped
}

// previewProfiles diffs an incoming "profiles" payload (profilesExportPayload)
// against cfg.Profiles by ID, applying the same "absent from file == kept,
// not removed" rule previewSSHHosts does, and the same "empty id/name and
// duplicate id are malformed" rule filterValidProfiles applies to inbound
// sync — reused here as a rule, not as code, because filterValidProfiles
// logs and returns a filtered slice; Preview needs a per-entry reason string
// instead, to put in ImportPreview.Skipped.
//
// Comparison is on the profile exactly as the file contains it, Env
// included. BuildConfigExport strips Env for any profile with SyncEnv ==
// false unless the export was built with includeLocalEnv, so a profile that
// has local env and was exported without it will show as "replace" even
// though only Env differs — that reflects what the file actually contains,
// not a bug in the diff. ApplyConfigImport (task 3) decides the merge
// policy for Env; Preview reports the file's literal content.
func previewProfiles(cfg appConfig, raw json.RawMessage) ([]ImportChange, []string) {
	var payload profilesExportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, []string{fmt.Sprintf("profiles: %v", err)}
	}

	localByID := make(map[string]SessionProfile, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		localByID[p.ID] = p
	}

	var changes []ImportChange
	var skipped []string

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
		changes = append(changes, ImportChange{
			Key:    "profile:" + id,
			Action: listAction(localByID, id, p),
			Detail: p.Name,
		})
	}

	if payload.DefaultProfileID != "" {
		action := "unchanged"
		switch {
		case cfg.DefaultProfileID == "":
			action = "add"
		case cfg.DefaultProfileID != payload.DefaultProfileID:
			action = "replace"
		}
		changes = append(changes, ImportChange{
			Key:    "default_profile_id",
			Action: action,
			Detail: payload.DefaultProfileID,
		})
	}

	return changes, skipped
}

// listAction is the shared "add vs replace vs unchanged" rule for a
// by-ID list entry: absent locally -> add, present and identical ->
// unchanged, present and different -> replace. T is comparable via
// reflect.DeepEqual (SSHHost, SSHKey, SessionProfile are all plain structs
// of comparable/slice fields, no funcs or channels), so a shallow == can't
// be used here — a struct with a slice field isn't comparable with ==.
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
// disk. A key outside this set is either from a hand-edited file or a
// format this build's Preferences shape doesn't know about — either way it
// is reported through Skipped rather than silently ignored, so an import
// preview never quietly drops a preference the user thinks it applied.
func knownImportKeys() map[string]bool {
	keys := map[string]bool{
		"ssh_hosts": true,
		"profiles":  true,
	}
	for _, k := range []string{
		"locale_preference", "quick_templates", "notifications_enabled",
		"ai_notifications_only", "command_notify_threshold_seconds",
		"shell_integration_enabled", "pinned_session_ids", "terminal_theme",
		"terminal_font_head", "terminal_font_size", "terminal_line_height",
		"terminal_cursor_style", "terminal_cursor_blink", "terminal_scrollback",
		"default_shell", "shortcut_bindings",
	} {
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
	var export ConfigExport
	if err := json.Unmarshal([]byte(jsonText), &export); err != nil {
		return ImportPreview{}, fmt.Errorf("parse config export: %w", err)
	}
	if export.Version != configExportVersion {
		return ImportPreview{}, fmt.Errorf("unsupported config export version %d (this build reads version %d)", export.Version, configExportVersion)
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

	var preview ImportPreview
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
			change, err := previewScalarPref(cfg, adapter.ReadValue, customized, key, raw)
			if err != nil {
				preview.Skipped = append(preview.Skipped, fmt.Sprintf("%s: %v", key, err))
				continue
			}
			preview.Changes = append(preview.Changes, change)
		}
	}

	return preview, nil
}
