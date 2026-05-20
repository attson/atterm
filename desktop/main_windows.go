//go:build windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// platformOptions returns Windows-specific Wails options. Frameless: true
// removes the native title bar so our TitleBar component owns the full
// top row, including the self-drawn WindowControls (min/max/close).
// DisableFramelessWindowDecorations: false keeps Aero shadow and rounded
// corners on Win11.
func platformOptions() *options.App {
	return &options.App{
		Frameless: true,
		Windows: &windows.Options{
			DisableFramelessWindowDecorations: false,
		},
	}
}
