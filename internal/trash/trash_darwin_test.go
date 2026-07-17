//go:build darwin

package trash

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSendDarwinInvokesOsascript(t *testing.T) {
	var got []string
	oldExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	t.Cleanup(func() { execCommand = oldExec })
	if err := Send("/tmp/xxx.txt"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(got) == 0 || got[0] != "osascript" {
		t.Fatalf("expected osascript, got %v", got)
	}
	joined := strings.Join(got[1:], " ")
	if !strings.Contains(joined, `POSIX file "/tmp/xxx.txt"`) {
		t.Fatalf("expected POSIX file arg, got %q", joined)
	}
	if !strings.Contains(joined, `tell application "Finder"`) {
		t.Fatalf("expected Finder script, got %q", joined)
	}
}

func TestSendDarwinFailurePropagates(t *testing.T) {
	oldExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}
	t.Cleanup(func() { execCommand = oldExec })
	if err := Send("/tmp/xxx.txt"); err == nil {
		t.Fatal("expected error propagation from failing osascript")
	}
}

func TestEscapeAppleScript(t *testing.T) {
	cases := []struct{ in, out string }{
		{"plain", "plain"},
		{`with "quote"`, `with \"quote\"`},
		{`with\backslash`, `with\\backslash`},
	}
	for _, c := range cases {
		got := escapeAppleScript(c.in)
		if got != c.out {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}
