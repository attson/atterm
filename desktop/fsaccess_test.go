package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeFSAccess(t *testing.T) (*fsAccess, string) {
	t.Helper()
	home := t.TempDir()
	return newFSAccess([]string{home}), home
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
