package relay

import "testing"

// The handler must echo its configured realmID in finalize responses. Assert
// the wiring at the unit level by constructing the handler with a known realm
// and checking it is stored; the full HTTP round-trip is covered by the
// existing opaque auth tests once RealmID flows through.
func TestOpaqueAuthHandlerCarriesRealmID(t *testing.T) {
	h := NewOpaqueAuthHandler(nil, nil, "", "realm-xyz", "")
	if h.realmID != "realm-xyz" {
		t.Fatalf("realmID not stored: %q", h.realmID)
	}
}
