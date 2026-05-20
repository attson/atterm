//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// platformOptions returns macOS-specific Wails options merged into the
// shared options.App in main.go. Mac.TitleBar = TitleBarHiddenInset gives
// us a transparent title bar with full-size content under the traffic
// lights so our TitleBar component can occupy that row.
func platformOptions() *options.App {
	return &options.App{
		Menu: darwinMenu(),
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	}
}

func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	return m
}
