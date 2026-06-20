package main

import (
	"context"
	"embed"

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
