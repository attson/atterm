package hookinstall

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ClaudeSettings models the subset of ~/.claude/settings.json we care
// about. Unknown top-level keys are preserved via extra map so the
// write back doesn't drop the user's theme, model, etc.
type ClaudeSettings struct {
	Hooks ClaudeHooks    `json:"hooks"`
	Extra map[string]any `json:"-"`
}

// ClaudeHooks is the "hooks" object. We only own the Notification slot.
// Other hook lists (e.g. PreToolUse) are passed through unmodified.
type ClaudeHooks struct {
	Notification []HookEntry    `json:"Notification,omitempty"`
	Extra        map[string]any `json:"-"`
}

// UnmarshalJSON splits the known field from the unknown rest so we can
// round-trip the file without losing user keys.
func (c *ClaudeSettings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Extra = map[string]any{}
	if h, ok := raw["hooks"]; ok {
		if err := json.Unmarshal(h, &c.Hooks); err != nil {
			return err
		}
		delete(raw, "hooks")
	}
	for k, v := range raw {
		var anyV any
		if err := json.Unmarshal(v, &anyV); err != nil {
			return err
		}
		c.Extra[k] = anyV
	}
	return nil
}

func (c ClaudeSettings) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range c.Extra {
		out[k] = v
	}
	out["hooks"] = c.Hooks
	return json.Marshal(out)
}

func (h *ClaudeHooks) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	h.Extra = map[string]any{}
	if n, ok := raw["Notification"]; ok {
		if err := json.Unmarshal(n, &h.Notification); err != nil {
			return err
		}
		delete(raw, "Notification")
	}
	for k, v := range raw {
		var anyV any
		if err := json.Unmarshal(v, &anyV); err != nil {
			return err
		}
		h.Extra[k] = anyV
	}
	return nil
}

func (h ClaudeHooks) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range h.Extra {
		out[k] = v
	}
	if h.Notification != nil {
		out["Notification"] = h.Notification
	}
	return json.Marshal(out)
}

// mergeAttermEntries strips every entry the marker recognizes as
// atterm-owned, then appends desired in order. Idempotent and order-
// preserving for non-atterm entries.
func mergeAttermEntries(existing, desired []HookEntry, marker func(HookEntry) bool) []HookEntry {
	out := make([]HookEntry, 0, len(existing)+len(desired))
	for _, e := range existing {
		if !marker(e) {
			out = append(out, e)
		}
	}
	return append(out, desired...)
}

// readClaudeSettings reads ~/.claude/settings.json. Returns a zero-
// value ClaudeSettings (and nil error) when the file does not exist.
// Surfaces a JSON-parse error to the caller — Install must NOT
// overwrite an unparseable file.
func readClaudeSettings(home string) (ClaudeSettings, error) {
	path := claudeSettingsPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ClaudeSettings{}, nil
		}
		return ClaudeSettings{}, err
	}
	var cfg ClaudeSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ClaudeSettings{}, err
	}
	return cfg, nil
}

// writeClaudeSettings serializes cfg with 2-space indent and writes it
// atomically (temp file + rename). Creates ~/.claude/ if needed.
func writeClaudeSettings(home string, cfg ClaudeSettings) error {
	if err := os.MkdirAll(claudeDir(home), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := claudeSettingsPath(home)
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path),
		filepath.Base(path)+".atterm-tmp-"+hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
