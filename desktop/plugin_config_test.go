package main

import (
	"encoding/json"
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
