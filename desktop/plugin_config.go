package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PluginConfig is the persisted plugin-system configuration block.
type PluginConfig struct {
	FileExplorer FileExplorerConfig `json:"fileExplorer"`
	Translate    TranslateConfig    `json:"translate"`
	Shortcuts    ShortcutsConfig    `json:"shortcuts"`
	Pet          PetConfig          `json:"pet"`
}

// PetConfig is the companion-window plugin ("桌面挂件" / Desk Widget) block. Unlike the other
// plugins it does not render into the main window — enabling it spawns a
// second process of this same binary with --pet, which owns a frameless
// always-on-top window. See
// docs/superpowers/specs/2026-08-10-ai-pet-companion-window-design.md.
type PetConfig struct {
	Enabled bool `json:"enabled"`
	// AIOnly restricts the widget to sessions classified as AI (claude /
	// codex / aider). Off by default: the widget's state model comes from
	// OSC 133 and works for any command, which is what separates it from
	// hook-based tools that only ever see one agent.
	AIOnly bool `json:"aiOnly"`
	// Collapsed hides the session list, leaving only the pet + summary header.
	Collapsed bool `json:"collapsed"`
	// WindowX / WindowY are the last dragged screen position in logical
	// pixels. Both -1 means "never positioned" — the pet window then places
	// itself at the bottom-right of the primary display on first show.
	WindowX int `json:"windowX"`
	WindowY int `json:"windowY"`
	// MutedUntilUnix silences the attention animation (jump + auto-peek)
	// until this wall-clock second. 0 means not muted. Counts are still
	// rendered while muted — muting suppresses motion, not information.
	MutedUntilUnix int64 `json:"mutedUntilUnix"`
}

type FileExplorerConfig struct {
	Enabled         bool    `json:"enabled"`
	PanelWidthPx    int     `json:"panelWidthPx"`
	PanelCollapsed  bool    `json:"panelCollapsed"`
	InnerTreeRatio  float64 `json:"innerTreeRatio"`
	ShowHidden      bool    `json:"showHidden"`
	ShowLineNumbers bool    `json:"showLineNumbers"`
}

type TranslateConfig struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`          // currently only "openai-compatible"
	BaseURL           string `json:"baseUrl"`           // e.g. "https://api.openai.com"
	APIKey            string `json:"apiKey"`            // plaintext; same trust as other plugin config
	Model             string `json:"model"`             // free-text; user picks per their endpoint
	DefaultTargetLang string `json:"defaultTargetLang"` // one of allowedTranslateTargetLangs
	// ExtraParams is a raw JSON object string merged into every chat
	// completion body (e.g. {"stream": true, "top_p": 0.9}). Empty means
	// "no overrides". Validated as a JSON object at save time.
	ExtraParams string `json:"extraParams"`
}

// ShortcutsConfig is the persisted shortcut-binding overrides. The map is
// sparse: only actions the user has explicitly changed appear here. Absent
// keys mean "use the frontend registry default for this action"; an empty
// value means "this action is disabled".
type ShortcutsConfig struct {
	Bindings map[string]string `json:"bindings"`
}

// shortcutBindingRe matches a serialized binding string. Tokens are in fixed
// order: optional Mod, optional Alt, optional Shift (at least one modifier
// required), then exactly one code token from the whitelist. The empty
// string is also a valid binding (means "disabled") and is handled by the
// caller before applying this regex.
var shortcutBindingRe = regexp.MustCompile(
	`^(?:Mod\+)?(?:Alt\+)?(?:Shift\+)?(?:KeyA|KeyB|KeyC|KeyD|KeyE|KeyF|KeyG|KeyH|KeyI|KeyJ|KeyK|KeyL|KeyM|KeyN|KeyO|KeyP|KeyQ|KeyR|KeyS|KeyT|KeyU|KeyV|KeyW|KeyX|KeyY|KeyZ|Digit0|Digit1|Digit2|Digit3|Digit4|Digit5|Digit6|Digit7|Digit8|Digit9|ArrowLeft|ArrowRight|ArrowUp|ArrowDown|BracketLeft|BracketRight|Minus|Equal|Backquote|Comma|Period|Slash|Semicolon|Quote|Backslash)$`,
)

func isValidShortcutBinding(s string) bool {
	if s == "" {
		return true
	}
	// Regex permits "no modifier" because Go's RE2 doesn't have lookaheads;
	// enforce the "at least one modifier" rule separately.
	if !strings.HasPrefix(s, "Mod+") && !strings.HasPrefix(s, "Alt+") && !strings.HasPrefix(s, "Shift+") {
		return false
	}
	return shortcutBindingRe.MatchString(s)
}

// allowedTranslateTargetLangs matches the TARGETS arrays in
// desktop/frontend/src/plugins/translate/{TranslatePanel.vue, TranslateSettings.vue}.
var allowedTranslateTargetLangs = map[string]bool{
	"zh-CN": true, "en": true, "ja": true, "ko": true,
	"de": true, "fr": true, "es": true,
}

var allowedTranslateProviders = map[string]bool{
	"openai-compatible": true,
}

// applyDefaults fills empty-valued fields with their defaults. Safe to call
// repeatedly.
func (c *PluginConfig) applyDefaults() {
	if c.FileExplorer.PanelWidthPx == 0 {
		c.FileExplorer.PanelWidthPx = 380
		c.FileExplorer.PanelCollapsed = false
		c.FileExplorer.InnerTreeRatio = 0.3
		c.FileExplorer.ShowHidden = false
	}
	if c.Translate.Provider == "" {
		c.Translate.Enabled = false
		c.Translate.Provider = "openai-compatible"
		c.Translate.BaseURL = "https://api.openai.com"
		c.Translate.Model = "gpt-4o-mini"
		c.Translate.DefaultTargetLang = "zh-CN"
	}
	if c.Shortcuts.Bindings == nil {
		c.Shortcuts.Bindings = map[string]string{}
	}
	if c.Pet.WindowX == 0 && c.Pet.WindowY == 0 {
		// (0,0) is a legal screen position but never a useful default, so it
		// doubles as the "unset" marker on a zero-valued struct.
		c.Pet.WindowX = -1
		c.Pet.WindowY = -1
	}
}

// ValidatePluginConfig rejects malformed PluginConfig payloads coming from the
// frontend. Internal callers can rely on applyDefaults producing valid output.
func ValidatePluginConfig(c PluginConfig) error {
	if c.FileExplorer.PanelWidthPx < 240 || c.FileExplorer.PanelWidthPx > 2000 {
		return errors.New("fileExplorer.panelWidthPx out of bounds [240, 2000]")
	}
	if c.FileExplorer.InnerTreeRatio < 0.15 || c.FileExplorer.InnerTreeRatio > 0.5 {
		return errors.New("fileExplorer.innerTreeRatio out of bounds [0.15, 0.5]")
	}
	if !allowedTranslateProviders[c.Translate.Provider] {
		return fmt.Errorf("translate.provider %q not allowed", c.Translate.Provider)
	}
	if !allowedTranslateTargetLangs[c.Translate.DefaultTargetLang] {
		return fmt.Errorf("translate.defaultTargetLang %q not allowed", c.Translate.DefaultTargetLang)
	}
	if s := strings.TrimSpace(c.Translate.ExtraParams); s != "" {
		var probe map[string]any
		if err := json.Unmarshal([]byte(s), &probe); err != nil {
			return fmt.Errorf("translate.extraParams: not a JSON object (%v)", err)
		}
	}
	// Screen coordinates can legitimately be negative (a display left of or
	// above the primary one), so only the sentinel and absurd magnitudes are
	// rejected — a stale position from an unplugged monitor is clamped back
	// on screen by the pet process, not here.
	if c.Pet.WindowX < -32000 || c.Pet.WindowX > 32000 {
		return fmt.Errorf("pet.windowX out of bounds [-32000, 32000]: %d", c.Pet.WindowX)
	}
	if c.Pet.WindowY < -32000 || c.Pet.WindowY > 32000 {
		return fmt.Errorf("pet.windowY out of bounds [-32000, 32000]: %d", c.Pet.WindowY)
	}
	if c.Pet.MutedUntilUnix < 0 {
		return errors.New("pet.mutedUntilUnix must not be negative")
	}
	for actionID, binding := range c.Shortcuts.Bindings {
		if actionID == "" {
			return errors.New("shortcuts.bindings: action id must be non-empty")
		}
		if !isValidShortcutBinding(binding) {
			return fmt.Errorf("shortcuts.bindings[%q]: malformed binding %q", actionID, binding)
		}
	}
	return nil
}
