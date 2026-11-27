package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalTransferEndpointSkipsSymlinkBeforeFollowingIt(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("do not follow"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	ep := newLocalTransferEndpoint(newFSAccess([]string{root}, false))
	entry, err := ep.Lstat(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != transferSkipped || entry.SkipReason != "symlink" {
		t.Fatalf("entry = %+v, want skipped symlink", entry)
	}
	if _, err := ep.OpenReader(context.Background(), link); err == nil {
		t.Fatal("OpenReader followed a skipped symlink outside allowRoots")
	}
}

func TestLocalTransferEndpointRejectsOutsideAllowRoots(t *testing.T) {
	root := t.TempDir()
	ep := newLocalTransferEndpoint(newFSAccess([]string{root}, false))
	if _, err := ep.Clean(filepath.Join(t.TempDir(), "outside")); !errors.Is(err, ErrPathForbidden) {
		t.Fatalf("Clean error = %v, want ErrPathForbidden", err)
	}
}

func TestLocalTransferEndpointStagedWriterDoesNotReplaceWithoutConsent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ep := newLocalTransferEndpoint(newFSAccess([]string{root}, false))

	w, err := ep.CreateStagedWriter(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "new"); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(context.Background(), false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Commit error = %v, want ErrAlreadyExists", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("target = %q, want old", data)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".atterm-transfer-*.partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial files left behind: %v", matches)
	}
}
