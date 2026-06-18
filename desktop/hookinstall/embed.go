package hookinstall

import (
	"crypto/sha256"
	"encoding/hex"
	_ "embed"
)

//go:embed atterm-hook
var embeddedHook []byte

// embeddedHash is the hex-encoded first 4 bytes (8 hex chars) of the
// SHA-256 of the embedded binary. Used to name the on-disk versioned
// file: ~/.atterm/bin/atterm-hook-<embeddedHash>.
var embeddedHash = func() string {
	sum := sha256.Sum256(embeddedHook)
	return hex.EncodeToString(sum[:4])
}()
