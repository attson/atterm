package main

import (
	"os"
	"path/filepath"
	"runtime"
)

func defaultLogFilePath() string {
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "Library", "Logs", "AT-Term", "desktop.log")
		}
	case "windows":
		if base := os.Getenv("LocalAppData"); base != "" {
			return filepath.Join(base, "ATTerm", "Logs", "desktop.log")
		}
		if base, err := os.UserCacheDir(); err == nil && base != "" {
			return filepath.Join(base, "ATTerm", "Logs", "desktop.log")
		}
	default:
		if base := os.Getenv("XDG_STATE_HOME"); base != "" {
			return filepath.Join(base, "atterm", "desktop.log")
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state", "atterm", "desktop.log")
		}
	}
	return "desktop.log"
}
