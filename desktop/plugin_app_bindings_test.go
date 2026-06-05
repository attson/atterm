package main

import (
	"testing"
)

func TestGetSetPluginConfigRoundtrip(t *testing.T) {
	app := newPluginTestApp(t)

	got := app.GetPluginConfig()
	if got.FileExplorer.PanelWidthPx != 380 {
		t.Fatalf("expected defaults populated, got %+v", got.FileExplorer)
	}

	got.FileExplorer.PanelWidthPx = 500
	if err := app.SetPluginConfig(got); err != nil {
		t.Fatal(err)
	}

	again := app.GetPluginConfig()
	if again.FileExplorer.PanelWidthPx != 500 {
		t.Fatalf("write did not persist, got PanelWidthPx %d", again.FileExplorer.PanelWidthPx)
	}
}

func TestSetPluginConfigRejectsInvalid(t *testing.T) {
	app := newPluginTestApp(t)

	bad := app.GetPluginConfig()
	bad.FileExplorer.PanelWidthPx = 100 // below 240 min, rejected by ValidatePluginConfig
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
