package shellintegration

import (
	"os"
	"strings"
	"testing"
)

func TestPrepareBashWritesRcfileAndReturnsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p := prepareBash("sess-bash-1")
	if p.Shell != "bash" {
		t.Fatalf("Plan.Shell = %q; want bash", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; bash prepare must register cleanup")
	}
	defer p.Cleanup()

	if len(p.ExtraArgs) < 3 || p.ExtraArgs[0] != "--rcfile" || p.ExtraArgs[2] != "-i" {
		t.Fatalf("Plan.ExtraArgs = %v; want [--rcfile <path> -i]", p.ExtraArgs)
	}

	rcPath := p.ExtraArgs[1]
	body, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rcfile %s: %v", rcPath, err)
	}
	got := string(body)
	if !strings.Contains(got, "~/.bashrc") {
		t.Fatalf("rcfile does not source ~/.bashrc; got %q", got)
	}
	if !strings.Contains(got, "atterm.bash") {
		t.Fatalf("rcfile does not source atterm.bash; got %q", got)
	}

	foundEnv := false
	for _, env := range p.ExtraEnv {
		if env == "ATTERM_SHELL_INTEGRATION=1" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("Plan.ExtraEnv missing ATTERM_SHELL_INTEGRATION=1; got %v", p.ExtraEnv)
	}

	p.Cleanup()
	if _, err := os.Stat(rcPath); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove rcfile %s: err=%v", rcPath, err)
	}
}
