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
	if petHeightCollapse >= petHeightExpanded {
		t.Fatal("collapsed height must be smaller than expanded")
	}
	if petWidth <= 0 || petHeightCollapse <= 0 {
		t.Fatal("pet geometry must be positive")
	}
}
