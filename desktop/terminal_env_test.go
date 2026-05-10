package main

import "testing"

func TestTerminalEnvForXtermOverridesInheritedTerminalIdentity(t *testing.T) {
	env := terminalEnvForXterm([]string{
		"PATH=/bin",
		"TERM=dumb",
		"COLORTERM=old",
		"TERM_PROGRAM=iTerm.app",
	})

	assertEnvValue(t, env, "PATH", "/bin")
	assertEnvValue(t, env, "TERM", "xterm-256color")
	assertEnvValue(t, env, "COLORTERM", "truecolor")
	assertEnvValue(t, env, "TERM_PROGRAM", "atterm")
	assertEnvCount(t, env, "TERM", 1)
	assertEnvCount(t, env, "COLORTERM", 1)
	assertEnvCount(t, env, "TERM_PROGRAM", 1)
}

func TestTerminalEnvForXtermAddsMissingTerminalIdentity(t *testing.T) {
	env := terminalEnvForXterm([]string{"PATH=/bin"})

	assertEnvValue(t, env, "TERM", "xterm-256color")
	assertEnvValue(t, env, "COLORTERM", "truecolor")
	assertEnvValue(t, env, "TERM_PROGRAM", "atterm")
}

func assertEnvValue(t *testing.T, env []string, key, want string) {
	t.Helper()
	prefix := key + "="
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			if got := entry[len(prefix):]; got != want {
				t.Fatalf("%s = %q; want %q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("%s missing from env %v", key, env)
}

func assertEnvCount(t *testing.T, env []string, key string, want int) {
	t.Helper()
	prefix := key + "="
	var got int
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s appeared %d times; want %d in %v", key, got, want, env)
	}
}
