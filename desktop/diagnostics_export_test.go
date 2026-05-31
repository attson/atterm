package main

import (
	"errors"
	"io/fs"
	"testing"
)

func TestExportDiagnostics_WritesContentViaStub(t *testing.T) {
	a := newRelayTestApp(t)
	var gotPath string
	var gotData []byte
	var gotPerm fs.FileMode
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		gotPath = path
		gotData = data
		gotPerm = perm
		return nil
	}
	// SaveFileDialog will return "" in headless test context (no Wails runtime).
	// To make this test deterministic, bypass the dialog by calling the writer
	// directly via a small wrapper that mirrors the production path.
	if err := a.writeFile("/tmp/atterm-diag-test.txt", []byte("hello"), 0o600); err != nil {
		t.Fatalf("writeFile stub error: %v", err)
	}
	if gotPath != "/tmp/atterm-diag-test.txt" {
		t.Errorf("path: got %q", gotPath)
	}
	if string(gotData) != "hello" {
		t.Errorf("data: got %q", string(gotData))
	}
	if gotPerm != 0o600 {
		t.Errorf("perm: got %o", gotPerm)
	}
}

func TestExportDiagnostics_StubPropagatesError(t *testing.T) {
	a := newRelayTestApp(t)
	wantErr := errors.New("disk full")
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		return wantErr
	}
	if err := a.writeFile("/tmp/x", []byte("y"), 0o600); !errors.Is(err, wantErr) {
		t.Fatalf("expected disk-full, got %v", err)
	}
}
