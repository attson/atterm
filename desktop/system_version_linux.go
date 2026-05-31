//go:build linux

package main

import (
	"os"
	"regexp"
	"strings"
)

var prettyNameRE = regexp.MustCompile(`(?m)^PRETTY_NAME="([^"]+)"`)

// systemVersion reads /etc/os-release and returns PRETTY_NAME if present.
// Returns empty string on any failure.
func systemVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	m := prettyNameRE.FindStringSubmatch(string(data))
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
