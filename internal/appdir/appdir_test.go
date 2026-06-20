package appdir

import (
	"path/filepath"
	"testing"
)

func reset() {
	mu.Lock()
	name = prodName
	devRoot = ""
	mu.Unlock()
}

func TestDefaultIsProd(t *testing.T) {
	reset()
	if got := Name(); got != "atterm" {
		t.Fatalf("Name() = %q, want atterm", got)
	}
	if IsDev() {
		t.Fatal("IsDev() = true in default state")
	}
	if KeychainSuffix() != "" {
		t.Fatalf("KeychainSuffix() = %q, want empty", KeychainSuffix())
	}
	if DevRoot() != "" {
		t.Fatalf("DevRoot() = %q, want empty in prod", DevRoot())
	}
	if _, ok := DevLogFile(); ok {
		t.Fatal("DevLogFile() ok=true in prod")
	}
}

func TestUseDevUsesProjectLocalRoot(t *testing.T) {
	reset()
	t.Cleanup(reset)
	root := filepath.Join(t.TempDir(), "devroot")
	t.Setenv("ATTERM_DEV_DIR", root)
	UseDev()

	if !IsDev() || KeychainSuffix() != ".dev" {
		t.Fatalf("IsDev=%v suffix=%q", IsDev(), KeychainSuffix())
	}
	cfg, err := ConfigDir()
	if err != nil || cfg != root {
		t.Fatalf("ConfigDir = %q, %v; want %q", cfg, err, root)
	}
	cache, err := CacheDir()
	if err != nil || cache != filepath.Join(root, "cache") {
		t.Fatalf("CacheDir = %q, %v; want %s/cache", cache, err, root)
	}
	logp, ok := DevLogFile()
	if !ok || logp != filepath.Join(root, "logs", "desktop.log") {
		t.Fatalf("DevLogFile = %q, %v; want %s/logs/desktop.log", logp, ok, root)
	}
}
