package main

import (
	"context"
	"embed"
	"errors"
	"os"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version is set at build time via -ldflags -X main.Version=<tag>.
// Empty / "dev" disables the auto-update subsystem.
var Version = "dev"

func main() {
	// The companion window ("桌面挂件" / Desk Widget) is a second process of this same
	// executable. It must branch before any of the setup below: it owns no
	// config, no keychain entry, no log file, and no relay host — everything
	// it renders arrives on stdin. See
	// docs/superpowers/specs/2026-08-10-desk-widget-design.md.
	if isWidgetProcess(os.Args[1:]) {
		if err := runWidgetWindow(assets); err != nil {
			println("Error:", err.Error())
		}
		return
	}

	// A `wails dev` build leaves Version unset / "dev". Route all of its data
	// (config, recovery, local relay db, cache, logs, credentials) to a
	// project-local .atterm-dev directory so development never reads, clobbers,
	// or pollutes the installed app's state or the system app dirs. Must run
	// before any path is computed below.
	if isDevBuild(Version) {
		appdir.UseDev()
		// Dev builds are unsigned, so the OS keychain is unreliable (macOS
		// prompts / kills the helper). Keep secrets in the project-local file
		// store instead — no keychain prompts during development.
		safekeyring.UseFileStore()
	}

	configurePlatformEnvironment()

	cfgStore := loadConfig()
	logger, err := newDesktopLoggingManager(cfgStore.Get(), Version)
	if err != nil {
		println("Error:", err.Error())
		return
	}
	defer logger.Close()

	app := NewApp(cfgStore, logger)

	opts := &options.App{
		Title:  "AT Term",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: newPluginFSHandler(app.pluginFS),
		},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		ErrorFormatter:   frontendErrorFormatter,
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose: func(ctx context.Context) bool {
			return app.beforeClose(ctx, func() {
				wailsruntime.EventsEmit(ctx, "before-close")
			})
		},
		Bind: []interface{}{
			app,
			app.pluginFS,
		},
	}

	mergePlatformOptions(opts, platformOptions())

	if err := wails.Run(opts); err != nil {
		println("Error:", err.Error())
	}
}

// frontendErrorFormatter decides what a rejected bound method sends to the
// frontend (options.ErrorFormatter, func(error) any).
//
// Without one, Wails serialises every rejection as err.Error() — a plain
// string. That is fatal for the unknown-host-key prompt: HostKeyUnknownError
// carries the fingerprint and the hop it belongs to in its *fields*, and
// Error() returns only the bare "ssh_host_key_unknown" sentinel. The frontend
// detects the case structurally (frontend/src/lib/sshHostKey.ts reads
// Fingerprint/Host/HopIndex/HopName), so with a string it can never open the
// TOFU dialog and the user is left staring at the sentinel with no way to
// accept the key. That is the whole of single-host TOFU, and — since a chain's
// prompt must name *which* hop an unfamiliar fingerprint belongs to — the whole
// of jump-host TOFU too.
//
// Everything else must keep arriving exactly as it does today. This formatter
// is cross-cutting: it applies to every bound method, and frontend callers
// almost universally do `e instanceof Error ? e.message : String(e)`, which
// only reads right because the rejection is a string. Returning err.Error() for
// the default case preserves current behaviour by construction — do not widen
// it to a richer shape for all errors.
//
// The struct is returned as-is: it has no json tags, so encoding/json emits the
// capitalised field names the frontend already reads. errors.As, not a type
// assertion, because the error may be wrapped on its way up the connect path.
func frontendErrorFormatter(err error) any {
	var hostKey *HostKeyUnknownError
	if errors.As(err, &hostKey) {
		return hostKey
	}
	return err.Error()
}

// isWidgetProcess reports whether this process was launched as the companion
// window. Kept as a plain scan rather than a flag package so it can run before
// any other initialisation and stays indifferent to whatever else the OS or a
// launcher appended to argv.
func isWidgetProcess(args []string) bool {
	for _, a := range args {
		if a == "--widget" {
			return true
		}
	}
	return false
}

// mergePlatformOptions copies fields that are non-zero on `p` into `into`.
// Only the fields actually set by any platform implementation need to be
// listed here.
func mergePlatformOptions(into *options.App, p *options.App) {
	if p == nil {
		return
	}
	if p.Menu != nil {
		into.Menu = p.Menu
	}
	if p.Mac != nil {
		into.Mac = p.Mac
	}
	if p.Windows != nil {
		into.Windows = p.Windows
	}
	if p.Linux != nil {
		into.Linux = p.Linux
	}
	if p.Frameless {
		into.Frameless = p.Frameless
	}
}
