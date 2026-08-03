package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndRemoveKnownHosts(t *testing.T) {
	dir := t.TempDir()
	kh := filepath.Join(dir, "known_hosts")
	content := "host-a ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAA\n" +
		"host-b ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBB\n"
	if err := os.WriteFile(kh, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{sshKnownHostsPath: kh}

	entries, err := a.ListKnownHosts()
	if err != nil {
		t.Fatalf("ListKnownHosts: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if err := a.RemoveKnownHost("host-a"); err != nil {
		t.Fatalf("RemoveKnownHost: %v", err)
	}
	entries, _ = a.ListKnownHosts()
	if len(entries) != 1 || entries[0].Host != "host-b" {
		t.Fatalf("host-a not removed: %+v", entries)
	}
}

func TestKnownHostsMissingFileIsEmpty(t *testing.T) {
	a := &App{sshKnownHostsPath: filepath.Join(t.TempDir(), "nope")}
	entries, err := a.ListKnownHosts()
	if err != nil {
		t.Fatalf("ListKnownHosts on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Fatal("expected empty")
	}
	if err := a.RemoveKnownHost("x"); err != nil {
		t.Fatalf("RemoveKnownHost idempotent: %v", err)
	}
}
