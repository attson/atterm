package relay

import (
	"testing"

	"github.com/bytemare/opaque"
)

// Smoke test: confirm the cipher suite we plan to use is available
// in this library version. Locks the choice so the rest of M1a can
// reference it without surprise.
func TestOPAQUESuiteAvailable(t *testing.T) {
	conf := opaque.DefaultConfiguration()
	if conf.OPRF == 0 || conf.KDF == 0 || conf.MAC == 0 || conf.Hash == 0 || conf.AKE == 0 {
		t.Fatalf("default OPAQUE configuration has unset field: %+v", conf)
	}
	if _, err := conf.Server(); err != nil {
		t.Fatalf("conf.Server: %v", err)
	}
	if _, err := conf.Client(); err != nil {
		t.Fatalf("conf.Client: %v", err)
	}
}
