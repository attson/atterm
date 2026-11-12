package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSSHKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "work.pem")
	const pem = "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(path, []byte(pem), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).ReadSSHKeyFile(path)
	if err != nil {
		t.Fatalf("ReadSSHKeyFile: %v", err)
	}
	if got.Name != "work.pem" || got.Content != pem {
		t.Fatalf("ReadSSHKeyFile() = %#v, want name work.pem and original content", got)
	}
}

func TestReadSSHKeyFileRejectsUnsafeInputs(t *testing.T) {
	dir := t.TempDir()
	tooLarge := filepath.Join(dir, "huge.pem")
	if err := os.WriteFile(tooLarge, []byte(strings.Repeat("x", maxSSHKeyFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "relative", path: "id_ed25519"},
		{name: "directory", path: dir},
		{name: "missing", path: filepath.Join(dir, "missing.pem")},
		{name: "too large", path: tooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&App{}).ReadSSHKeyFile(tt.path); err == nil {
				t.Fatalf("ReadSSHKeyFile(%q) succeeded, want rejection", tt.path)
			}
		})
	}
}

func TestReadSSHKeyFileRejectsNonKeyWithoutLeakingPath(t *testing.T) {
	dir := t.TempDir()
	nonKey := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(nonKey, []byte("not a secret key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&App{}).ReadSSHKeyFile(nonKey); err == nil {
		t.Fatal("ReadSSHKeyFile accepted a non-key file")
	}

	missing := filepath.Join(dir, "missing.pem")
	_, err := (&App{}).ReadSSHKeyFile(missing)
	if err == nil {
		t.Fatal("ReadSSHKeyFile accepted a missing file")
	}
	if strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), dir) {
		t.Fatalf("ReadSSHKeyFile error leaked the local path: %q", err)
	}
}
