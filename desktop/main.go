package main

import (
	"context"
	"embed"
	stdruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version is set at build time via -ldflags -X main.Version=<tag>.
// Empty / "dev" disables the auto-update subsystem (the running build is
// not from a tagged release, so there's no sensible base to compare).
var Version = "dev"

func main() {
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
			Assets: assets,
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

	// macOS only: install a custom menu that keeps native App + Edit
	// submenus (Hide / Quit / Cut / Copy / Paste / Select All) but omits
	// the Window submenu. The Window submenu is where Cocoa would bind
	// ⌘W / ⌘M, and we need ⌘W for "close pane" and don't want to claim
	// ⌘M either. Linux and Windows webviews don't have this problem.
	if stdruntime.GOOS == "darwin" {
		opts.Menu = darwinMenu()
	}

	if err := wails.Run(opts); err != nil {
		println("Error:", err.Error())
	}
}

func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	return m
}
