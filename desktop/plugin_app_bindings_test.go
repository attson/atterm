package main

import (
	"testing"
)

func TestGetSetPluginConfigRoundtrip(t *testing.T) {
	app := newPluginTestApp(t)

	got := app.GetPluginConfig()
	if len(got.QuickInput.Buttons) != 3 {
		t.Fatalf("expected default buttons populated, got %d", len(got.QuickInput.Buttons))
	}

	got.QuickInput.Buttons[0].Label = "yes"
	if err := app.SetPluginConfig(got); err != nil {
		t.Fatal(err)
	}

	again := app.GetPluginConfig()
	if again.QuickInput.Buttons[0].Label != "yes" {
		t.Fatalf("write did not persist, got label %q", again.QuickInput.Buttons[0].Label)
	}
}

func TestSetPluginConfigRejectsInvalid(t *testing.T) {
	app := newPluginTestApp(t)

	bad := app.GetPluginConfig()
	bad.QuickInput.Buttons[0].ID = "" // forbidden by ValidatePluginConfig
	if err := app.SetPluginConfig(bad); err == nil {
		t.Fatal("expected validation error")
	}
}

// newPluginTestApp constructs an App backed by an in-memory configStore.
// The returned App has no Wails context; EventsEmit will be skipped.
func newPluginTestApp(t *testing.T) *App {
	t.Helper()
	cfgStore := &configStore{}
	cfgStore.cfg.Plugins.applyDefaults()
	lm := &loggingManager{}
	app := NewApp(cfgStore, lm)
	// ctx is left nil so EventsEmit is skipped in tests
	return app
}
