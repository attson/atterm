package relay

import (
	"bytes"
	"os"
	"testing"
)

// TestPermissions_NoReadOnlyTokenBranch verifies that permissions.go contains
// no references to the read-only-token logic that was removed in Task 4.2.
// If any of the forbidden identifiers appear in the file, the test fails.
func TestPermissions_NoReadOnlyTokenBranch(t *testing.T) {
	src, err := os.ReadFile("permissions.go")
	if err != nil {
		t.Fatalf("read permissions.go: %v", err)
	}
	forbidden := []string{
		"readOnlyToken",
		"ReadOnlyToken",
		"read_only_token",
		"readonly_token",
	}
	for _, p := range forbidden {
		if bytes.Contains(src, []byte(p)) {
			t.Errorf("permissions.go still references %q; remove the read-only-token branch", p)
		}
	}
}
