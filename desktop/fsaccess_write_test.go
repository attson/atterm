package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAccess(t *testing.T) (*fsAccess, string) {
	t.Helper()
	// On macOS t.TempDir() lives under /var/… which EvalSymlinks resolves to
	// /private/var/…; the allow-root check on non-existent targets would
	// otherwise fail because the target and root disagree on symlink hops.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks tempdir: %v", err)
	}
	return newFSAccess([]string{dir}, true), dir
}

func writeSeed(t *testing.T, path, body string) int64 {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("seed stat: %v", err)
	}
	return info.ModTime().UnixMilli()
}

func TestWriteFileHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	mt := writeSeed(t, target, "old")
	meta, err := a.writeFile(target, []byte("new content"), mt, false)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	if meta.Size != int64(len("new content")) {
		t.Fatalf("meta.Size = %d, want %d", meta.Size, len("new content"))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("body = %q, want %q", got, "new content")
	}
}

func TestWriteFileStaleModTime(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "old")
	_, err := a.writeFile(target, []byte("nope"), 1, false)
	if !errors.Is(err, ErrStaleModTime) {
		t.Fatalf("expected ErrStaleModTime, got %v", err)
	}
	if !strings.Contains(err.Error(), "current=") {
		t.Fatalf("expected current= in error, got %q", err.Error())
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("original mutated: %q", got)
	}
}

func TestWriteFileForbiddenPath(t *testing.T) {
	a, _ := newTestAccess(t)
	_, err := a.writeFile("/etc/hosts", []byte("x"), 0, true)
	if !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("expected ErrPathForbidden, got %v", err)
	}
}

func TestWriteFileDenied(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, ".env")
	writeSeed(t, target, "x")
	_, err := a.writeFile(target, []byte("y"), 0, false)
	if !errors.Is(err, ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}
}

func TestWriteFileHardCap(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "x")
	_, err := a.writeFile(target, make([]byte, maxWriteBytesHard+1), 0, false)
	if err == nil {
		t.Fatal("expected error for oversized write")
	}
	if !strings.Contains(err.Error(), "write_denied") {
		t.Fatalf("expected write_denied prefix, got %q", err.Error())
	}
}

func TestWriteFileCreateIfMissing(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "new.txt")
	meta, err := a.writeFile(target, []byte("hi"), 0, true)
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	if meta.Size != 2 {
		t.Fatalf("meta.Size = %d", meta.Size)
	}
}

func TestWriteFileMissingWithoutCreateReturnsNotFound(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "missing.txt")
	_, err := a.writeFile(target, []byte("x"), 0, false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWriteFileRefusesDirectory(t *testing.T) {
	a, dir := newTestAccess(t)
	_, err := a.writeFile(dir, []byte("x"), 0, false)
	if !errors.Is(err, ErrIsDirectory) {
		t.Fatalf("expected ErrIsDirectory, got %v", err)
	}
}

func TestWriteFileAtomicOnRenameFailure(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "old")
	orig := osRename
	osRename = func(a, b string) error { return errors.New("boom") }
	t.Cleanup(func() { osRename = orig })
	_, err := a.writeFile(target, []byte("new"), 0, false)
	if err == nil {
		t.Fatal("expected error from rename failure")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("original mutated on rename failure: %q", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".atterm-tmp-") {
			t.Fatalf("leaked temp file: %s", e.Name())
		}
	}
}

func TestCreateFileAlreadyExists(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "x")
	_, err := a.createFile(target)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestCreateFileSucceeds(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "new.txt")
	meta, err := a.createFile(target)
	if err != nil {
		t.Fatalf("createFile: %v", err)
	}
	if meta.Size != 0 {
		t.Fatalf("meta.Size = %d", meta.Size)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestRenameHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	from := filepath.Join(dir, "a.txt")
	to := filepath.Join(dir, "b.txt")
	writeSeed(t, from, "x")
	meta, err := a.renamePath(from, to)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(from); err == nil {
		t.Fatal("source still exists")
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	if meta.Path != to {
		t.Fatalf("meta.Path = %q, want %q", meta.Path, to)
	}
}

func TestRenameForbiddenTarget(t *testing.T) {
	a, dir := newTestAccess(t)
	from := filepath.Join(dir, "a.txt")
	writeSeed(t, from, "x")
	_, err := a.renamePath(from, "/etc/hosts.moved")
	if !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("expected ErrPathForbidden, got %v", err)
	}
}

func TestRenameSourceMissing(t *testing.T) {
	a, dir := newTestAccess(t)
	_, err := a.renamePath(filepath.Join(dir, "no"), filepath.Join(dir, "yes"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameTargetExists(t *testing.T) {
	a, dir := newTestAccess(t)
	from := filepath.Join(dir, "a")
	to := filepath.Join(dir, "b")
	writeSeed(t, from, "x")
	writeSeed(t, to, "y")
	_, err := a.renamePath(from, to)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestRemoveRefusesNonEmptyDirWithoutRecursive(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSeed(t, filepath.Join(sub, "x"), "y")
	if err := a.removePath(sub, false); err == nil {
		t.Fatal("expected error removing non-empty dir")
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("dir was removed anyway: %v", err)
	}
}

func TestRemoveRecursive(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSeed(t, filepath.Join(sub, "x"), "y")
	if err := a.removePath(sub, true); err != nil {
		t.Fatalf("removePath: %v", err)
	}
	if _, err := os.Stat(sub); err == nil {
		t.Fatal("dir still exists after recursive remove")
	}
}

func TestRemoveMissing(t *testing.T) {
	a, dir := newTestAccess(t)
	if err := a.removePath(filepath.Join(dir, "nope"), false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMkdirHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "nested")
	meta, err := a.mkdir(target)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	if meta.Path != target {
		t.Fatalf("meta.Path = %q, want %q", meta.Path, target)
	}
}

func TestMkdirAlreadyExists(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := a.mkdir(sub)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}
