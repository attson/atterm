package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFS returns a PluginFS rooted at a temp dir that simulates the user's
// HOME for the lifetime of the test.
func makeFS(t *testing.T) (*PluginFS, string) {
	t.Helper()
	home := t.TempDir()
	fs := &PluginFS{
		allowRoots: []string{home},
	}
	return fs, home
}

func TestResolveAcceptsPathInsideAllowRoot(t *testing.T) {
	fs, home := makeFS(t)
	sub := filepath.Join(home, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := fs.resolve(sub)
	if err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
	// On macOS, t.TempDir() returns /var/... which EvalSymlinks resolves to /private/var/...
	// So we compare EvalSymlinks-resolved versions
	wantSub, _ := filepath.EvalSymlinks(sub)
	if got != wantSub {
		t.Errorf("resolve returned %q, want %q", got, wantSub)
	}
}

func TestResolveRejectsRelativePath(t *testing.T) {
	fs, _ := makeFS(t)
	if _, err := fs.resolve("relative/path"); err == nil {
		t.Fatal("expected reject of relative path")
	}
}

func TestResolveRejectsOutsideRoot(t *testing.T) {
	fs, _ := makeFS(t)
	tmp := t.TempDir()
	if _, err := fs.resolve(tmp); err == nil {
		t.Fatalf("expected reject of %q outside allow roots", tmp)
	}
}

func TestResolveRejectsParentTraversal(t *testing.T) {
	fs, home := makeFS(t)
	bad := filepath.Join(home, "..", "..", "etc")
	if _, err := fs.resolve(bad); err == nil {
		t.Fatal("expected reject of .. traversal")
	}
}

func TestResolveResolvesSymlinkBeforeChecking(t *testing.T) {
	fs, home := makeFS(t)
	outside := t.TempDir()
	link := filepath.Join(home, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := fs.resolve(link); err == nil {
		t.Fatal("expected reject of symlink pointing outside roots")
	}
}

func TestResolveRejectsDenyPattern(t *testing.T) {
	fs, home := makeFS(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(ssh); err == nil {
		t.Fatal("expected reject of ~/.ssh")
	}
	envFile := filepath.Join(home, "app", ".env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("X=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(envFile); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected deny on .env, got %v", err)
	}
}

func TestListDirReturnsEntries(t *testing.T) {
	fs, home := makeFS(t)
	if err := os.Mkdir(filepath.Join(home, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ListDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	var foundDir, foundFile bool
	for _, e := range got {
		if e.Name == "sub" && e.IsDir {
			foundDir = true
		}
		if e.Name == "f.txt" && !e.IsDir && e.Size == 2 {
			foundFile = true
		}
	}
	if !foundDir || !foundFile {
		t.Fatalf("entries did not include expected dir+file: %+v", got)
	}
}

func TestListDirRefusesOutsideRoots(t *testing.T) {
	fs, _ := makeFS(t)
	if _, err := fs.ListDir(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestReadFileSuccessAndTruncation(t *testing.T) {
	fs, home := makeFS(t)
	big := make([]byte, 200)
	for i := range big {
		big[i] = 'A'
	}
	path := filepath.Join(home, "f.txt")
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != string(big) {
		t.Fatal("data mismatch")
	}
	if got.TruncatedAt != 0 {
		t.Fatalf("expected no truncation, got %d", got.TruncatedAt)
	}

	got2, err := fs.ReadFile(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Data) != 50 || got2.TruncatedAt != 200 {
		t.Fatalf("unexpected truncation result: %+v", got2)
	}
}

func TestReadFileRejectsTooLarge(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "huge.txt")
	if err := os.WriteFile(path, make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(path, maxReadBytesHard+1); err == nil {
		t.Fatal("expected hard-cap rejection")
	}
}

func TestReadFileDetectsBinary(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "bin")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsBinary {
		t.Fatal("expected IsBinary=true")
	}
}

func TestFileMeta(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.FileMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 5 || got.IsBinary {
		t.Fatalf("unexpected meta %+v", got)
	}
}

func TestRevealInOSRefusesForbidden(t *testing.T) {
	fs, _ := makeFS(t)
	if err := fs.RevealInOS(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestOpenExternalRefusesForbidden(t *testing.T) {
	fs, _ := makeFS(t)
	if err := fs.OpenExternal(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestWatchUnwatchLifecycle(t *testing.T) {
	fs, home := makeFS(t)
	fs.setupWatcher(context.Background())
	defer fs.shutdownWatcher()

	id, err := fs.WatchDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero handle id")
	}
	if err := fs.UnwatchDir(id); err != nil {
		t.Fatal(err)
	}
}

func TestWatchDirCapEnforced(t *testing.T) {
	fs, home := makeFS(t)
	fs.setupWatcher(context.Background())
	defer fs.shutdownWatcher()

	// Create more than the cap of subdirs.
	for i := 0; i <= maxWatchers; i++ {
		d := filepath.Join(home, fmt.Sprintf("d%d", i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	good := 0
	for i := 0; i < maxWatchers+1; i++ {
		_, err := fs.WatchDir(filepath.Join(home, fmt.Sprintf("d%d", i)))
		if err == nil {
			good++
		}
	}
	if good != maxWatchers {
		t.Fatalf("expected %d successful watches, got %d", maxWatchers, good)
	}
}
