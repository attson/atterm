package e2eeclient

import (
	"encoding/hex"
	"testing"

	"github.com/bytemare/ksf"
)

// TestOpaqueKSFGoldenVector pins the OPAQUE password-stretch KSF to a value
// shared byte-for-byte with the web client (web/src/shared/lib/opaque.ts
// opaqueStretch — see web/tests/unit/opaque.test.ts, same golden hex).
//
// bytemare/opaque hardens the password to the P-256 OPRF *element* length
// (33 bytes); the web client (@cloudflare/opaque-ts) must therefore use scrypt
// dkLen=33, NOT the library default of 32. A 1-byte drift yields a different
// randomized_pwd and breaks web<->desktop login with "invalid credentials".
func TestOpaqueKSFGoldenVector(t *testing.T) {
	out := ksf.Scrypt.Get().Harden([]byte("atterm-interop"), nil, 33)
	got := hex.EncodeToString(out)
	const want = "7001bf4adf717b8af42e1d88c36629ec8d1677e0a50d7d3095eda1bd3bde740bf3"
	if got != want {
		t.Fatalf("OPAQUE KSF golden mismatch:\n got %s\nwant %s", got, want)
	}
	// The length the bytemare opaque client actually hardens to (client.go
	// buildPRK) is the OPRF element length — which the web dkLen must equal.
	if n := defaultOpaqueConfig().OPRF.Group().ElementLength(); n != 33 {
		t.Fatalf("P-256 OPRF element length = %d, want 33", n)
	}
}
