package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
)

func mustWriteFile(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReceivedFilesListEnumerates(t *testing.T) {
	base, err := appdir.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "paste-files")
	_ = os.RemoveAll(root)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	sid1 := uuid.New()
	sid2 := uuid.New()
	mustWriteFile(t, filepath.Join(root, sid1.String()), "a.txt", 100)
	mustWriteFile(t, filepath.Join(root, sid1.String()), "b.pdf", 200)
	mustWriteFile(t, filepath.Join(root, sid2.String()), "c.log", 50)

	a := &App{}
	summary, err := a.ReceivedFilesList()
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalBytes != 350 {
		t.Errorf("total = %d, want 350", summary.TotalBytes)
	}
	if len(summary.Sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(summary.Sessions))
	}
}

func TestReceivedFilesListMissingRoot(t *testing.T) {
	// No root: should return empty summary, not error.
	base, _ := appdir.CacheDir()
	root := filepath.Join(base, "paste-files")
	_ = os.RemoveAll(root)

	a := &App{}
	summary, err := a.ReceivedFilesList()
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalBytes != 0 || len(summary.Sessions) != 0 {
		t.Errorf("expected empty summary, got %+v", summary)
	}
}

func TestReceivedFilesClearSession(t *testing.T) {
	base, _ := appdir.CacheDir()
	root := filepath.Join(base, "paste-files")
	_ = os.RemoveAll(root)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	sid := uuid.New()
	mustWriteFile(t, filepath.Join(root, sid.String()), "a.txt", 10)

	a := &App{}
	if err := a.ReceivedFilesClearSession(sid.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, sid.String())); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, err = %v", err)
	}
}

func TestReceivedFilesDeleteHappyPath(t *testing.T) {
	base, _ := appdir.CacheDir()
	root := filepath.Join(base, "paste-files")
	_ = os.RemoveAll(root)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	sid := uuid.New()
	mustWriteFile(t, filepath.Join(root, sid.String()), "a.txt", 10)

	a := &App{}
	if err := a.ReceivedFilesDelete(sid.String(), "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, sid.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, err = %v", err)
	}
}

func TestReceivedFilesDeleteRejectsBadInput(t *testing.T) {
	a := &App{}
	bad := []struct{ sid, name string }{
		{uuid.New().String(), "../evil"},
		{uuid.New().String(), "sub/dir.txt"},
		{uuid.New().String(), `sub\dir.txt`},
		{uuid.New().String(), ""},
		{uuid.New().String(), "."},
		{uuid.New().String(), ".."},
		{"not-a-uuid", "foo.txt"},
	}
	for _, c := range bad {
		if err := a.ReceivedFilesDelete(c.sid, c.name); err == nil {
			t.Errorf("expected error for sid=%q name=%q", c.sid, c.name)
		}
	}
}
