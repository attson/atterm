//go:build windows

package main

import (
	"os/exec"
	"strings"
)

// systemVersion runs `cmd /c ver` which prints
// "Microsoft Windows [Version X.Y.Z]". Returns the trimmed line, or
// empty string on failure.
func systemVersion() string {
	out, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
