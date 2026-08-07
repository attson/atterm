package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeFSAccess(t *testing.T) (*fsAccess, string) {
	t.Helper()
	home := t.TempDir()
	// Default to the remote posture (deny .env) so the existing cases keep
	// their original meaning; TestFSAccessEnvDenyIsConditional covers both.
	return newFSAccess([]string{home}, true), home
}

func TestFSAccessResolveRejectsSymlinkEscapeAndDenylist(t *testing.T) {
	access, home := makeFSAccess(t)

	outside := t.TempDir()
	link := filepath.Join(home, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := access.resolve(link); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("expected forbidden symlink escape, got %v", err)
	}

	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := access.resolve(ssh); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("expected denylist rejection for .ssh, got %v", err)
	}

	envFile := filepath.Join(home, "app", ".env.local")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := access.resolve(envFile); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("expected denylist rejection for .env.local, got %v", err)
	}
}

func TestFSAccessListReadMetaAndReadChunk(t *testing.T) {
	access, home := makeFSAccess(t)

	if err := os.Mkdir(filepath.Join(home, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(home, "note.txt")
	if err := os.WriteFile(textPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(home, "bin.dat")
	if err := os.WriteFile(binPath, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := access.listDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	var sawDir, sawText bool
	for _, entry := range entries {
		if entry.Name == "dir" && entry.IsDir {
			sawDir = true
		}
		if entry.Name == "note.txt" && !entry.IsDir && entry.Size == 11 {
			sawText = true
		}
	}
	if !sawDir || !sawText {
		t.Fatalf("entries missing expected dir/text file: %+v", entries)
	}

	content, err := access.readFile(textPath, 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(content.Data) != "hello" || content.TruncatedAt != 11 || content.IsBinary {
		t.Fatalf("unexpected readFile result: %+v", content)
	}

	meta, err := access.fileMeta(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != 3 || !meta.IsBinary {
		t.Fatalf("unexpected binary meta: %+v", meta)
	}

	chunk, err := access.readChunk(textPath, 6, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk.Data) != "world" || !chunk.EOF || !strings.HasPrefix(chunk.ContentType, "text/plain") {
		t.Fatalf("unexpected final chunk: %+v", chunk)
	}

	chunk, err = access.readChunk(textPath, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk.Data) != "hell" || chunk.EOF {
		t.Fatalf("unexpected partial chunk: %+v", chunk)
	}
}

func TestFSAccessReadChunkCapsLength(t *testing.T) {
	access, home := makeFSAccess(t)
	path := filepath.Join(home, "large.txt")
	data := []byte(strings.Repeat("x", 300*1024))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	chunk, err := access.readChunk(path, 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.Data) != 256*1024 {
		t.Fatalf("expected 256 KiB cap, got %d", len(chunk.Data))
	}
	if chunk.EOF {
		t.Fatal("expected EOF=false for capped chunk before file end")
	}
	if string(chunk.Data[:8]) != "xxxxxxxx" {
		t.Fatalf("unexpected capped chunk prefix %q", string(chunk.Data[:8]))
	}
}

func TestFSAccessReadFileRejectsNegativeMaxBytes(t *testing.T) {
	access, home := makeFSAccess(t)
	path := filepath.Join(home, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("readFile panicked for negative maxBytes: %v", r)
		}
	}()
	if _, err := access.readFile(path, -1); err == nil {
		t.Fatal("expected negative maxBytes to return an error")
	}
}

func TestFSAccessReadFileUsesActualShortReadLength(t *testing.T) {
	access, home := makeFSAccess(t)
	path := filepath.Join(home, "shrinks.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	origStat := osStat
	origOpen := osOpenFile
	t.Cleanup(func() {
		osStat = origStat
		osOpenFile = origOpen
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	osStat = func(name string) (os.FileInfo, error) {
		if name == resolvedPath {
			return fakeFileInfo{FileInfo: info, size: 10}, nil
		}
		return origStat(name)
	}
	osOpenFile = func(name string) (fsReadFile, error) {
		if name == resolvedPath {
			return &shortReadFile{data: []byte("abc")}, nil
		}
		return origOpen(name)
	}

	content, err := access.readFile(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(content.Data) != "abc" {
		t.Fatalf("expected short read data without zero padding, got %q", string(content.Data))
	}
	if content.IsBinary {
		t.Fatal("zero padding should not be included in binary detection")
	}
}

func TestFSAccessReadChunkBoundaryAndInvalidInputs(t *testing.T) {
	access, home := makeFSAccess(t)
	path := filepath.Join(home, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := access.readChunk(path, -1, 1); err == nil {
		t.Fatal("expected negative offset error")
	}
	if _, err := access.readChunk(path, 0, -1); err == nil {
		t.Fatal("expected negative length error")
	}
	if _, err := access.readChunk(dir, 0, 1); err == nil {
		t.Fatal("expected directory input error")
	}

	chunk, err := access.readChunk(path, 99, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.Data) != 0 || !chunk.EOF {
		t.Fatalf("expected empty EOF chunk beyond file end, got %+v", chunk)
	}
}

type fakeFileInfo struct {
	os.FileInfo
	size int64
}

func (f fakeFileInfo) Size() int64 {
	return f.size
}

type shortReadFile struct {
	data []byte
}

func (f *shortReadFile) Read(p []byte) (int, error) {
	n := copy(p, f.data)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *shortReadFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *shortReadFile) Seek(offset int64, whence int) (int64, error) {
	return offset, nil
}

func (f *shortReadFile) Close() error {
	return nil
}

func TestFSAccessWatcherLifecycleAndDebounce(t *testing.T) {
	access, home := makeFSAccess(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan string, 4)
	access.setupWatcher(ctx, func(path string) {
		changed <- path
	})
	defer access.shutdownWatcher()

	handleID, err := access.watchDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if handleID == 0 {
		t.Fatal("expected non-zero watch handle")
	}

	access.scheduleDirChanged(home)
	access.scheduleDirChanged(home)

	select {
	case got := <-changed:
		if got != home {
			t.Fatalf("changed dir = %q, want %q", got, home)
		}
	case <-time.After(3 * debounceWindow):
		t.Fatal("timed out waiting for debounced change")
	}

	select {
	case got := <-changed:
		t.Fatalf("expected one debounced event, got extra %q", got)
	case <-time.After(debounceWindow + 50*time.Millisecond):
	}

	if err := access.unwatchDir(handleID); err != nil {
		t.Fatal(err)
	}
	if err := access.unwatchDir(handleID); err != nil {
		t.Fatalf("unwatch should be idempotent, got %v", err)
	}
}

// The .env deny is conditional on transport safety, not on the file
// being secret: reading it locally is fine, reading it over an
// unencrypted relay is not. .ssh / .gnupg / .aws stay unconditional.
func TestFSAccessEnvDenyIsConditional(t *testing.T) {
	home := t.TempDir()
	envFile := filepath.Join(home, "app", ".env.local")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		denyEnv    bool
		wantDenied bool
	}{
		{"env readable when sealing is in effect", false, false},
		{"env denied when keyless", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newFSAccess([]string{home}, tc.denyEnv)
			_, err := a.resolve(envFile)
			gotDenied := errors.Is(err, ErrPathDenied)
			if gotDenied != tc.wantDenied {
				t.Fatalf("denied=%v want %v (err=%v)", gotDenied, tc.wantDenied, err)
			}
			if _, err := a.resolve(ssh); !errors.Is(err, ErrPathDenied) {
				t.Fatalf(".ssh must always be denied, got %v", err)
			}
		})
	}
}

func TestEnvDenyCoversAllVariants(t *testing.T) {
	home := t.TempDir()
	denying := newFSAccess([]string{home}, true)
	allowing := newFSAccess([]string{home}, false)
	for _, name := range []string{".env", ".env.local", ".env.example", ".env.production"} {
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, []byte("X=1"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := denying.resolve(p); !errors.Is(err, ErrPathDenied) {
			t.Fatalf("%s: expected deny, got %v", name, err)
		}
		if _, err := allowing.resolve(p); err != nil {
			t.Fatalf("%s: expected allow, got %v", name, err)
		}
	}
	// A file merely containing "env" is not an env file.
	other := filepath.Join(home, "environment.txt")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := denying.resolve(other); err != nil {
		t.Fatalf("environment.txt must not be caught by the .env rule, got %v", err)
	}
}
