package relay

import (
	"bytes"
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// TestLoadOrInitServer_GeneratesOnFirstBoot exercises the singleton
// bootstrap: first call populates opaque_server_state with a freshly
// generated OPRF seed + AKE keypair, second call reuses the persisted
// row so the AKE public key (and therefore the server identity clients
// pin) stays stable across relay restarts.
func TestLoadOrInitServer_GeneratesOnFirstBoot(t *testing.T) {
	store := userstore.NewInMemory(t)
	ctx := context.Background()

	srv1, err := LoadOrInitOpaqueServer(ctx, store)
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if srv1 == nil {
		t.Fatalf("nil server")
	}

	srv2, err := LoadOrInitOpaqueServer(ctx, store)
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}

	pk1 := srv1.AkePublicKey()
	pk2 := srv2.AkePublicKey()
	if !bytes.Equal(pk1, pk2) {
		t.Fatalf("AKE public key changed across boots: %x vs %x", pk1, pk2)
	}
	if len(pk1) == 0 {
		t.Fatalf("empty AKE public key")
	}

	// The persisted row should reflect what the loader returned —
	// otherwise a second boot would be silently re-generating.
	state, err := store.GetOpaqueServerState(ctx)
	if err != nil {
		t.Fatalf("GetOpaqueServerState after init: %v", err)
	}
	if !bytes.Equal(state.AKEServerPublic, pk1) {
		t.Fatalf("persisted AKE public key differs from loader output")
	}
	if len(state.OPRFSeed) == 0 {
		t.Fatalf("persisted OPRF seed is empty")
	}
	if len(state.AKEServerSecret) == 0 {
		t.Fatalf("persisted AKE secret key is empty")
	}

	// newServer must produce a server primed with the persisted material
	// (i.e. SetKeyMaterial succeeded). If the seed/keys mismatched the
	// configured suite, the library would reject them here.
	sv, err := srv2.newServer()
	if err != nil {
		t.Fatalf("newServer after reload: %v", err)
	}
	if sv == nil {
		t.Fatalf("newServer returned nil")
	}
}
