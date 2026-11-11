package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeFS returns a PluginFS rooted at a temp dir that simulates the user's
// HOME for the lifetime of the test.
func makeFS(t *testing.T) (*PluginFS, string) {
	t.Helper()
	home := t.TempDir()
	fs := &PluginFS{
		// PluginFS is the local Wails path: no relay, so .env is readable.
		access: newFSAccess([]string{home}, false),
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
	// .env is readable on the local path: PluginFS is the Wails direct
	// binding, so nothing here ever reaches a relay.
	envFile := filepath.Join(home, "app", ".env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("X=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(envFile); err != nil {
		t.Fatalf("expected .env to resolve on the local path, got %v", err)
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

func TestResolveRevealPathAllowsExistingPathOutsideFileExplorerRoots(t *testing.T) {
	fs, _ := makeFS(t)
	outside := t.TempDir()
	path := filepath.Join(outside, "system.desktop")
	if err := os.WriteFile(path, []byte("desktop entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(path); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("file explorer resolve error = %v, want ErrPathForbidden", err)
	}
	got, err := resolveRevealPath(path)
	if err != nil {
		t.Fatalf("resolveRevealPath() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolveRevealPath() = %q, want %q", got, want)
	}
}

func TestResolveRevealPathRejectsRelativeAndMissingPaths(t *testing.T) {
	if _, err := resolveRevealPath("relative/path"); !errors.Is(err, ErrPathRelative) {
		t.Fatalf("relative path error = %v, want ErrPathRelative", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := resolveRevealPath(missing); err == nil {
		t.Fatal("expected missing path to be rejected")
	}
}

func TestRevealPathInOSUsesFileManagerShowItemsOnLinux(t *testing.T) {
	const path = "/usr/share/applications/google-chrome.desktop"
	shown := ""
	err := revealPathInOS(
		"linux",
		"COSMIC",
		path,
		func(got string) error {
			shown = got
			return nil
		},
		func(string) (string, error) {
			t.Fatal("fallback command lookup should not run")
			return "", nil
		},
		func(*exec.Cmd) error {
			t.Fatal("fallback command should not start")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("revealPathInOS() error = %v", err)
	}
	if shown != path {
		t.Fatalf("shown path = %q, want %q", shown, path)
	}
}

func TestRevealPathInOSFallsBackWhenShowItemsIsUnavailable(t *testing.T) {
	const path = "/home/me/Music/demo.mp4"
	var started []string
	err := revealPathInOS(
		"linux",
		"COSMIC",
		path,
		func(string) error { return errors.New("service unavailable") },
		func(name string) (string, error) {
			if name != "cosmic-files" {
				t.Fatalf("lookPath name = %q", name)
			}
			return "/usr/bin/cosmic-files", nil
		},
		func(cmd *exec.Cmd) error {
			started = append([]string(nil), cmd.Args...)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("revealPathInOS() error = %v", err)
	}
	want := []string{"/usr/bin/cosmic-files", "/home/me/Music"}
	if fmt.Sprint(started) != fmt.Sprint(want) {
		t.Fatalf("started args = %v, want %v", started, want)
	}
}

func TestFileManagerURI(t *testing.T) {
	got := fileManagerURI("/tmp/a file.desktop")
	want := "file:///tmp/a%20file.desktop"
	if got != want {
		t.Fatalf("fileManagerURI() = %q, want %q", got, want)
	}
}

func TestRevealInOSCommandUsesCosmicFilesOnCOSMIC(t *testing.T) {
	cmd := revealInOSCommand(
		"linux",
		"COSMIC",
		"/home/me/.local/share/applications/browser.desktop",
		func(name string) (string, error) {
			if name != "cosmic-files" {
				t.Fatalf("lookPath name = %q", name)
			}
			return "/usr/bin/cosmic-files", nil
		},
	)
	want := []string{"/usr/bin/cosmic-files", "/home/me/.local/share/applications"}
	if fmt.Sprint(cmd.Args) != fmt.Sprint(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
}

func TestRevealInOSCommandFallsBackToXDGOpen(t *testing.T) {
	cmd := revealInOSCommand(
		"linux",
		"COSMIC:Wayland",
		"/home/me/Music/demo.mp4",
		func(string) (string, error) { return "", errors.New("not installed") },
	)
	want := []string{"xdg-open", "/home/me/Music"}
	if fmt.Sprint(cmd.Args) != fmt.Sprint(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
}

func TestRevealInOSCommandKeepsNativePlatformVerbs(t *testing.T) {
	lookPath := func(string) (string, error) { return "", errors.New("unused") }
	mac := revealInOSCommand("darwin", "", "/Users/me/file.txt", lookPath)
	if got, want := fmt.Sprint(mac.Args), "[open -R /Users/me/file.txt]"; got != want {
		t.Fatalf("mac args = %s, want %s", got, want)
	}
	windows := revealInOSCommand("windows", "", `C:\\Users\\me\\file.txt`, lookPath)
	if got, want := fmt.Sprint(windows.Args), `[explorer /select,C:\\Users\\me\\file.txt]`; got != want {
		t.Fatalf("windows args = %s, want %s", got, want)
	}
}

func TestOpenExternalRefusesForbidden(t *testing.T) {
	fs, _ := makeFS(t)
	if err := fs.OpenExternal(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestWatchDirReturnsErrorWhenWatcherUnavailable(t *testing.T) {
	fs, home := makeFS(t)
	// Deliberately skip setupWatcher.
	if _, err := fs.WatchDir(home); err == nil {
		t.Fatal("expected error when watcher not set up")
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
