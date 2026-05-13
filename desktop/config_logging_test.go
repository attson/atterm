package main

import "testing"

func TestLogToFileEnabledOrDefault(t *testing.T) {
	if got := (appConfig{}).LogToFileEnabledOrDefault(); !got {
		t.Fatalf("LogToFileEnabledOrDefault() = false; want true")
	}

	enabled := true
	if got := (appConfig{LogToFileEnabled: &enabled}).LogToFileEnabledOrDefault(); !got {
		t.Fatalf("LogToFileEnabledOrDefault(true) = false; want true")
	}

	disabled := false
	if got := (appConfig{LogToFileEnabled: &disabled}).LogToFileEnabledOrDefault(); got {
		t.Fatalf("LogToFileEnabledOrDefault(false) = true; want false")
	}
}

func TestLogFilePathOrDefaultPreservesCustomPath(t *testing.T) {
	cfg := appConfig{LogFilePath: "/tmp/custom-atterm.log"}
	if got := cfg.LogFilePathOrDefault(); got != "/tmp/custom-atterm.log" {
		t.Fatalf("LogFilePathOrDefault() = %q; want %q", got, "/tmp/custom-atterm.log")
	}
}

func TestDefaultLogFilePathIsNotEmpty(t *testing.T) {
	if got := defaultLogFilePath(); got == "" {
		t.Fatal("defaultLogFilePath() = empty; want platform default")
	}
}
