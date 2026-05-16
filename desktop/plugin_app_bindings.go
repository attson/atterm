package main

import (
	"fmt"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetPluginConfig returns the current plugin configuration.
func (a *App) GetPluginConfig() PluginConfig {
	return a.cfgStore.Get().Plugins
}

// SetPluginConfig validates, persists, and broadcasts the new plugin config.
// The event payload is the full PluginConfig — the frontend consumes it from
// a single Pinia store, no diffing.
func (a *App) SetPluginConfig(next PluginConfig) error {
	if err := ValidatePluginConfig(next); err != nil {
		return fmt.Errorf("plugin config invalid: %w", err)
	}
	cur := a.cfgStore.Get()
	cur.Plugins = next
	if err := a.cfgStore.Set(cur); err != nil {
		return err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "plugin-config-changed", next)
	}
	return nil
}
