//go:build !darwin

package main

// readPasteboardFileURLs on non-darwin has no NSPasteboard equivalent wired
// up yet; callers treat an empty result as "no source path, fall back to the
// PASTE_FILE upload path". Extend per-platform when we ship linux/windows.
func readPasteboardFileURLs() []string { return nil }
