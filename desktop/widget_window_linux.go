//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <gdk/gdkx.h>

static gboolean atterm_widget_disable_frame_sync(gpointer data) {
	guint *attempts = data;
	(*attempts)++;
	GList *windows = gtk_window_list_toplevels();
	gboolean applied = FALSE;
	for (GList *item = windows; item != NULL; item = item->next) {
		GtkWidget *widget = GTK_WIDGET(item->data);
		GdkWindow *window = gtk_widget_get_window(widget);
		if (window != NULL && GDK_IS_X11_WINDOW(window)) {
			gdk_x11_window_set_frame_sync_enabled(window, FALSE);
			applied = TRUE;
		}
	}
	g_list_free(windows);
	// Ready is normally after realization, but keep the callback alive for a
	// short bounded period if a compositor or Wails delays mapping the window.
	// Returning REMOVE when the list is empty was the startup race: the only
	// attempt could happen before gtk_widget_get_window() returned a GdkWindow.
	if (applied || *attempts >= 200) {
		g_free(attempts);
		return G_SOURCE_REMOVE;
	}
	return G_SOURCE_CONTINUE;
}

static void atterm_widget_schedule_disable_frame_sync(void) {
	// An idle callback can spin if the window has not been realized yet. A
	// short timeout retries without consuming a core and is cancelled as soon
	// as the first X11 toplevel is updated.
	g_timeout_add(50, atterm_widget_disable_frame_sync, g_new0(guint, 1));
}
*/
import "C"

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

// applyWidgetPostStartup intentionally does nothing on Linux. The GTK window
// may not be realized when Wails invokes OnStartup, so the X11 frame-sync
// operation is deferred to applyWidgetPostReady below.
func applyWidgetPostStartup(_ context.Context) {}

// applyWidgetPostReady runs after the frontend has mounted. Wails invokes the
// bound method on a Go goroutine, so GTK work is queued onto the main loop.
// Mutter's X11 frame-sync
// protocol can stop acknowledging a window after its first frame; GTK then
// keeps the process responsive but suppresses every subsequent draw. The
// widget does not need compositor pacing, so opt its sole toplevel out.
func applyWidgetPostReady(_ context.Context) {
	C.atterm_widget_schedule_disable_frame_sync()
}
