package userstore

import (
	"fmt"
	"strings"
	"testing"
)

func TestSecret_RedactsAllVerbs(t *testing.T) {
	s := NewSecret("atk_supersecretvalueXYZ", "atk_")
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		out := fmt.Sprintf(verb, s)
		if strings.Contains(out, "supersecret") {
			t.Fatalf("verb %s leaked plaintext: %q", verb, out)
		}
		if !strings.Contains(out, "atk_") {
			t.Fatalf("verb %s lost prefix: %q", verb, out)
		}
	}
}

func TestSecret_ExposeReturnsPlaintext(t *testing.T) {
	s := NewSecret("atk_supersecretvalueXYZ", "atk_")
	if s.Expose() != "atk_supersecretvalueXYZ" {
		t.Fatalf("Expose returned %q", s.Expose())
	}
}

func TestSecret_PrefixOnlyShownInUI(t *testing.T) {
	s := NewSecret("atk_abcdefghij1234", "atk_")
	if got, want := s.Prefix(), "atk_abcd"; got != want {
		t.Fatalf("Prefix: got %q want %q", got, want)
	}
}
