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
	Hooks ClaudeHooks                `json:"hooks"`
	Extra map[string]json.RawMessage `json:"-"`
}

// ClaudeHooks is the "hooks" object. atterm owns five slots, all pointing
// at the same atterm-hook binary; the dispatcher discriminates by
// hook_event_name + tool_name on receipt:
//
//	Notification     — waiting-input prompts (permission/idle)
//	PreToolUse       — empty matcher: covers AskUserQuestion (waiting-input
//	                   card) AND non-AskUserQuestion tools (streaming 🛠 calls)
//	UserPromptSubmit — streaming 👤 user message
//	Stop             — streaming 🤖 assistant final
//	PostToolUse      — streaming 🛠 tool result
//
// Other hook lists are passed through unmodified.
type ClaudeHooks struct {
	Notification     []HookEntry                `json:"Notification,omitempty"`
	PreToolUse       []HookEntry                `json:"PreToolUse,omitempty"`
	UserPromptSubmit []HookEntry                `json:"UserPromptSubmit,omitempty"`
	Stop             []HookEntry                `json:"Stop,omitempty"`
	PostToolUse      []HookEntry                `json:"PostToolUse,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON splits the known field from the unknown rest so we can
// round-trip the file without losing user keys.
func (c *ClaudeSettings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if h, ok := raw["hooks"]; ok {
		if err := json.Unmarshal(h, &c.Hooks); err != nil {
			return err
		}
		delete(raw, "hooks")
	}
	c.Extra = raw
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
	for _, b := range []struct {
		key  string
		dest *[]HookEntry
	}{
		{"Notification", &h.Notification},
		{"PreToolUse", &h.PreToolUse},
		{"UserPromptSubmit", &h.UserPromptSubmit},
		{"Stop", &h.Stop},
		{"PostToolUse", &h.PostToolUse},
	} {
		if v, ok := raw[b.key]; ok {
			if err := json.Unmarshal(v, b.dest); err != nil {
				return err
			}
			delete(raw, b.key)
		}
	}
	h.Extra = raw
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
	if h.PreToolUse != nil {
		out["PreToolUse"] = h.PreToolUse
	}
	if h.UserPromptSubmit != nil {
		out["UserPromptSubmit"] = h.UserPromptSubmit
	}
	if h.Stop != nil {
		out["Stop"] = h.Stop
	}
	if h.PostToolUse != nil {
		out["PostToolUse"] = h.PostToolUse
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
