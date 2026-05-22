package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginConfigDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()

	if !c.QuickInput.Enabled {
		t.Fatal("quickInput should be enabled by default")
	}
	if len(c.QuickInput.Buttons) != 3 {
		t.Fatalf("expected 3 default buttons, got %d", len(c.QuickInput.Buttons))
	}
	labels := []string{c.QuickInput.Buttons[0].Label, c.QuickInput.Buttons[1].Label, c.QuickInput.Buttons[2].Label}
	want := []string{"ok", "continue", "发布"}
	for i := range labels {
		if labels[i] != want[i] {
			t.Errorf("button[%d].Label = %q, want %q", i, labels[i], want[i])
		}
		if !c.QuickInput.Buttons[i].AppendNewline {
			t.Errorf("button[%d].AppendNewline should default to true", i)
		}
		if c.QuickInput.Buttons[i].ID == "" {
			t.Errorf("button[%d].ID should be populated", i)
		}
	}

	if c.FileExplorer.Enabled {
		t.Fatal("fileExplorer should be disabled by default")
	}
	if c.FileExplorer.PanelWidthPx != 380 {
		t.Errorf("PanelWidthPx default = %d, want 380", c.FileExplorer.PanelWidthPx)
	}
	if c.FileExplorer.PanelCollapsed {
		t.Error("PanelCollapsed default should be false (expanded on enable)")
	}
	if c.FileExplorer.InnerTreeRatio != 0.3 {
		t.Errorf("InnerTreeRatio default = %v, want 0.3", c.FileExplorer.InnerTreeRatio)
	}
}

func TestPluginConfigJSONRoundtrip(t *testing.T) {
	var orig PluginConfig
	orig.applyDefaults()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back PluginConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.QuickInput.Buttons[0].Send != "ok" {
		t.Fatalf("roundtrip mangled buttons: %+v", back.QuickInput.Buttons)
	}
}

func TestValidatePluginConfig(t *testing.T) {
	c := PluginConfig{}
	c.applyDefaults()

	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("defaults must validate, got %v", err)
	}

	dup := c
	dup.QuickInput.Buttons[1].ID = dup.QuickInput.Buttons[0].ID
	if err := ValidatePluginConfig(dup); err == nil {
		t.Error("duplicate button IDs must fail validation")
	}

	bad := c
	bad.FileExplorer.PanelWidthPx = 100
	if err := ValidatePluginConfig(bad); err == nil {
		t.Error("PanelWidthPx < 240 must fail validation")
	}

	bad2 := c
	bad2.FileExplorer.InnerTreeRatio = 0.05
	if err := ValidatePluginConfig(bad2); err == nil {
		t.Error("InnerTreeRatio < 0.15 must fail validation")
	}
}

func TestAppConfigEmbedsPluginConfig(t *testing.T) {
	var c appConfig
	if c.Plugins.QuickInput.Buttons != nil {
		t.Fatal("expected zero-value Plugins")
	}
	// applyConfigDefaults must inject plugin defaults.
	applyConfigDefaults(&c)
	if len(c.Plugins.QuickInput.Buttons) != 3 {
		t.Fatalf("plugin defaults not injected, got %+v", c.Plugins)
	}
}

func TestPluginConfig_TranslateDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	if c.Translate.Provider != "openai-compatible" {
		t.Fatalf("Translate.Provider default: got %q want %q", c.Translate.Provider, "openai-compatible")
	}
	if c.Translate.BaseURL != "https://api.openai.com" {
		t.Fatalf("Translate.BaseURL default: got %q", c.Translate.BaseURL)
	}
	if c.Translate.Model != "gpt-4o-mini" {
		t.Fatalf("Translate.Model default: got %q", c.Translate.Model)
	}
	if c.Translate.DefaultTargetLang != "zh-CN" {
		t.Fatalf("Translate.DefaultTargetLang default: got %q", c.Translate.DefaultTargetLang)
	}
	if c.Translate.APIKey != "" {
		t.Fatalf("Translate.APIKey default: got %q want empty", c.Translate.APIKey)
	}
	if c.Translate.Enabled {
		t.Fatalf("Translate.Enabled default: got true want false (opt-in)")
	}
}

func TestValidatePluginConfig_TranslateProviderEnum(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Translate.Provider = "deepl"
	err := ValidatePluginConfig(c)
	if err == nil || !strings.Contains(err.Error(), "translate.provider") {
		t.Fatalf("want translate.provider validation error, got %v", err)
	}
}

func TestValidatePluginConfig_TranslateTargetLangEnum(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Translate.DefaultTargetLang = "xx-YY"
	err := ValidatePluginConfig(c)
	if err == nil || !strings.Contains(err.Error(), "translate.defaultTargetLang") {
		t.Fatalf("want translate.defaultTargetLang validation error, got %v", err)
	}
}

func TestPluginConfig_ShortcutsDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	if c.Shortcuts.Bindings == nil {
		t.Fatal("Shortcuts.Bindings should be initialized to an empty map by applyDefaults")
	}
	if len(c.Shortcuts.Bindings) != 0 {
		t.Fatalf("Shortcuts.Bindings should default to empty, got %d entries", len(c.Shortcuts.Bindings))
	}
}

func TestValidatePluginConfig_ShortcutsAcceptsValid(t *testing.T) {
	c := PluginConfig{}
	c.applyDefaults()
	c.Shortcuts.Bindings = map[string]string{
		"pane.split-vertical-new":   "Mod+KeyN",
		"pane.focus-left":           "Mod+Alt+ArrowLeft",
		"pane.split-horizontal-new": "Mod+Alt+Shift+KeyJ",
		"pane.close":                "", // disabled is valid
	}
	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("valid bindings should validate, got %v", err)
	}
}

func TestValidatePluginConfig_ShortcutsRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"missing modifier":   "KeyN",
		"missing code":       "Mod+Alt+",
		"unknown token":      "Foo+KeyN",
		"two codes":          "Mod+KeyN+KeyM",
		"code only":          "ArrowLeft",
		"only modifiers":     "Mod+Alt+Shift",
		"wrong code name":    "Mod+keya",
		"empty action id":    "Mod+KeyN", // checked via empty key below
	}
	for label, binding := range cases {
		c := PluginConfig{}
		c.applyDefaults()
		c.Shortcuts.Bindings = map[string]string{"some.action": binding}
		if label == "empty action id" {
			c.Shortcuts.Bindings = map[string]string{"": binding}
		}
		if err := ValidatePluginConfig(c); err == nil {
			t.Errorf("case %q: expected validation error for binding %q", label, binding)
		}
	}
}
