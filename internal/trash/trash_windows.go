//go:build windows

package trash

import (
	"fmt"
	"strings"
)

// sendPlatform uses PowerShell + Shell.Application COM to route the path to
// the Recycle Bin. Available on every supported Windows.
func sendPlatform(path string) error {
	esc := strings.ReplaceAll(path, "'", "''")
	script := fmt.Sprintf(
		`$shell = New-Object -ComObject 'Shell.Application'; `+
			`$item = $shell.Namespace((Split-Path -Parent '%s')).ParseName((Split-Path -Leaf '%s')); `+
			`if ($item -ne $null) { $item.InvokeVerb('delete') } else { exit 1 }`,
		esc, esc,
	)
	cmd := execCommand("powershell", "-NoProfile", "-Command", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("trash: powershell: %w", err)
	}
	return nil
}
