//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// platformOptions returns macOS-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{
		Menu: darwinMenu(),
	}
}

// darwinMenu installs a custom menu that keeps native App + Edit submenus
// (Hide / Quit / Cut / Copy / Paste / Select All) but omits the Window
// submenu, where Cocoa would bind ⌘W / ⌘M — we need ⌘W for "close pane"
// and don't want to claim ⌘M either.
func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	return m
}
