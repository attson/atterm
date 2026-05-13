package main

import "testing"

func TestTerminalThemeOrDefaultFallsBackToClassic(t *testing.T) {
	tests := []struct {
		name string
		cfg  appConfig
	}{
		{name: "empty", cfg: appConfig{}},
		{name: "unknown", cfg: appConfig{TerminalTheme: "gruvbox"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.TerminalThemeOrDefault(); got != terminalThemeClassic {
				t.Fatalf("TerminalThemeOrDefault() = %q; want %q", got, terminalThemeClassic)
			}
		})
	}
}

func TestTerminalThemeOrDefaultAcceptsSupportedThemes(t *testing.T) {
	for _, theme := range supportedTerminalThemes() {
		cfg := appConfig{TerminalTheme: theme}
		if got := cfg.TerminalThemeOrDefault(); got != theme {
			t.Fatalf("TerminalThemeOrDefault(%q) = %q; want %q", theme, got, theme)
		}
	}
}

func TestIsSupportedTerminalTheme(t *testing.T) {
	for _, theme := range supportedTerminalThemes() {
		if !isSupportedTerminalTheme(theme) {
			t.Fatalf("isSupportedTerminalTheme(%q) = false; want true", theme)
		}
	}
	if isSupportedTerminalTheme("") {
		t.Fatalf("isSupportedTerminalTheme(empty) = true; want false")
	}
	if isSupportedTerminalTheme("classic ") {
		t.Fatalf("isSupportedTerminalTheme(classic with space) = true; want false")
	}
}
