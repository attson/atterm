// Package session — session type classification.
//
// ClassifyCommand reads a command line and returns one of five labels.
// Used by applyOSC133Locked when a C event reports a new command, with
// sticky-non-shell semantics: a returned "shell" never overwrites a
// previously-set non-shell Type on the Session.
package session

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Type labels exported to clients via proto.SessionInfo.Type.
const (
	SessionTypeShell  = "shell"
	SessionTypeAI     = "ai"
	SessionTypeTest   = "test"
	SessionTypeBuild  = "build"
	SessionTypeDeploy = "deploy"
)

var envAssignRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`)

// wrapperCommands take another command as an argument. We strip them while
// finding the "real" first token of a command line.
var wrapperCommands = map[string]struct{}{
	"sudo": {}, "time": {}, "nice": {}, "env": {},
}

// ClassifyCommand returns one of the SessionType* constants for cmd.
// Pure / total / O(len(cmd)).
func ClassifyCommand(cmd string) string {
	first, second := commandExecutableTokens(cmd)
	if first == "" {
		return SessionTypeShell
	}

	switch first {
	case "codex", "claude", "gemini", "aider":
		return SessionTypeAI
	case "kubectl", "terraform":
		return SessionTypeDeploy
	case "docker-compose":
		return SessionTypeBuild
	case "docker":
		if second == "build" || second == "compose" {
			return SessionTypeBuild
		}
	case "go", "npm", "pnpm", "yarn", "cargo":
		if second == "test" {
			return SessionTypeTest
		}
	}
	return SessionTypeShell
}

func commandExecutableTokens(cmd string) (first, second string) {
	tokens := strings.Fields(cmd)
	// Strip wrappers and POSIX env-var prefixes from the front.
	for len(tokens) > 0 {
		t := tokens[0]
		if _, ok := wrapperCommands[t]; ok {
			tokens = tokens[1:]
			continue
		}
		if envAssignRE.MatchString(t) {
			tokens = tokens[1:]
			continue
		}
		break
	}
	if len(tokens) == 0 {
		return "", ""
	}
	first = filepath.Base(tokens[0])
	if len(tokens) > 1 {
		second = tokens[1]
	}
	return first, second
}
