package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestPluginFS(t *testing.T) (*PluginFS, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks tempdir: %v", err)
	}
	return &PluginFS{access: newFSAccess([]string{dir})}, dir
}

func TestPluginFSWriteFileSuccess(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := fs.WriteFile(target, []byte("new"), info.ModTime().UnixMilli(), false)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if meta.Size != 3 {
		t.Fatalf("meta.Size = %d", meta.Size)
	}
}

func TestPluginFSWriteFileForbidden(t *testing.T) {
	fs, _ := newTestPluginFS(t)
	_, err := fs.WriteFile("/etc/hosts", []byte("x"), 0, true)
	if err == nil {
		t.Fatal("expected forbidden error")
	}
}

func TestPluginFSCreateFile(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "new.txt")
	if _, err := fs.CreateFile(target); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestPluginFSCreateFileAlreadyExists(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "a")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := fs.CreateFile(target)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestPluginFSRename(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	from := filepath.Join(dir, "a")
	to := filepath.Join(dir, "b")
	if err := os.WriteFile(from, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Rename(from, to); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(from); err == nil {
		t.Fatal("source still exists")
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
}

func TestPluginFSRemove(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "a")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(target, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("file still exists")
	}
}

func TestPluginFSMkdir(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "new")
	if _, err := fs.Mkdir(target); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}
