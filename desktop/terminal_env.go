package main

import "strings"

const (
	xtermTerm       = "xterm-256color"
	xtermColorTerm  = "truecolor"
	attermTermProg  = "atterm"
	envKeyTerm      = "TERM"
	envKeyColorTerm = "COLORTERM"
	envKeyTermProg  = "TERM_PROGRAM"
)

// terminalEnvForXterm gives child shells the capabilities of the renderer they
// actually talk to, not the environment the desktop process was launched from.
func terminalEnvForXterm(base []string) []string {
	env := append([]string(nil), base...)
	env = setEnv(env, envKeyTerm, xtermTerm)
	env = setEnv(env, envKeyColorTerm, xtermColorTerm)
	env = setEnv(env, envKeyTermProg, attermTermProg)
	// Ensure the shell boots in a UTF-8 locale. Without this, zsh's ZLE
	// falls back to C / POSIX classification and treats bytes 0x80-0x9F as
	// invisible C1 controls, rendering them as literal "<00NN>" — which
	// shreds any multi-byte UTF-8 character (e.g. "发布", 布) whose
	// continuation bytes happen to land in that range. We only inject these
	// when the user has not set them themselves, so the parent shell's
	// preference still wins.
	env = setEnvIfMissing(env, "LANG", "en_US.UTF-8")
	env = setEnvIfMissing(env, "LC_CTYPE", "en_US.UTF-8")
	return env
}

func setEnvIfMissing(env []string, key, value string) []string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) && len(entry) > len(prefix) {
			return env
		}
	}
	return append(env, prefix+value)
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	found := false
	out := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				out = append(out, prefix+value)
				found = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !found {
		out = append(out, prefix+value)
	}
	return out
}
