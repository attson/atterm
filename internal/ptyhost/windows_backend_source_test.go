package ptyhost

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsBackendUsesConPTYInsteadOfUnsupportedCreackPTY(t *testing.T) {
	common, err := os.ReadFile("ptyhost.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(common), "//go:build !windows") {
		t.Fatal("ptyhost.go must be excluded on Windows; creack/pty returns ErrUnsupported there")
	}

	body, err := os.ReadFile("ptyhost_windows.go")
	if err != nil {
		t.Fatalf("missing Windows ptyhost backend: %v", err)
	}
	source := string(body)
	for _, want := range []string{"CreatePseudoConsole", "PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE", "ResizePseudoConsole"} {
		if !strings.Contains(source, want) {
			t.Fatalf("Windows ptyhost backend missing %s", want)
		}
	}
	if strings.Contains(source, "github.com/creack/pty") {
		t.Fatal("Windows ptyhost backend must not call creack/pty; it is unsupported on Windows")
	}
}
