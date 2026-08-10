//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>

// atterm_widget_configure fixes two things Wails v2.12 gives no option for.
//
// 1. Activation policy. An accessory app has no Dock tile, no menu bar, and
//    does not appear in Cmd-Tab — what every desktop widget needs. LSUIElement
//    cannot be used: the widget is a second process of the SAME bundle as the
//    main window, and Info.plist keys are per-bundle. Wails does not expose
//    ActivationPolicy either (pkg/options/mac/mac.go has the whole type and
//    field commented out, with no implementation in the module), and it
//    hardcodes NSApplicationActivationPolicyRegular in
//    AppDelegate.m applicationWillFinishLaunching.
//
// 2. Window opacity. Wails never calls setOpaque:NO, so a frameless window
//    still composites on an opaque backing and the card's rounded corners
//    show a light rectangle behind them. mac.Options.WindowIsTranslucent is
//    NOT the fix — it inserts an NSVisualEffectView (WailsContext.m:185) whose
//    light material is exactly that white fringe.
//
// Both must run on the main queue: OnStartup is invoked from a Go goroutine,
// and AppKit calls off the main thread are silently ignored — which is why
// setting the policy directly in OnStartup appears to do nothing.
// dispatch_async also lands after Wails' own startup, so it wins the race
// against applicationWillFinishLaunching.
static void atterm_widget_configure(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
            for (NSWindow *w in [NSApp windows]) {
                [w setOpaque:NO];
                [w setBackgroundColor:[NSColor clearColor]];
                // Let AppKit derive the shadow from the content's alpha so it
                // hugs the card's rounded corners instead of outlining the
                // full rectangle.
                [w setHasShadow:YES];
            }
        }
    });
}
*/
import "C"

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// applyWidgetPlatformOptions configures the companion window for macOS.
func applyWidgetPlatformOptions(opts *options.App) {
	opts.Mac = &mac.Options{
		WebviewIsTransparent: true,
		// Deliberately NOT WindowIsTranslucent: that inserts a light
		// NSVisualEffectView behind the content, which shows through the
		// card's rounded corners as a white fringe. Real transparency comes
		// from setOpaque:NO in atterm_widget_configure.
		DisableZoom: true,
	}
}

// applyWidgetPostStartup runs from OnStartup, once Wails has finished its own
// NSApplication setup.
func applyWidgetPostStartup(_ context.Context) {
	C.atterm_widget_configure()
}
