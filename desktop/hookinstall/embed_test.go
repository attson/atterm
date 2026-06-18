package hookinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedBinaryRunnable writes the embedded bytes to disk and runs
// the result with --version, asserting it exits 0 and prints a line
// starting with "atterm-hook ". Catches:
//   - embed file missing or zero bytes
//   - embed file is not the atterm-hook binary
//   - atterm-hook binary lacks the --version handler
func TestEmbeddedBinaryRunnable(t *testing.T) {
	if len(embeddedHook) == 0 {
		t.Fatal("embeddedHook is empty — did you forget to run `make atterm-hook-embed`?")
	}

	tmp := t.TempDir()
	bin := filepath.Join(tmp, "atterm-hook")
	if err := os.WriteFile(bin, embeddedHook, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "atterm-hook ") {
		t.Errorf("unexpected output %q", got)
	}
}

func TestEmbeddedHashLength(t *testing.T) {
	if len(embeddedHash) != 8 {
		t.Errorf("hash length = %d; want 8", len(embeddedHash))
	}
}
