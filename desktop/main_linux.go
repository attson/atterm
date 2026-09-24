//go:build linux

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// platformOptions returns Linux-specific Wails options. Frameless: true
// removes the native chrome so our TitleBar + WindowControls own the
// entire top row. Known limitation: GTK frameless windows lose
// WM-provided edge resize handles — documented in the spec; resize
// requires the maximize button or WM keyboard shortcuts.
func platformOptions() *options.App {
	return &options.App{
		Frameless: true,
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "AT-Term",
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	}
}
