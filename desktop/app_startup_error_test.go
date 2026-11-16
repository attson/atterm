package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetStartupErrorIncludesMessageAndLogPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "desktop.log")
	a := newLoggingTestApp(t, appConfig{LogFilePath: logPath})
	a.setStartupFatalError("start relay host", errors.New("open userstore: duplicate column"))

	got := a.GetStartupError()
	if !got.Fatal {
		t.Fatal("Fatal = false; want true")
	}
	if !strings.Contains(got.Message, "start relay host: open userstore: duplicate column") {
		t.Fatalf("Message = %q", got.Message)
	}
	if got.LogPath != logPath {
		t.Fatalf("LogPath = %q; want %q", got.LogPath, logPath)
	}
}

func TestGetStartupErrorEmptyWhenStartupSucceeded(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})

	got := a.GetStartupError()
	if got.Fatal || got.Message != "" {
		t.Fatalf("GetStartupError() = %+v; want empty non-fatal payload", got)
	}
}

func TestConfigLoadFailureBecomesStartupError(t *testing.T) {
	store := &configStore{loadErr: errors.New("parse config.json: unexpected end of JSON input")}
	a := NewApp(store, nil)

	if a.ensureConfigReady() {
		t.Fatal("config with a load failure was accepted for startup")
	}
	got := a.GetStartupError()
	if !got.Fatal || !strings.Contains(got.Message, "load config: parse config.json") {
		t.Fatalf("GetStartupError() = %+v; want visible config load failure", got)
	}
}
