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
	return env
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
