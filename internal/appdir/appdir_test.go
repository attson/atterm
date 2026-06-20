package appdir

import (
	"path/filepath"
	"testing"
)

func TestDefaultIsProd(t *testing.T) {
	name = prodName // ensure clean state regardless of test order
	if got := Name(); got != "atterm" {
		t.Fatalf("Name() = %q, want atterm", got)
	}
	if IsDev() {
		t.Fatal("IsDev() = true in default state")
	}
	if got := KeychainSuffix(); got != "" {
		t.Fatalf("KeychainSuffix() = %q, want empty", got)
	}
}

func TestUseDevNamespaces(t *testing.T) {
	t.Cleanup(func() { name = prodName })
	UseDev()
	if got := Name(); got != "atterm-dev" {
		t.Fatalf("Name() = %q, want atterm-dev", got)
	}
	if !IsDev() {
		t.Fatal("IsDev() = false after UseDev")
	}
	if got := KeychainSuffix(); got != ".dev" {
		t.Fatalf("KeychainSuffix() = %q, want .dev", got)
	}
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if filepath.Base(dir) != "atterm-dev" {
		t.Fatalf("ConfigDir leaf = %q, want atterm-dev", filepath.Base(dir))
	}
}
