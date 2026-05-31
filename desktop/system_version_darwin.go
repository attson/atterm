//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// systemVersion returns the macOS product version (e.g. "14.6.1") via
// `sw_vers -productVersion`. Returns empty string on any failure.
func systemVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
