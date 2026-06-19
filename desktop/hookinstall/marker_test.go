package hookinstall

import "testing"

func TestIsAttermHookCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"empty", "", false},
		{"bare binary on PATH", "atterm-hook", false},
		{"user's homebrew install", "/usr/local/bin/atterm-hook", false},
		{"atterm-managed absolute", "/Users/foo/.atterm/bin/atterm-hook", true},
		{"atterm-managed with args", "/Users/foo/.atterm/bin/atterm-hook --debug", true},
		{"atterm-managed with env prefix", "FOO=1 /Users/foo/.atterm/bin/atterm-hook", true},
		{"different user", "/home/bar/.atterm/bin/atterm-hook", true},
		{"unrelated command containing .atterm", "/usr/bin/grep .atterm/bin/atterm-hook somefile", true /* corner case we accept */},
		{"only substring partial", "/Users/x/.atterm/bin/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isAttermHookCommand(HookEntry{Hooks: []HookCommand{{Type: "command", Command: c.cmd}}})
			if got != c.want {
				t.Errorf("isAttermHookCommand(%q) = %v; want %v", c.cmd, got, c.want)
			}
		})
	}
}
