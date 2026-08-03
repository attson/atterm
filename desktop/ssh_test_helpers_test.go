package main

import (
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

// useIsolatedKeyring points safekeyring at a fresh per-test temp dir and
// resets the global override on cleanup. safekeyring.fileDir is process-global,
// so without the reset one SSH test's temp dir leaks into the next test —
// which passed locally but failed on CI where test ordering differed. Mirrors
// the pattern already used by the feishu remote-terminal tests.
func useIsolatedKeyring(t *testing.T) {
	t.Helper()
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	t.Cleanup(func() { safekeyring.SetFileDirForTest("") })
}
