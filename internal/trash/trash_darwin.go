//go:build darwin

package trash

import (
	"fmt"
	"strings"
)

// sendPlatform uses AppleScript (osascript) so we don't have to link any
// Cocoa framework or the /usr/local Homebrew `trash` binary.
func sendPlatform(path string) error {
	esc := escapeAppleScript(path)
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file "%s"`, esc)
	cmd := execCommand("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("trash: osascript: %w", err)
	}
	return nil
}

func escapeAppleScript(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
