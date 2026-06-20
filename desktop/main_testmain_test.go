package main

import (
	"os"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

// TestMain forces the file-backed secret store (in a throwaway temp dir) for
// the whole desktop test package. Without this, account-key / login tests that
// don't mock go-keyring hit the real OS keychain — which on macOS pops a
// blocking "keychain access" prompt and on CI may behave inconsistently.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atterm-test-secrets")
	if err == nil {
		safekeyring.SetFileDirForTest(dir)
		safekeyring.UseFileStore()
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
