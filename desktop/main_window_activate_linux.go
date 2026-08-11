//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdlib.h>
#include <string.h>

static int atterm_has_wm_state(Display *display, Window window, Atom wmStateAtom) {
	Atom actualType = None;
	int actualFormat = 0;
	unsigned long itemCount = 0;
	unsigned long bytesAfter = 0;
	unsigned char *property = NULL;
	int status = XGetWindowProperty(
		display,
		window,
		wmStateAtom,
		0,
		2,
		False,
		AnyPropertyType,
		&actualType,
		&actualFormat,
		&itemCount,
		&bytesAfter,
		&property
	);
	if (property != NULL) {
		XFree(property);
	}
	return status == Success && actualType != None && itemCount > 0;
}

static Window atterm_find_window_by_pid(
	Display *display,
	Window parent,
	Atom pidAtom,
	Atom wmStateAtom,
	unsigned long pid
) {
	Window root = 0;
	Window parentReturn = 0;
	Window *children = NULL;
	unsigned int childCount = 0;

	if (!XQueryTree(display, parent, &root, &parentReturn, &children, &childCount)) {
		return 0;
	}

	Window found = 0;
	for (unsigned int i = 0; i < childCount && found == 0; i++) {
		Atom actualType = None;
		int actualFormat = 0;
		unsigned long itemCount = 0;
		unsigned long bytesAfter = 0;
		unsigned char *property = NULL;
		int status = XGetWindowProperty(
			display,
			children[i],
			pidAtom,
			0,
			1,
			False,
			XA_CARDINAL,
			&actualType,
			&actualFormat,
			&itemCount,
			&bytesAfter,
			&property
		);
		if (status == Success && property != NULL && actualFormat == 32 && itemCount == 1) {
			unsigned long windowPID = *((unsigned long *)property);
			XFree(property);
			property = NULL;
			// Wails also creates a 10x10, unmapped InputOnly helper window with
			// the process PID. WM_STATE is set by the window manager only on
			// real managed application windows, including minimised ones.
			if (windowPID == pid && atterm_has_wm_state(display, children[i], wmStateAtom)) {
				found = children[i];
				break;
			}
		}
		if (property != NULL) {
			XFree(property);
		}
		if (found == 0) {
			found = atterm_find_window_by_pid(display, children[i], pidAtom, wmStateAtom, pid);
		}
	}

	if (children != NULL) {
		XFree(children);
	}
	return found;
}

// Returns 0 on success, 1 when DISPLAY cannot be opened, 2 when the process
// window is not found, and 3 when the WM activation message cannot be sent.
static int atterm_activate_window_for_pid(unsigned long pid) {
	Display *display = XOpenDisplay(NULL);
	if (display == NULL) {
		return 1;
	}

	Window root = DefaultRootWindow(display);
	Atom pidAtom = XInternAtom(display, "_NET_WM_PID", True);
	Atom wmStateAtom = XInternAtom(display, "WM_STATE", True);
	if (pidAtom == None || wmStateAtom == None) {
		XCloseDisplay(display);
		return 2;
	}
	Window target = atterm_find_window_by_pid(display, root, pidAtom, wmStateAtom, pid);
	if (target == 0) {
		XCloseDisplay(display);
		return 2;
	}

	Atom activeAtom = XInternAtom(display, "_NET_ACTIVE_WINDOW", False);
	XEvent event;
	memset(&event, 0, sizeof(event));
	event.xclient.type = ClientMessage;
	event.xclient.send_event = True;
	event.xclient.display = display;
	event.xclient.window = target;
	event.xclient.message_type = activeAtom;
	event.xclient.format = 32;
	// EWMH source indication 2 means a pager/user-driven activation. The
	// request originates from a real click in the widget process.
	event.xclient.data.l[0] = 2;
	event.xclient.data.l[1] = CurrentTime;
	event.xclient.data.l[2] = 0;

	Status sent = XSendEvent(
		display,
		root,
		False,
		SubstructureRedirectMask | SubstructureNotifyMask,
		&event
	);
	XFlush(display);
	XCloseDisplay(display);
	return sent == 0 ? 3 : 0;
}
*/
import "C"

import (
	"fmt"
	"os"
)

func requestMainWindowActivation() error {
	switch code := int(C.atterm_activate_window_for_pid(C.ulong(os.Getpid()))); code {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("open X11 display")
	case 2:
		return fmt.Errorf("find X11 window for pid %d", os.Getpid())
	default:
		return fmt.Errorf("send _NET_ACTIVE_WINDOW request")
	}
}
