package session

import (
	"path/filepath"
	"strings"
)

// DetectInteractiveShellCommand reports whether command starts a supported
// interactive shell and returns its canonical name.
func DetectInteractiveShellCommand(command string) (shell string, ok bool) {
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return "", false
	}

	// filepath.Base only recognizes the current platform's separator. Commands
	// can arrive from Windows peers, so normalize their separator first.
	base := filepath.Base(strings.ReplaceAll(tokens[0], `\`, "/"))
	shell = canonicalInteractiveShell(base)
	if shell == "" || !safeInteractiveShellArgs(shell, tokens[1:]) {
		return "", false
	}
	return shell, true
}

func canonicalInteractiveShell(base string) string {
	switch base {
	case "bash", "zsh", "fish":
		return base
	}

	powerShellBase := strings.TrimSuffix(strings.ToLower(base), ".exe")
	if powerShellBase == "pwsh" || powerShellBase == "powershell" {
		return "pwsh"
	}
	return ""
}

func safeInteractiveShellArgs(shell string, args []string) bool {
	for _, arg := range args {
		switch shell {
		case "bash", "zsh":
			if arg == "--login" || safeCombinedShortFlags(arg, "il") {
				continue
			}
		case "fish":
			if arg == "--interactive" || arg == "--login" || safeCombinedShortFlags(arg, "il") {
				continue
			}
		case "pwsh":
			switch strings.ToLower(arg) {
			case "-noexit", "-nologo", "-noprofile":
				continue
			}
		}
		return false
	}
	return true
}

func safeCombinedShortFlags(arg, allowed string) bool {
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return false
	}
	for _, flag := range arg[1:] {
		if !strings.ContainsRune(allowed, flag) {
			return false
		}
	}
	return true
}
