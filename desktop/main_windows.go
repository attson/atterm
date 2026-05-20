//go:build windows

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// platformOptions returns Windows-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{}
}
