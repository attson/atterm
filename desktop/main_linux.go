//go:build linux

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// platformOptions returns Linux-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{}
}
