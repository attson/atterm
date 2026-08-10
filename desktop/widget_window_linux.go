//go:build linux

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// applyWidgetPlatformOptions configures the companion window for Linux/GTK.
//
// Known gap: GTK's skip-taskbar hint is not exposed by Wails v2, so on some
// desktop environments the widget also appears in the window list. Reaching it
// would mean grabbing the GtkWindow* out of Wails' internals via cgo, which
// is fragile across Wails patch releases — not worth it for a cosmetic issue
// that varies by DE anyway. Revisit if Wails v3 exposes a window-hint API.
//
// WindowIsTranslucent is the only transparency knob Linux has (there is no
// WebviewIsTransparent on this platform), and it requires a compositing WM;
// without one the widget renders on an opaque background but stays usable.
func applyWidgetPlatformOptions(opts *options.App) {
	opts.Linux = &linux.Options{
		WindowIsTranslucent: true,
	}
}

// applyWidgetPostStartup runs from OnStartup. Nothing to do on Linux: the
// skip-taskbar hint would need the GtkWindow* out of Wails' internals.
func applyWidgetPostStartup(_ context.Context) {}
