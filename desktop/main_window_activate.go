package main

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// activateMainWindow restores the Wails window and asks the platform window
// manager to focus it. The extra platform request matters on GNOME, where a
// background process calling Show alone produces an "application is ready"
// notification instead of activating the window.
func activateMainWindow(ctx context.Context) {
	wailsruntime.WindowShow(ctx)
	wailsruntime.WindowUnminimise(ctx)
	if err := requestMainWindowActivation(); err != nil {
		logDebug("widget", "main window activation request failed: %v", err)
	}
}
