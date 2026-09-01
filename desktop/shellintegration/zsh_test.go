package shellintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareZshWritesWrapperAndReturnsPlan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// UserCacheDir is HOME-derived on linux/darwin once HOME is set; on macOS
	// it is $HOME/Library/Caches. The test only asserts that the dir lives
	// somewhere under dir; let UserCacheDir resolve naturally.
	t.Setenv("ZDOTDIR", "")

	p := prepareZsh("sess-1234")
	if p.Shell != "zsh" {
		t.Fatalf("Plan.Shell = %q; want zsh", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; zsh prepare must register cleanup")
	}
	defer p.Cleanup()

	zdir := ""
	for _, env := range p.ExtraEnv {
		if strings.HasPrefix(env, "ZDOTDIR=") {
			zdir = strings.TrimPrefix(env, "ZDOTDIR=")
		}
	}
	if zdir == "" {
		t.Fatalf("Plan.ExtraEnv missing ZDOTDIR; got %v", p.ExtraEnv)
	}

	// The wrapper .zshrc must exist and source both the user's rc and the
	// embedded atterm.zsh.
	wrapperRC := filepath.Join(zdir, ".zshrc")
	body, err := os.ReadFile(wrapperRC)
	if err != nil {
		t.Fatalf("read wrapper rc: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "ATTERM_ORIG_ZDOTDIR") {
		t.Fatalf("wrapper rc does not propagate ATTERM_ORIG_ZDOTDIR; got %q", got)
	}
	if !strings.Contains(got, "atterm.zsh") {
		t.Fatalf("wrapper rc does not source atterm.zsh; got %q", got)
	}

	// Regression: macOS /etc/zshrc sets HISTFILE=${ZDOTDIR:-$HOME}/.zsh_history
	// before our wrapper runs. With ZDOTDIR pointed at our per-session shim
	// dir, history gets anchored to a temp file that no other window can see
	// and that Cleanup deletes on exit. The wrapper MUST repair HISTFILE when
	// it still points inside the shim dir, and MUST restore ZDOTDIR so the
	// user's .zshrc sees the original value.
	if !strings.Contains(got, "HISTFILE") {
		t.Fatalf("wrapper rc does not repair HISTFILE after ZDOTDIR shim; got %q", got)
	}
	// The wrapper should restore ZDOTDIR before sourcing the user rc, so any
	// `${ZDOTDIR:-$HOME}/.zshrc`-style logic and user-side $ZDOTDIR references
	// land on the original value rather than our temp dir.
	zdotidx := strings.Index(got, "ZDOTDIR=\"$_atterm_orig\"")
	if zdotidx < 0 {
		// Accept any reassignment of ZDOTDIR from the original value.
		if !(strings.Contains(got, "ZDOTDIR=$ATTERM_ORIG_ZDOTDIR") ||
			strings.Contains(got, "ZDOTDIR=\"${ATTERM_ORIG_ZDOTDIR")) {
			t.Fatalf("wrapper rc does not restore ZDOTDIR to the original; got %q", got)
		}
	}
	srcidx := strings.Index(got, "source ")
	if zdotidx >= 0 && srcidx >= 0 && zdotidx > srcidx {
		t.Fatalf("wrapper rc restores ZDOTDIR after sourcing user rc; must restore first. got %q", got)
	}

	// The snippet itself must be copied alongside so the wrapper can source it
	// without depending on file paths inside the binary.
	if _, err := os.Stat(filepath.Join(zdir, "atterm.zsh")); err != nil {
		t.Fatalf("snippet not written next to wrapper: %v", err)
	}

	// ATTERM_ORIG_ZDOTDIR must be exported so the wrapper can source the
	// user's original rc. Empty original is OK ($HOME fallback).
	foundOrig := false
	for _, env := range p.ExtraEnv {
		if strings.HasPrefix(env, "ATTERM_ORIG_ZDOTDIR=") {
			foundOrig = true
		}
	}
	if !foundOrig {
		t.Fatalf("Plan.ExtraEnv missing ATTERM_ORIG_ZDOTDIR; got %v", p.ExtraEnv)
	}

	// Cleanup should remove the temp dir.
	p.Cleanup()
	if _, err := os.Stat(zdir); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove dir %s: err=%v", zdir, err)
	}
}

func TestPrepareZshWritesChainShimsForZshenvAndZprofile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ZDOTDIR", "")

	p := prepareZsh("sess-chain")
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil")
	}
	defer p.Cleanup()

	zdir := ""
	for _, env := range p.ExtraEnv {
		if strings.HasPrefix(env, "ZDOTDIR=") {
			zdir = strings.TrimPrefix(env, "ZDOTDIR=")
		}
	}
	if zdir == "" {
		t.Fatalf("Plan.ExtraEnv missing ZDOTDIR; got %v", p.ExtraEnv)
	}

	// zsh resolves .zshenv and .zprofile against $ZDOTDIR too. Without shim
	// counterparts the user's ~/.zprofile (brew shellenv on macOS) never
	// runs and PATH-dependent plugins break inside atterm. Each shim must
	// chain to the user's file and then point ZDOTDIR back at the shim dir
	// so the .zshrc wrapper still runs.
	for _, name := range []string{".zshenv", ".zprofile"} {
		body, err := os.ReadFile(filepath.Join(zdir, name))
		if err != nil {
			t.Fatalf("shim %s not written: %v", name, err)
		}
		got := string(body)
		if !strings.Contains(got, "${ZDOTDIR:-$HOME}/"+name) {
			t.Fatalf("shim %s does not chain to the user's %s; got %q", name, name, got)
		}
		reshim := strings.Index(got, `ZDOTDIR="$_atterm_shim_dir"`)
		src := strings.Index(got, "source ")
		if reshim < 0 {
			t.Fatalf("shim %s does not restore ZDOTDIR to the shim dir; got %q", name, got)
		}
		if src >= 0 && reshim < src {
			t.Fatalf("shim %s re-shims ZDOTDIR before sourcing the user file; got %q", name, got)
		}
	}
}

func TestPrepareZshReturnsZeroPlanWhenCacheDirFails(t *testing.T) {
	// Force os.UserCacheDir to fail by clearing both HOME and XDG_CACHE_HOME
	// and (on darwin) HOME-derived candidates.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	p := prepareZsh("sess-empty")
	if p.Shell != "" || len(p.ExtraEnv) != 0 || p.Cleanup != nil {
		t.Fatalf("expected zero Plan on cache-dir failure; got %+v", p)
	}
}
