package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func loadConfigStoreFrom(cfg appConfig) *configStore {
	return &configStore{cfg: cfg}
}

func newTestLoggingManager(t *testing.T, cfg appConfig) *loggingManager {
	t.Helper()
	m, err := newLoggingManager(loggingOptions{
		devMode:  false,
		terminal: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	if err := m.Apply(loggingConfigState{
		enabled: cfg.LogToFileEnabledOrDefault(),
		path:    cfg.LogFilePath,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	t.Cleanup(func() {
		_ = m.Close()
	})
	return m
}

func newLoggingTestApp(t *testing.T, cfg appConfig) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))
	return &App{
		cfgStore: loadConfigStoreFrom(cfg),
		logger:   newTestLoggingManager(t, cfg),
	}
}

func TestGetLoggingConfigReturnsEffectiveDefaults(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	cfg := a.GetLoggingConfig()
	if !cfg.Enabled {
		t.Fatalf("Enabled = false; want true")
	}
	if cfg.EffectivePath == "" {
		t.Fatal("EffectivePath = empty; want platform default")
	}
}

func TestSetLoggingConfigPersistsAndReconfigures(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	customPath := filepath.Join(t.TempDir(), "custom.log")

	if err := a.SetLoggingConfig(LoggingConfig{
		Enabled: true,
		Path:    customPath,
	}); err != nil {
		t.Fatalf("SetLoggingConfig() error = %v", err)
	}

	cfg := a.cfgStore.Get()
	if cfg.LogFilePath != customPath {
		t.Fatalf("LogFilePath = %q; want %q", cfg.LogFilePath, customPath)
	}
	if cfg.LogToFileEnabled == nil || !*cfg.LogToFileEnabled {
		t.Fatalf("LogToFileEnabled = %#v; want true", cfg.LogToFileEnabled)
	}
}

func TestGetLogPreviewReturnsRecentContent(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	a.logger.Logger().Print("preview me")

	preview, err := a.GetLogPreview()
	if err != nil {
		t.Fatalf("GetLogPreview() error = %v", err)
	}
	if !strings.Contains(preview.Content, "preview me") {
		t.Fatalf("preview content missing log line: %q", preview.Content)
	}
}
