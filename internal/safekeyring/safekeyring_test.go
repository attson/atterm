package safekeyring_test

import (
	"errors"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
	"github.com/zalando/go-keyring"
)

// Healthy keychain: Get/Set/Delete delegate to the keyring, missing keys
// surface as ErrNotFound.
func TestKeychainHappyPath(t *testing.T) {
	safekeyring.Reset()
	keyring.MockInit()

	if err := safekeyring.Set("svc", "acct", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := safekeyring.Get("svc", "acct")
	if err != nil || got != "v1" {
		t.Fatalf("Get = %q, %v; want v1", got, err)
	}
	if _, err := safekeyring.Get("svc", "missing"); !errors.Is(err, safekeyring.ErrNotFound) {
		t.Fatalf("missing Get err = %v; want ErrNotFound", err)
	}
}

// A hard keychain failure (the unsigned-build "signal: killed" case) latches
// degraded and routes secrets to the 0600 file store, which persists across
// calls and is removable.
func TestFileFallbackOnKeychainError(t *testing.T) {
	safekeyring.Reset()
	safekeyring.SetFileDirForTest(t.TempDir())
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	keyring.MockInitWithError(errors.New("exec: signal: killed"))

	if err := safekeyring.Set("svc", "acct", "secret-value"); err != nil {
		t.Fatalf("Set fell through to file but errored: %v", err)
	}
	got, err := safekeyring.Get("svc", "acct")
	if err != nil || got != "secret-value" {
		t.Fatalf("Get = %q, %v; want secret-value from file fallback", got, err)
	}
	if err := safekeyring.Delete("svc", "acct"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := safekeyring.Get("svc", "acct"); !errors.Is(err, safekeyring.ErrNotFound) {
		t.Fatalf("after delete Get err = %v; want ErrNotFound", err)
	}
}

// Once degraded, a different key still round-trips through the file store
// without ever invoking the (broken) keychain again.
func TestFileFallbackPersistsAcrossKeys(t *testing.T) {
	safekeyring.Reset()
	safekeyring.SetFileDirForTest(t.TempDir())
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	keyring.MockInitWithError(errors.New("exec: signal: killed"))

	_ = safekeyring.Set("a", "1", "alpha")
	_ = safekeyring.Set("b", "2", "beta")
	if v, _ := safekeyring.Get("a", "1"); v != "alpha" {
		t.Fatalf("a/1 = %q; want alpha", v)
	}
	if v, _ := safekeyring.Get("b", "2"); v != "beta" {
		t.Fatalf("b/2 = %q; want beta", v)
	}
}
