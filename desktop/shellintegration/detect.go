// Package shellintegration prepares per-shell injection plans so that
// atterm-spawned PTYs emit OSC 133 command boundary markers.
package shellintegration

import (
	"path/filepath"
	"strings"
)

// Shell enumerates the shells we know how to inject into. ShellUnknown is the
// fallback for empty paths, unsupported shells (cmd.exe, nu, xonsh, elvish),
// or detection failures.
type Shell int

const (
	ShellUnknown Shell = iota
	ShellZsh
	ShellBash
	ShellFish
	ShellPwsh
)

func (s Shell) String() string {
	switch s {
	case ShellZsh:
		return "zsh"
	case ShellBash:
		return "bash"
	case ShellFish:
		return "fish"
	case ShellPwsh:
		return "pwsh"
	default:
		return ""
	}
}

// DetectShell maps a shell binary path to a Shell. Comparison is on the
// lowercased basename without the .exe suffix; absolute paths and bare
// names both work. Both Unix (/) and Windows (\) separators are handled
// so that Windows paths are correctly resolved on any host OS.
func DetectShell(path string) Shell {
	if path == "" {
		return ShellUnknown
	}
	// filepath.Base only splits on the host OS separator; handle the
	// opposite-OS separator explicitly so Windows paths work on Unix hosts.
	normalized := strings.ReplaceAll(path, `\`, "/")
	base := strings.ToLower(filepath.Base(normalized))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "zsh":
		return ShellZsh
	case "bash":
		return ShellBash
	case "fish":
		return ShellFish
	case "pwsh", "powershell":
		return ShellPwsh
	default:
		return ShellUnknown
	}
}
