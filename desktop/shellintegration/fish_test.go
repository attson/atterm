package shellintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareFishWritesToConfdAndReturnsNilCleanup(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p := prepareFish()
	if p.Shell != "fish" {
		t.Fatalf("Plan.Shell = %q; want fish", p.Shell)
	}
	if p.Cleanup != nil {
		t.Fatalf("fish Plan.Cleanup must be nil; got non-nil")
	}
	if len(p.ExtraArgs) != 0 {
		t.Fatalf("fish Plan.ExtraArgs must be empty; got %v", p.ExtraArgs)
	}

	target := filepath.Join(xdg, "fish", "conf.d", "atterm-integration.fish")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("conf.d snippet not written to %s: %v", target, err)
	}
	if !strings.Contains(string(body), "fish_preexec") {
		t.Fatalf("conf.d snippet missing fish_preexec hook; got %q", string(body))
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
}

func TestPrepareFishFallsBackToHomeConfigWhenXdgUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	p := prepareFish()
	if p.Shell != "fish" {
		t.Fatalf("Plan.Shell = %q; want fish", p.Shell)
	}

	target := filepath.Join(home, ".config", "fish", "conf.d", "atterm-integration.fish")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected fallback path %s to exist: %v", target, err)
	}
}

func TestPrepareFishOverwriteIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	p1 := prepareFish()
	if p1.Shell != "fish" {
		t.Fatalf("first prepare returned non-fish plan: %+v", p1)
	}

	p2 := prepareFish()
	if p2.Shell != "fish" {
		t.Fatalf("second prepare returned non-fish plan: %+v", p2)
	}
}
