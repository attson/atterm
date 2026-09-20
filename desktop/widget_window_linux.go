//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0 xext
#include <gtk/gtk.h>
#include <gdk/gdkx.h>
#include <X11/Xatom.h>
#include <X11/extensions/sync.h>

typedef struct {
	GWeakRef widget;
	guint odd_samples;
} AttermWidgetFrameWatch;

typedef struct {
	GtkWidget *widget;
	gint x;
	gint y;
	gint width;
	gint height;
} AttermWidgetRecovery;

static gboolean atterm_widget_clear_attention(gpointer data) {
	GtkWidget *widget = GTK_WIDGET(data);
	gtk_window_set_urgency_hint(GTK_WINDOW(widget), FALSE);
	GdkWindow *window = gtk_widget_get_window(widget);
	if (window == NULL || !GDK_IS_X11_WINDOW(window))
		return G_SOURCE_REMOVE;

	Display *display = GDK_WINDOW_XDISPLAY(window);
	XEvent event = {0};
	event.xclient.type = ClientMessage;
	event.xclient.window = GDK_WINDOW_XID(window);
	event.xclient.message_type = XInternAtom(display, "_NET_WM_STATE", False);
	event.xclient.format = 32;
	event.xclient.data.l[0] = 0; // _NET_WM_STATE_REMOVE
	event.xclient.data.l[1] = XInternAtom(display,
		"_NET_WM_STATE_DEMANDS_ATTENTION", False);
	event.xclient.data.l[3] = 1; // application-originated request
	XSendEvent(display, DefaultRootWindow(display), False,
		SubstructureRedirectMask | SubstructureNotifyMask, &event);
	XFlush(display);
	return G_SOURCE_REMOVE;
}

static gboolean atterm_widget_show_after_stall(gpointer data) {
	AttermWidgetRecovery *recovery = data;
	GtkWindow *gtk_window = GTK_WINDOW(recovery->widget);
	gtk_window_move(gtk_window, recovery->x, recovery->y);
	gtk_window_resize(gtk_window, recovery->width, recovery->height);
	gtk_widget_show(recovery->widget);
	// Mutter may treat a background remap as a request for attention and may
	// choose a fresh placement. This recovery is invisible maintenance, so
	// restore the user's geometry and clear that hint after mapping too.
	gtk_window_move(gtk_window, recovery->x, recovery->y);
	gtk_window_set_urgency_hint(gtk_window, FALSE);
	// Mutter adds DEMANDS_ATTENTION asynchronously after MapNotify when a
	// background application remaps a window. Remove it after that WM turn.
	g_timeout_add_full(G_PRIORITY_DEFAULT, 250,
		atterm_widget_clear_attention, g_object_ref(recovery->widget), g_object_unref);
	GdkWindow *window = gtk_widget_get_window(recovery->widget);
	if (window != NULL && GDK_IS_X11_WINDOW(window))
		gdk_x11_window_set_frame_sync_enabled(window, FALSE);
	return G_SOURCE_REMOVE;
}

static void atterm_widget_recovery_free(gpointer data) {
	AttermWidgetRecovery *recovery = data;
	g_object_unref(recovery->widget);
	g_free(recovery);
}

static gboolean atterm_widget_extended_counter_is_odd(GdkWindow *window,
		gboolean *is_odd) {
	Display *display = GDK_WINDOW_XDISPLAY(window);
	Atom property = XInternAtom(display, "_NET_WM_SYNC_REQUEST_COUNTER", False);
	Atom actual_type = None;
	int actual_format = 0;
	unsigned long item_count = 0;
	unsigned long bytes_after = 0;
	unsigned char *raw = NULL;

	int status = XGetWindowProperty(display, GDK_WINDOW_XID(window), property,
		0, 2, False, XA_CARDINAL, &actual_type, &actual_format,
		&item_count, &bytes_after, &raw);
	if (status != Success || actual_type != XA_CARDINAL ||
		actual_format != 32 || item_count < 2 || raw == NULL) {
		if (raw != NULL)
			XFree(raw);
		return FALSE;
	}

	XSyncCounter extended = (XSyncCounter)((unsigned long *)raw)[1];
	XFree(raw);
	XSyncValue value;
	if (!XSyncQueryCounter(display, extended, &value))
		return FALSE;
	*is_odd = (XSyncValueLow32(value) & 1U) != 0;
	return TRUE;
}

static gboolean atterm_widget_watch_frames(gpointer data) {
	AttermWidgetFrameWatch *watch = data;
	GtkWidget *widget = g_weak_ref_get(&watch->widget);
	if (widget == NULL)
		return G_SOURCE_REMOVE;

	GdkWindow *window = gtk_widget_get_window(widget);
	gboolean odd = FALSE;
	if (!gtk_widget_get_mapped(widget) || window == NULL ||
		!GDK_IS_X11_WINDOW(window) ||
		!atterm_widget_extended_counter_is_odd(window, &odd)) {
		watch->odd_samples = 0;
		g_object_unref(widget);
		return G_SOURCE_CONTINUE;
	}

	watch->odd_samples = odd ? watch->odd_samples + 1 : 0;
	if (watch->odd_samples >= 3) {
		watch->odd_samples = 0;
		AttermWidgetRecovery *recovery = g_new0(AttermWidgetRecovery, 1);
		recovery->widget = g_object_ref(widget);
		gtk_window_get_position(GTK_WINDOW(widget), &recovery->x, &recovery->y);
		gtk_window_get_size(GTK_WINDOW(widget), &recovery->width, &recovery->height);
		// GTK clears frame_pending and thaws the frame clock from its
		// UnmapNotify handler. Showing on a later main-loop turn ensures the
		// unmap is observed before another frame is requested.
		gtk_widget_hide(widget);
		g_timeout_add_full(G_PRIORITY_DEFAULT, 100,
			atterm_widget_show_after_stall, recovery, atterm_widget_recovery_free);
	}

	g_object_unref(widget);
	return G_SOURCE_CONTINUE;
}

static void atterm_widget_frame_watch_free(gpointer data) {
	AttermWidgetFrameWatch *watch = data;
	g_weak_ref_clear(&watch->widget);
	g_free(watch);
}

static void atterm_widget_ensure_frame_watch(GtkWidget *widget) {
	static const char *watch_key = "atterm-widget-frame-watch";
	if (g_object_get_data(G_OBJECT(widget), watch_key) != NULL)
		return;

	AttermWidgetFrameWatch *watch = g_new0(AttermWidgetFrameWatch, 1);
	g_weak_ref_init(&watch->widget, G_OBJECT(widget));
	g_object_set_data(G_OBJECT(widget), watch_key, GINT_TO_POINTER(1));
	g_timeout_add_seconds_full(G_PRIORITY_DEFAULT, 1,
		atterm_widget_watch_frames, watch, atterm_widget_frame_watch_free);
}

static gboolean atterm_widget_disable_frame_sync(gpointer data) {
	guint *attempts = data;
	(*attempts)++;
	GList *windows = gtk_window_list_toplevels();
	gboolean applied = FALSE;
	for (GList *item = windows; item != NULL; item = item->next) {
		GtkWidget *widget = GTK_WIDGET(item->data);
		if (!GTK_IS_WINDOW(widget) ||
			g_strcmp0(gtk_window_get_title(GTK_WINDOW(widget)), "AT Term Widget") != 0)
			continue;
		GdkWindow *window = gtk_widget_get_window(widget);
		if (window != NULL && GDK_IS_X11_WINDOW(window)) {
			gdk_x11_window_set_frame_sync_enabled(window, FALSE);
			atterm_widget_ensure_frame_watch(widget);
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
	// Keep every retry one-shot. A high-priority idle source that returned
	// CONTINUE here would spin and starve the GTK realization it is waiting for.
	g_timeout_add_full(G_PRIORITY_DEFAULT, 10,
		atterm_widget_disable_frame_sync, attempts, NULL);
	return G_SOURCE_REMOVE;
}

static void atterm_widget_schedule_disable_frame_sync(void) {
	// Run before GTK's redraw idles. Disabling after the first synchronized frame
	// is already pending only prevents the next freeze; it does not thaw the
	// frame clock that is currently waiting for Mutter's acknowledgement.
	g_idle_add_full(G_PRIORITY_DEFAULT, atterm_widget_disable_frame_sync,
		g_new0(guint, 1), NULL);
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

// applyWidgetPostStartup starts the bounded realization retry before the hidden
// webview paints its first frame. It also installs the XSync watchdog that
// recovers the runtime stall where Mutter leaves a frame's extended update
// counter odd indefinitely.
func applyWidgetPostStartup(_ context.Context) {
	C.atterm_widget_schedule_disable_frame_sync()
}

// applyWidgetPostReady is a second bounded attempt after the frontend has
// mounted. It covers unusual startup ordering where OnStartup's retry budget
// expires before Wails realizes the native window. Wails invokes both hooks on
// Go goroutines, so GTK work is always queued onto the main loop.
func applyWidgetPostReady(_ context.Context) {
	C.atterm_widget_schedule_disable_frame_sync()
}
