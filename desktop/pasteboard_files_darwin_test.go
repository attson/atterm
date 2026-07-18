//go:build darwin

package main

import "testing"

// TestReadPasteboardFileURLs_NoPanic is a smoke test: the cgo call must not
// panic on any environment, and every returned entry must be a non-empty
// string. Actual pasteboard content is test-env dependent, so we don't
// assert values here — behavior is exercised via manual verification.
func TestReadPasteboardFileURLs_NoPanic(t *testing.T) {
	got := readPasteboardFileURLs()
	for i, p := range got {
		if p == "" {
			t.Fatalf("entry %d is empty", i)
		}
	}
}
