package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestIsWidgetProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"no args", nil, false},
		{"exact flag", []string{"--widget"}, true},
		{"flag after others", []string{"-psn_0_12345", "--widget"}, true},
		{"unrelated flags only", []string{"--debug"}, false},
		{"must not match a prefix", []string{"--widgetfoo"}, false},
		{"must not match a substring", []string{"--no-widget"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWidgetProcess(tc.args); got != tc.want {
				t.Fatalf("isWidgetProcess(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestWidgetEntryRewrite(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
	})
	h := widgetEntryRewrite(next)

	for _, tc := range []struct{ in, want string }{
		{"/", "/" + widgetEntryDocument},
		{"/index.html", "/" + widgetEntryDocument},
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

func TestWidgetEntryRewriteDoesNotMutateCallerRequest(t *testing.T) {
	// The middleware clones before editing; mutating the caller's request
	// in place would corrupt anything upstream that still holds it.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h := widgetEntryRewrite(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if req.URL.Path != "/" {
		t.Fatalf("caller's request was mutated to %q", req.URL.Path)
	}
}

func TestWidgetGeometryIsSelfConsistent(t *testing.T) {
	if widgetWidth <= 0 || widgetHeightInitial <= 0 {
		t.Fatal("widget geometry must be positive")
	}
	// The initial height is only a pre-measurement guess; it must still fit
	// inside the bound the frontend is held to.
	if widgetHeightInitial > widgetMaxHeight {
		t.Fatalf("initial height %d exceeds the cap %d", widgetHeightInitial, widgetMaxHeight)
	}
}

func TestWidgetWindowOptionsAllowContentDrivenHeight(t *testing.T) {
	opts := newWidgetWindowOptions(fstest.MapFS{}, NewWidgetBridge())

	if opts.DisableResize {
		t.Fatal("DisableResize locks GTK min/max to the startup size and rejects content-driven resizing")
	}
	if opts.MinWidth != widgetWidth || opts.MaxWidth != widgetWidth {
		t.Fatalf("widget width constraints = %d..%d, want %d..%d", opts.MinWidth, opts.MaxWidth, widgetWidth, widgetWidth)
	}
	if opts.MinHeight != 1 || opts.MaxHeight != widgetMaxHeight {
		t.Fatalf("widget height constraints = %d..%d, want 1..%d", opts.MinHeight, opts.MaxHeight, widgetMaxHeight)
	}
}

// The window height is reported by the frontend (ResizeObserver on the
// rendered card) rather than hardcoded, because it varies with row count,
// font and locale — a constant clipped the card's bottom edge. Go's only job
// is to reject values that would be nonsense.
func TestWidgetResizeHeightBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     int
		accept bool
	}{
		{"zero is ignored", 0, false},
		{"negative is ignored", -12, false},
		{"a real card height passes", 60, true},
		{"the cap itself passes", widgetMaxHeight, true},
		{"beyond the cap is clamped, not rejected", widgetMaxHeight + 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clampWidgetHeight(tc.in)
			if !tc.accept {
				if got != 0 {
					t.Fatalf("height %d should be ignored, got %d", tc.in, got)
				}
				return
			}
			if got <= 0 || got > widgetMaxHeight {
				t.Fatalf("height %d resolved to %d, outside (0, %d]", tc.in, got, widgetMaxHeight)
			}
		})
	}
}
