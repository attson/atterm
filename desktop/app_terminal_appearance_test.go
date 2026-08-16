package main

import (
	"os"
	"strings"
	"testing"
)

// Round-trip + rejection coverage for the six terminal-appearance Wails
// bindings (app.go ~903-996). config_test.go covers the pure appConfig
// accessors (OrDefault clamping); nothing before this file proved that the
// App methods actually Set→Get round-trip, persist to disk, leave unrelated
// config alone, or that the out-of-range branches reject without mutating
// the stored value. Mirrors app_theme_test.go's newThemeTestApp fixture.

func TestTerminalFontHeadRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalFontHead("Fira Code"); err != nil {
		t.Fatalf("SetTerminalFontHead() error = %v", err)
	}
	if got := a.GetTerminalFontHead(); got != "Fira Code" {
		t.Fatalf("GetTerminalFontHead() = %q; want %q", got, "Fira Code")
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("ReadFile(configPath()) error = %v", err)
	}
	if !strings.Contains(string(data), `"terminal_font_head": "Fira Code"`) {
		t.Fatalf("persisted config missing terminal font head: %s", data)
	}
}

func TestTerminalFontSizeRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalFontSize(20); err != nil {
		t.Fatalf("SetTerminalFontSize() error = %v", err)
	}
	if got := a.GetTerminalFontSize(); got != 20 {
		t.Fatalf("GetTerminalFontSize() = %d; want 20", got)
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("ReadFile(configPath()) error = %v", err)
	}
	if !strings.Contains(string(data), `"terminal_font_size": 20`) {
		t.Fatalf("persisted config missing terminal font size: %s", data)
	}
}

func TestSetTerminalFontSizeRejectsOutOfRange(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalFontSize: 16})
	if err := a.SetTerminalFontSize(999); err == nil {
		t.Fatalf("SetTerminalFontSize(999) error = nil; want error")
	}
	if got := a.GetTerminalFontSize(); got != 16 {
		t.Fatalf("GetTerminalFontSize() = %d; want unchanged 16", got)
	}
}

func TestTerminalLineHeightRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalLineHeight(1.5); err != nil {
		t.Fatalf("SetTerminalLineHeight() error = %v", err)
	}
	if got := a.GetTerminalLineHeight(); got != 1.5 {
		t.Fatalf("GetTerminalLineHeight() = %v; want 1.5", got)
	}
}

func TestSetTerminalLineHeightRejectsOutOfRange(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalLineHeight: 1.2})
	if err := a.SetTerminalLineHeight(9); err == nil {
		t.Fatalf("SetTerminalLineHeight(9) error = nil; want error")
	}
	if got := a.GetTerminalLineHeight(); got != 1.2 {
		t.Fatalf("GetTerminalLineHeight() = %v; want unchanged 1.2", got)
	}
}

func TestTerminalCursorStyleRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalCursorStyle("underline"); err != nil {
		t.Fatalf("SetTerminalCursorStyle() error = %v", err)
	}
	if got := a.GetTerminalCursorStyle(); got != "underline" {
		t.Fatalf("GetTerminalCursorStyle() = %q; want %q", got, "underline")
	}
}

func TestSetTerminalCursorStyleRejectsUnknown(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalCursorStyle: "bar"})
	if err := a.SetTerminalCursorStyle("spiral"); err == nil {
		t.Fatalf("SetTerminalCursorStyle(spiral) error = nil; want error")
	}
	if got := a.GetTerminalCursorStyle(); got != "bar" {
		t.Fatalf("GetTerminalCursorStyle() = %q; want unchanged %q", got, "bar")
	}
}

func TestTerminalCursorBlinkRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalCursorBlink(false); err != nil {
		t.Fatalf("SetTerminalCursorBlink() error = %v", err)
	}
	if got := a.GetTerminalCursorBlink(); got != false {
		t.Fatalf("GetTerminalCursorBlink() = %v; want false", got)
	}
	if err := a.SetTerminalCursorBlink(true); err != nil {
		t.Fatalf("SetTerminalCursorBlink() error = %v", err)
	}
	if got := a.GetTerminalCursorBlink(); got != true {
		t.Fatalf("GetTerminalCursorBlink() = %v; want true", got)
	}
}

func TestTerminalScrollbackRoundTrip(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if err := a.SetTerminalScrollback(12000); err != nil {
		t.Fatalf("SetTerminalScrollback() error = %v", err)
	}
	if got := a.GetTerminalScrollback(); got != 12000 {
		t.Fatalf("GetTerminalScrollback() = %d; want 12000", got)
	}
}

func TestSetTerminalScrollbackRejectsZero(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalScrollback: 8000})
	if err := a.SetTerminalScrollback(0); err == nil {
		t.Fatalf("SetTerminalScrollback(0) error = nil; want error")
	}
	if got := a.GetTerminalScrollback(); got != 8000 {
		t.Fatalf("GetTerminalScrollback() = %d; want unchanged 8000", got)
	}
}

func TestSetTerminalScrollbackRejectsAboveCeiling(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalScrollback: 8000})
	if err := a.SetTerminalScrollback(999_999); err == nil {
		t.Fatalf("SetTerminalScrollback(999999) error = nil; want error")
	}
	if got := a.GetTerminalScrollback(); got != 8000 {
		t.Fatalf("GetTerminalScrollback() = %d; want unchanged 8000", got)
	}
}

// TestSetTerminalAppearancePreservesUnrelatedConfig mirrors
// TestSetTerminalThemePersistsAndPreservesConfig: a write to one
// appearance field must not disturb relay config, update state, or the
// other five appearance fields.
func TestSetTerminalAppearancePreservesUnrelatedConfig(t *testing.T) {
	autoCheck := false
	blink := false
	a := newThemeTestApp(t, appConfig{
		RelayURL:            "wss://relay.example.com",
		RelaySessionToken:   "secret-token",
		AllowInsecureRelay:  true,
		AutoCheckUpdates:    &autoCheck,
		LastCheckAt:         123,
		SkipVersion:         "v9.9.9",
		TerminalTheme:       terminalThemeNord,
		TerminalFontHead:    "SF Mono",
		TerminalFontSize:    18,
		TerminalLineHeight:  1.3,
		TerminalCursorStyle: "underline",
		TerminalCursorBlink: &blink,
		TerminalScrollback:  9000,
	})

	if err := a.SetTerminalScrollback(11000); err != nil {
		t.Fatalf("SetTerminalScrollback() error = %v", err)
	}

	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "wss://relay.example.com" ||
		cfg.RelaySessionToken != "secret-token" ||
		!cfg.AllowInsecureRelay ||
		cfg.AutoCheckUpdates != &autoCheck ||
		cfg.LastCheckAt != 123 ||
		cfg.SkipVersion != "v9.9.9" ||
		cfg.TerminalTheme != terminalThemeNord {
		t.Fatalf("SetTerminalScrollback changed unrelated config: %#v", cfg)
	}
	if cfg.TerminalFontHead != "SF Mono" ||
		cfg.TerminalFontSize != 18 ||
		cfg.TerminalLineHeight != 1.3 ||
		cfg.TerminalCursorStyle != "underline" ||
		cfg.TerminalCursorBlink == nil || *cfg.TerminalCursorBlink != false {
		t.Fatalf("SetTerminalScrollback changed a sibling appearance field: %#v", cfg)
	}
	if cfg.TerminalScrollback != 11000 {
		t.Fatalf("TerminalScrollback = %d; want 11000", cfg.TerminalScrollback)
	}
}
