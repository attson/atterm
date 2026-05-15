package userstore

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idGen wraps oklog/ulid's monotonic source. The time function is
// injectable so tests can pin "now".
type idGen struct {
	mu   sync.Mutex
	mono *ulid.MonotonicEntropy
	now  func() time.Time
}

func newIDGen(now func() time.Time) *idGen {
	return &idGen{
		mono: ulid.Monotonic(rand.Reader, 0),
		now:  now,
	}
}

// New returns a 26-char ULID string. Safe for concurrent use.
func (g *idGen) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(g.now()), g.mono).String()
}

// defaultIDs is used by all store CRUD; tests construct their own gen.
var defaultIDs = newIDGen(time.Now)
