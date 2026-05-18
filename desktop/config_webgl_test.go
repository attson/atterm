package main

import "testing"

// WebGL renderer default is platform-specific because NVIDIA proprietary +
// X11 + WebKitGTK paints the cursor / last cell on a delayed schedule, which
// surfaces as visible typing lag even when CPU is idle. macOS and Windows
// keep WebGL on so the #33 light-theme ghosting fix stays active.

func TestWebglRendererDefaultLinuxFalse(t *testing.T) {
	if defaultWebglRendererEnabledFor("linux") {
		t.Fatal("Linux default should be false (NVIDIA + X11 + WebKitGTK lag)")
	}
}

func TestWebglRendererDefaultDarwinTrue(t *testing.T) {
	if !defaultWebglRendererEnabledFor("darwin") {
		t.Fatal("macOS default should be true to keep #33 ghosting fix active")
	}
}

func TestWebglRendererDefaultWindowsTrue(t *testing.T) {
	if !defaultWebglRendererEnabledFor("windows") {
		t.Fatal("Windows default should be true to keep #33 ghosting fix active")
	}
}

func TestWebglRendererRoundtripFalse(t *testing.T) {
	f := false
	cfg := appConfig{WebglRendererEnabled: &f}
	if cfg.WebglRendererEnabledOrDefault() {
		t.Fatal("explicit false should not be overridden by the platform default")
	}
}

func TestWebglRendererRoundtripTrue(t *testing.T) {
	tr := true
	cfg := appConfig{WebglRendererEnabled: &tr}
	if !cfg.WebglRendererEnabledOrDefault() {
		t.Fatal("explicit true should be preserved")
	}
}
