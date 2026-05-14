package shellintegration

import (
	"os"
	"strings"
	"testing"
)

func TestPreparePwshWritesScriptAndReturnsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p := preparePwsh("sess-pwsh-1")
	if p.Shell != "pwsh" {
		t.Fatalf("Plan.Shell = %q; want pwsh", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; pwsh prepare must register cleanup")
	}
	defer p.Cleanup()

	// Expect args: -NoExit -Command "& '<path>'"
	if len(p.ExtraArgs) != 4 {
		t.Fatalf("Plan.ExtraArgs has %d entries; want 4 (-NoExit -Command \"& '<path>'\"); got %v", len(p.ExtraArgs), p.ExtraArgs)
	}
	if p.ExtraArgs[0] != "-NoExit" || p.ExtraArgs[1] != "-Command" {
		t.Fatalf("Plan.ExtraArgs[0:2] = %v; want [-NoExit -Command]", p.ExtraArgs[0:2])
	}
	if !strings.HasPrefix(p.ExtraArgs[2], "& '") || !strings.HasSuffix(p.ExtraArgs[2], "'") {
		t.Fatalf("Plan.ExtraArgs[2] = %q; want \"& '<path>'\" form", p.ExtraArgs[2])
	}
	if p.ExtraArgs[3] != "-" {
		// We append a trailing '-' so PowerShell drops to the interactive
		// prompt after the script. (Without it, pwsh exits when -Command
		// completes despite -NoExit on some shells.)
		t.Fatalf("Plan.ExtraArgs[3] = %q; want '-'", p.ExtraArgs[3])
	}

	scriptPath := strings.TrimSuffix(strings.TrimPrefix(p.ExtraArgs[2], "& '"), "'")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script %s: %v", scriptPath, err)
	}
	if !strings.Contains(string(body), "ATTERM_SHELL_INTEGRATION") {
		t.Fatalf("script body missing snippet markers; got %q", string(body))
	}

	p.Cleanup()
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove script %s: err=%v", scriptPath, err)
	}
}
