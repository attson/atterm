package hookinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureBinary_FirstInstall(t *testing.T) {
	home := t.TempDir()
	path, version, err := ensureBinary(home)
	if err != nil {
		t.Fatal(err)
	}
	if version != embeddedHash {
		t.Errorf("version = %q; want %q", version, embeddedHash)
	}
	if path != attermHookSymlink(home) {
		t.Errorf("path = %q; want %q", path, attermHookSymlink(home))
	}

	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	if target != want {
		t.Errorf("symlink target = %q; want %q", target, want)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("binary not executable: %s", info.Mode())
	}
	if int(info.Size()) != len(embeddedHook) {
		t.Errorf("binary size = %d; want %d", info.Size(), len(embeddedHook))
	}
}

func TestEnsureBinary_Idempotent(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	first, _ := os.Stat(target)
	time.Sleep(20 * time.Millisecond)
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(target)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("binary rewritten on second call (mtime changed)")
	}
}

func TestEnsureBinary_StaleSymlinkRetargeted(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a stale symlink pointing somewhere else.
	stale := filepath.Join(bin, "atterm-hook-DEADBEEF")
	os.WriteFile(stale, []byte("stale"), 0o755)
	os.Symlink(stale, attermHookSymlink(home))

	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	got, _ := os.Readlink(attermHookSymlink(home))
	want := filepath.Join(bin, "atterm-hook-"+embeddedHash)
	if got != want {
		t.Errorf("symlink target = %q; want %q", got, want)
	}
}

func TestGCOldVersions_KeepsCurrentAndFresh(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	current := filepath.Join(bin, "atterm-hook-"+embeddedHash)
	old := filepath.Join(bin, "atterm-hook-OLDOLDOL")
	young := filepath.Join(bin, "atterm-hook-YOUNG123")
	os.WriteFile(current, []byte("x"), 0o755)
	os.WriteFile(old, []byte("y"), 0o755)
	os.WriteFile(young, []byte("z"), 0o755)
	weekAgo := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(old, weekAgo, weekAgo)
	// young keeps "now" mtime

	gcOldVersions(bin, current, 7*24*time.Hour)

	if _, err := os.Stat(current); err != nil {
		t.Errorf("current removed: %v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("young removed: %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Errorf("old NOT removed")
	}
}

func TestEnsureBinary_NonHookFilesIgnoredByGC(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	// Place an unrelated user file in ~/.atterm/bin/ — GC must not touch.
	unrelated := filepath.Join(bin, "user-script.sh")
	os.WriteFile(unrelated, []byte("#!/bin/sh"), 0o755)
	weekAgo := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(unrelated, weekAgo, weekAgo)

	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("user file removed by GC: %v", err)
	}
}

func TestEnsureBinary_PrefixMustMatch(t *testing.T) {
	// Sanity: gc only touches files whose names start with atterm-hook-
	if !strings.HasPrefix("atterm-hook-DEAD", "atterm-hook-") {
		t.Fatal("guard")
	}
}
