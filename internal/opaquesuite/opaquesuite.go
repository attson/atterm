// Package opaquesuite is the single source of truth for atterm's OPAQUE cipher
// suite and server identity, shared by the desktop SDK (internal/e2eeclient),
// the browser WASM client (cmd/opaque-wasm), and the relay server. All three
// bytemare/opaque endpoints MUST use this exact suite — a single differing
// field produces non-interoperable protocol bytes ("invalid credentials").
//
// Suite: P-256-SHA256 OPRF + AKE, SHA-256 KDF/MAC/Hash, Scrypt KSF. This is the
// CFRG-recommended TLS-1.3-compatible OPAQUE configuration. It deliberately
// avoids any net/http or relay imports so it also compiles under
// GOOS=js GOARCH=wasm for the browser client.
package opaquesuite

import (
	"crypto"
	_ "crypto/sha256" // register SHA-256 so crypto.SHA256.Available() is true

	"github.com/bytemare/ksf"
	"github.com/bytemare/opaque"
)

// ServerIdentity is bound into the OPAQUE AKE transcript on every client and
// the server. Changing it breaks every existing account.
const ServerIdentity = "atterm-relay"

// Config returns the OPAQUE configuration. Callers must not mutate the result.
func Config() *opaque.Configuration {
	return &opaque.Configuration{
		OPRF:    opaque.P256Sha256,
		KDF:     crypto.SHA256,
		MAC:     crypto.SHA256,
		Hash:    crypto.SHA256,
		KSF:     ksf.Scrypt,
		AKE:     opaque.P256Sha256,
		Context: nil,
	}
}
