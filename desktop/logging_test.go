package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggingManagerDevModeMirrorsToTerminalAndFile(t *testing.T) {
	dir := t.TempDir()
	var terminal bytes.Buffer
	m, err := newLoggingManager(loggingOptions{
		devMode:       true,
		terminal:      &terminal,
		maxBytes:      1024,
		maxBackups:    2,
		defaultPathFn: func() string { return filepath.Join(dir, "desktop.log") },
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	defer m.Close()

	if err := m.Apply(loggingConfigState{enabled: true, path: ""}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	m.Logger().Print("hello dev logging")

	data, err := os.ReadFile(filepath.Join(dir, "desktop.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "hello dev logging") {
		t.Fatalf("file log missing line: %s", data)
	}
	if !strings.Contains(terminal.String(), "hello dev logging") {
		t.Fatalf("terminal log missing line: %s", terminal.String())
	}
}

func TestLoggingManagerRotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	m, err := newLoggingManager(loggingOptions{
		devMode:       false,
		terminal:      &bytes.Buffer{},
		maxBytes:      64,
		maxBackups:    2,
		defaultPathFn: func() string { return filepath.Join(dir, "desktop.log") },
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	defer m.Close()

	if err := m.Apply(loggingConfigState{enabled: true}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	m.Logger().Print(strings.Repeat("a", 80))
	m.Logger().Print(strings.Repeat("b", 80))
	m.Logger().Print(strings.Repeat("c", 80))

	if _, err := os.Stat(filepath.Join(dir, "desktop.log.1")); err != nil {
		t.Fatalf("expected rotated file .1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop.log.2")); err != nil {
		t.Fatalf("expected rotated file .2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop.log.3")); !os.IsNotExist(err) {
		t.Fatalf("expected no file .3; got err=%v", err)
	}
}

func TestLogPreviewReturnsTailAndTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop.log")
	content := strings.Repeat("0123456789", 40)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	preview, err := readLogPreview(path, 32)
	if err != nil {
		t.Fatalf("readLogPreview() error = %v", err)
	}
	if !preview.Exists || !preview.Truncated {
		t.Fatalf("preview = %#v; want exists and truncated", preview)
	}
	if len(preview.Content) == 0 || len(preview.Content) > 32 {
		t.Fatalf("preview content length = %d; want 1..32", len(preview.Content))
	}
}
