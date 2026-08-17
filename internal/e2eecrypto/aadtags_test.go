package e2eecrypto

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestAADTagsAreUnique(t *testing.T) {
	seen := map[string]byte{}
	for b, name := range AADTags {
		if prev, dup := seen[name]; dup {
			t.Errorf("name %q used for both 0x%02x and 0x%02x", name, prev, b)
		}
		seen[name] = b
	}
	// map keys are bytes, so duplicate bytes are impossible by construction —
	// that is the point of keying by byte rather than listing pairs.
}

func TestAADTagsMatchProtocolDoc(t *testing.T) {
	// The registry is the code's view; docs/spec/protocol.md's sealed-envelope
	// table is what a reader (and redline #22) treats as authoritative. If they
	// diverge, a new sealed namespace can silently reuse a discriminator byte,
	// which is what lets one envelope type be replayed in another's place.
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", "protocol.md"))
	if err != nil {
		t.Fatalf("read protocol.md: %v", err)
	}
	documented := map[byte]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\|\\s*`0x([0-9a-fA-F]{2})`").FindAllSubmatch(raw, -1) {
		v, err := strconv.ParseUint(string(m[1]), 16, 8)
		if err != nil {
			t.Fatalf("bad byte %q in protocol.md: %v", m[1], err)
		}
		documented[byte(v)] = true
	}
	if len(documented) == 0 {
		t.Fatal("parsed zero bytes out of protocol.md — the table format changed, fix this regex rather than deleting the test")
	}
	for b, name := range AADTags {
		if !documented[b] {
			t.Errorf("AAD tag 0x%02x (%s) is used in code but absent from protocol.md's sealed envelope table — add a row there (redline #22)", b, name)
		}
	}
}
