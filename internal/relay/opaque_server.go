// Package relay's OPAQUE server singleton. The relay needs ONE long-lived
// OPRF seed and ONE long-term AKE keypair: rotating either would invalidate
// every persisted user envelope (the OPAQUE record encrypts under a key
// derived from the OPRF, and clients pin the server's AKE public key as
// part of the AKE transcript). First boot generates them with the library's
// own helpers (which return suite-correct byte lengths so we don't have to
// guess at scalar sizes); subsequent boots reload from opaque_server_state.
//
// We keep the cipher suite at opaque.DefaultConfiguration() — locked in by
// the T1 smoke test — and tag persisted rows with "default-v1" so a future
// suite migration can detect mismatches before handing the bytes to
// SetKeyMaterial.
package relay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/opaquesuite"
	"github.com/attson/atterm/internal/userstore"
)

// opaqueSuiteTag identifies the cipher suite the persisted seed + AKE
// keys were generated under. Stored verbatim so a later suite change can
// surface a clear error instead of letting SetKeyMaterial reject the
// material with an opaque (heh) message.
//
// v2: P256Sha256 + Scrypt — chosen so the @cloudflare/opaque-ts client
// library (which only ships P-256 / P-384 / P-521 suites and Scrypt or
// Identity as memory-hard function) can interop with this server. v1
// was opaque.DefaultConfiguration (ristretto255-SHA512 + Argon2id),
// which was the secure default but unreachable from browsers without
// shipping a custom WASM build.
const opaqueSuiteTag = "p256-scrypt-v1"

// opaqueServerIdentity is the relay's static server identity, mixed into
// every AKE transcript via SetKeyMaterial. Clients pin the same string via
// internal/opaquesuite.ServerIdentity, so any rename here must ship in
// lockstep with the SDK and browser WASM client.
var opaqueServerIdentity = []byte(opaquesuite.ServerIdentity)

// OpaqueServer wraps the bytemare/opaque configuration with the relay's
// persisted OPRF seed and AKE keypair. It is a passive bundle of material:
// every handler call constructs a fresh *opaque.Server via newServer() and
// primes it with SetKeyMaterial, so concurrent registrations / logins do
// not share internal protocol state.
type OpaqueServer struct {
	conf      *opaque.Configuration
	serverID  []byte
	oprfSeed  []byte
	akeSecret []byte
	akePublic []byte
}

// LoadOrInitOpaqueServer returns the relay's OPAQUE server singleton.
//
// On first boot (no opaque_server_state row), it generates a fresh OPRF
// seed and AKE keypair via the library's own helpers (GenerateOPRFSeed,
// Configuration.KeyGen) and persists them. On every subsequent boot it
// reloads the persisted material verbatim. The returned *OpaqueServer is
// safe to share across goroutines because it holds only immutable bytes;
// per-request *opaque.Server instances come from newServer().
func LoadOrInitOpaqueServer(ctx context.Context, store *userstore.DBStore) (*OpaqueServer, error) {
	conf := opaquesuite.Config()

	state, err := store.GetOpaqueServerState(ctx)
	switch {
	case err == nil:
		// Sanity check: if a future build changes the suite, refuse to
		// load mismatched material rather than silently letting
		// SetKeyMaterial fail later in the request path.
		if state.Suite != opaqueSuiteTag {
			return nil, fmt.Errorf("opaque server state suite %q does not match expected %q", state.Suite, opaqueSuiteTag)
		}
		return &OpaqueServer{
			conf:      conf,
			serverID:  opaqueServerIdentity,
			oprfSeed:  state.OPRFSeed,
			akeSecret: state.AKEServerSecret,
			akePublic: state.AKEServerPublic,
		}, nil
	case errors.Is(err, userstore.ErrOpaqueStateMissing):
		// fall through to first-boot generation
	default:
		return nil, fmt.Errorf("load opaque state: %w", err)
	}

	// First boot: let the library decide the seed length and key
	// encoding. We never roll these by hand — the suite parameters
	// (group, scalar length) are an internal library detail.
	seed := conf.GenerateOPRFSeed()
	sk, pk := conf.KeyGen()

	if err := store.StoreOpaqueServerState(ctx, userstore.OpaqueServerState{
		OPRFSeed:        seed,
		AKEServerSecret: sk,
		AKEServerPublic: pk,
		Suite:           opaqueSuiteTag,
		CreatedAt:       time.Now().UTC(),
	}); err != nil {
		return nil, fmt.Errorf("persist opaque state: %w", err)
	}

	return &OpaqueServer{
		conf:      conf,
		serverID:  opaqueServerIdentity,
		oprfSeed:  seed,
		akeSecret: sk,
		akePublic: pk,
	}, nil
}

// AkePublicKey returns a copy of the relay's long-term AKE public key.
// Callers may publish this to clients (e.g. embed it in a /api/auth/config
// response so registration can pin it). The defensive copy keeps callers
// from mutating the singleton's backing buffer.
func (o *OpaqueServer) AkePublicKey() []byte {
	return append([]byte(nil), o.akePublic...)
}

// newServer constructs a fresh per-request *opaque.Server primed with this
// singleton's seed + key material. Each request gets its own instance so
// the library's internal multi-step state (e.g. login_init → login_finish
// MAC bookkeeping) is not shared across goroutines.
func (o *OpaqueServer) newServer() (*opaque.Server, error) {
	sv, err := o.conf.Server()
	if err != nil {
		return nil, fmt.Errorf("opaque.Configuration.Server: %w", err)
	}
	if err := sv.SetKeyMaterial(o.serverID, o.akeSecret, o.akePublic, o.oprfSeed); err != nil {
		return nil, fmt.Errorf("opaque.Server.SetKeyMaterial: %w", err)
	}
	return sv, nil
}
