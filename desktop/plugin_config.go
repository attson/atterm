package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// PluginConfig is the persisted plugin-system configuration block.
type PluginConfig struct {
	QuickInput   QuickInputConfig   `json:"quickInput"`
	FileExplorer FileExplorerConfig `json:"fileExplorer"`
	Translate    TranslateConfig    `json:"translate"`
}

type QuickInputConfig struct {
	Enabled bool               `json:"enabled"`
	Buttons []QuickInputButton `json:"buttons"`
}

type QuickInputButton struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Send          string `json:"send"`
	AppendNewline bool   `json:"appendNewline"`
	Hotkey        string `json:"hotkey,omitempty"`
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
	Provider          string `json:"provider"`           // currently only "openai-compatible"
	BaseURL           string `json:"baseUrl"`            // e.g. "https://api.openai.com"
	APIKey            string `json:"apiKey"`             // plaintext; same trust as other plugin config
	Model             string `json:"model"`              // free-text; user picks per their endpoint
	DefaultTargetLang string `json:"defaultTargetLang"`  // one of allowedTranslateTargetLangs
}

// allowedTranslateTargetLangs matches the dropdown in TranslateSettings.vue.
// Keep in sync with desktop/frontend/src/plugins/translate/index.ts SUPPORTED_TARGETS.
var allowedTranslateTargetLangs = map[string]bool{
	"zh-CN": true, "en": true, "ja": true, "ko": true,
	"de": true, "fr": true, "es": true,
}

var allowedTranslateProviders = map[string]bool{
	"openai-compatible": true,
}

// applyDefaults fills empty-valued fields with their defaults. Safe to call
// repeatedly. Note: empty Buttons slice triggers the default 3 buttons; if a
// user explicitly emptied the buttons in config.json, applyDefaults will
// reinject them — acceptable because (a) the UI prevents reaching empty
// without re-add and (b) the user would copy the defaults anyway.
func (c *PluginConfig) applyDefaults() {
	if len(c.QuickInput.Buttons) == 0 {
		c.QuickInput.Enabled = true
		c.QuickInput.Buttons = defaultQuickInputButtons()
	}
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
}

func defaultQuickInputButtons() []QuickInputButton {
	return []QuickInputButton{
		{ID: newButtonID(), Label: "ok", Send: "ok", AppendNewline: true},
		{ID: newButtonID(), Label: "continue", Send: "continue", AppendNewline: true},
		{ID: newButtonID(), Label: "发布", Send: "发布", AppendNewline: true},
	}
}

func newButtonID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidatePluginConfig rejects malformed PluginConfig payloads coming from the
// frontend. Internal callers can rely on applyDefaults producing valid output.
func ValidatePluginConfig(c PluginConfig) error {
	seen := make(map[string]struct{}, len(c.QuickInput.Buttons))
	for i, b := range c.QuickInput.Buttons {
		if b.ID == "" {
			return fmt.Errorf("button[%d]: id must be non-empty", i)
		}
		if _, dup := seen[b.ID]; dup {
			return fmt.Errorf("button[%d]: duplicate id %q", i, b.ID)
		}
		seen[b.ID] = struct{}{}
		if b.Label == "" {
			return fmt.Errorf("button[%d]: label must be non-empty", i)
		}
	}
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
	return nil
}
