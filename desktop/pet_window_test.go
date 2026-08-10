package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPetProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"no args", nil, false},
		{"exact flag", []string{"--pet"}, true},
		{"flag after others", []string{"-psn_0_12345", "--pet"}, true},
		{"unrelated flags only", []string{"--debug"}, false},
		{"must not match a prefix", []string{"--petfoo"}, false},
		{"must not match a substring", []string{"--no-pet"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPetProcess(tc.args); got != tc.want {
				t.Fatalf("isPetProcess(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestPetEntryRewrite(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
	})
	h := petEntryRewrite(next)

	for _, tc := range []struct{ in, want string }{
		{"/", "/" + petEntryDocument},
		{"/index.html", "/" + petEntryDocument},
		// Shared hashed chunks must pass through untouched — both windows are
		// served from the same asset tree.
		{"/assets/index-abc123.js", "/assets/index-abc123.js"},
		{"/assets/style-def456.css", "/assets/style-def456.css"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.in, nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if seen != tc.want {
			t.Fatalf("path %q rewritten to %q, want %q", tc.in, seen, tc.want)
		}
	}
}

func TestPetEntryRewriteDoesNotMutateCallerRequest(t *testing.T) {
	// The middleware clones before editing; mutating the caller's request
	// in place would corrupt anything upstream that still holds it.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h := petEntryRewrite(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if req.URL.Path != "/" {
		t.Fatalf("caller's request was mutated to %q", req.URL.Path)
	}
}

func TestPetGeometryIsSelfConsistent(t *testing.T) {
	if petWidth <= 0 || petHeightInitial <= 0 {
		t.Fatal("pet geometry must be positive")
	}
	// The initial height is only a pre-measurement guess; it must still fit
	// inside the bound the frontend is held to.
	if petHeightInitial > petMaxHeight {
		t.Fatalf("initial height %d exceeds the cap %d", petHeightInitial, petMaxHeight)
	}
}

// The window height is reported by the frontend (ResizeObserver on the
// rendered card) rather than hardcoded, because it varies with row count,
// font and locale — a constant clipped the card's bottom edge. Go's only job
// is to reject values that would be nonsense.
func TestPetResizeHeightBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     int
		accept bool
	}{
		{"zero is ignored", 0, false},
		{"negative is ignored", -12, false},
		{"a real card height passes", 60, true},
		{"the cap itself passes", petMaxHeight, true},
		{"beyond the cap is clamped, not rejected", petMaxHeight + 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clampPetHeight(tc.in)
			if !tc.accept {
				if got != 0 {
					t.Fatalf("height %d should be ignored, got %d", tc.in, got)
				}
				return
			}
			if got <= 0 || got > petMaxHeight {
				t.Fatalf("height %d resolved to %d, outside (0, %d]", tc.in, got, petMaxHeight)
			}
		})
	}
}
