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

func TestWidgetWindowOptionsAllowContentDrivenSize(t *testing.T) {
	opts := newWidgetWindowOptions(fstest.MapFS{}, NewWidgetBridge())

	if opts.DisableResize {
		t.Fatal("DisableResize locks GTK min/max to the startup size and rejects content-driven resizing")
	}
	if opts.MinWidth != 56 || opts.MaxWidth != widgetWidth {
		t.Fatalf("widget width constraints = %d..%d, want %d..%d", opts.MinWidth, opts.MaxWidth, 56, widgetWidth)
	}
	if opts.MinHeight != 1 || opts.MaxHeight != widgetMaxHeight {
		t.Fatalf("widget height constraints = %d..%d, want 1..%d", opts.MinHeight, opts.MaxHeight, widgetMaxHeight)
	}
}

// The window dimensions are reported by the frontend (ResizeObserver on the
// rendered card). Width changes for icon-only mode and height changes with
// content; Go only clamps the frontend request to sane bounds.
func TestWidgetResizeBounds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{"zero width is ignored", 0, 60, 0, 0},
		{"zero height is ignored", widgetWidth, 0, 0, 0},
		{"negative height is ignored", widgetWidth, -12, 0, 0},
		{"icon width passes", widgetMinWidth, widgetMinWidth, widgetMinWidth, widgetMinWidth},
		{"undersized width is clamped", 20, 60, widgetMinWidth, 60},
		{"normal card passes", widgetWidth, 60, widgetWidth, 60},
		{"oversized width is clamped", widgetWidth + 100, 60, widgetWidth, 60},
		{"oversized height is clamped", widgetWidth, widgetMaxHeight + 500, widgetWidth, widgetMaxHeight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := clampWidgetSize(tc.width, tc.height)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("size %dx%d resolved to %dx%d, want %dx%d", tc.width, tc.height, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestWaitForWidgetSizeWaitsForNativeFrame(t *testing.T) {
	reads := 0
	waits := 0
	confirmed := waitForWidgetSize(
		252,
		224,
		5,
		func() (int, int) {
			reads++
			if reads < 3 {
				return 252, 83
			}
			return 252, 224
		},
		func() { waits++ },
	)

	if !confirmed {
		t.Fatal("native target size was reached but not confirmed")
	}
	if reads != 3 || waits != 2 {
		t.Fatalf("resize confirmation used %d reads and %d waits, want 3 reads and 2 waits", reads, waits)
	}
}
