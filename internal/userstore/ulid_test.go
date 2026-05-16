package userstore

import (
	"strings"
	"testing"
	"time"
)

func TestNewID_MonotonicallyIncreasingWhenSequential(t *testing.T) {
	g := newIDGen(time.Now)
	prev := g.New()
	for i := 0; i < 100; i++ {
		got := g.New()
		if got <= prev {
			t.Fatalf("ULIDs not monotonic: %s !> %s", got, prev)
		}
		prev = got
	}
}

func TestNewID_ULIDFormat(t *testing.T) {
	g := newIDGen(time.Now)
	id := g.New()
	if len(id) != 26 {
		t.Fatalf("ULID length: got %d, want 26 (%q)", len(id), id)
	}
	// ULID alphabet is Crockford base32: no I, L, O, U.
	for _, c := range strings.ToUpper(id) {
		if strings.ContainsRune("ILOU", c) {
			t.Fatalf("ULID contains forbidden char %q in %q", c, id)
		}
	}
}
