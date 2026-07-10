package relay

import (
	"bytes"
	"os"
	"testing"

	"github.com/attson/atterm/internal/proto"
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

// TestPasteFilePermissionMatrix locks in the frameAllowedByPermission
// behavior for PASTE_FILE: only authWrite + permFull (driver-implied via
// the caller) may pass; every read-scope or lesser-perm combination is
// dropped, matching PASTE_IMAGE's gating.
func TestPasteFilePermissionMatrix(t *testing.T) {
	cases := []struct {
		scope authScope
		perm  remotePermission
		want  bool
	}{
		{authRead, permView, false},
		{authRead, permControl, false},
		{authRead, permFull, false},
		{authWrite, permView, false},
		{authWrite, permControl, false},
		{authWrite, permFull, true},
	}
	for _, tc := range cases {
		got := frameAllowedByPermission(tc.scope, tc.perm, proto.TypePasteFile)
		if got != tc.want {
			t.Errorf("scope=%v perm=%v: got %v want %v", tc.scope, tc.perm, got, tc.want)
		}
	}
}
