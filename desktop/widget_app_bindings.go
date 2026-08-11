package main

import wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

// Bindings for the companion window ("桌面挂件" / Desk Widget). The frontend owns the state
// projection (lib/widgetState.ts) and the enable flag (PluginConfig.Widget.Enabled);
// Go owns only the child process.

// StartWidget spawns the companion window. Idempotent — the frontend reconciles
// on every plugin-config change and calls this unconditionally when enabled.
func (a *App) StartWidget() error {
	cfg := a.cfgStore.Get()
	return a.widget.Start(widgetBootstrap{
		Collapsed: cfg.Plugins.Widget.Collapsed,
		X:         cfg.Plugins.Widget.WindowX,
		Y:         cfg.Plugins.Widget.WindowY,
		Locale:    cfg.LocalePreferenceOrDefault(),
	})
}

// StopWidget terminates the companion window. Idempotent.
func (a *App) StopWidget() {
	a.widget.Stop()
}

// PushWidgetState forwards a serialized WidgetState snapshot to the companion
// window. The payload is opaque here on purpose: mirroring the projection's
// shape in Go would create a second definition to keep in sync with
// lib/widgetState.ts, which is where it is unit-tested.
func (a *App) PushWidgetState(payload string) {
	if err := a.widget.PushState(payload); err != nil {
		logWarn("widget", "state push failed: %v", err)
	}
}

// handleWidgetEvent routes a child→parent event. Runs on the pipe reader
// goroutine, so it must not block.
func (a *App) handleWidgetEvent(ev widgetEvent) {
	switch ev.Type {
	case "activate":
		if a.ctx == nil || ev.SessionID == "" {
			return
		}
		// Raise the main window first: the click happened in another process's
		// window, so this one is in the background and would otherwise route
		// to a tab the user cannot see. Linux needs an explicit WM activation
		// request in addition to Wails' show/unminimise calls.
		if a.windowActivator != nil {
			a.windowActivator(a.ctx)
		}
		// Widget activation is its own event. It must not reuse the notification
		// event, whose semantics belong to OS notification responses.
		if a.eventsEmitter != nil {
			a.eventsEmitter(a.ctx, "widget:activate", map[string]interface{}{
				"session_id": ev.SessionID,
			})
		}
	case "collapse":
		a.mutateWidgetConfig(func(p *WidgetConfig) { p.Collapsed = ev.Collapsed })
	case "move":
		a.mutateWidgetConfig(func(p *WidgetConfig) {
			p.WindowX = ev.X
			p.WindowY = ev.Y
		})
	case "mute":
		a.mutateWidgetConfig(func(p *WidgetConfig) { p.MutedUntilUnix = ev.MutedUntilUnix })
	case "ai-only":
		// The filter is applied by the projection in the main app, so this
		// only has to persist; the next push already carries the filtered list.
		a.mutateWidgetConfig(func(p *WidgetConfig) { p.AIOnly = ev.AIOnly })
	case "hide":
		// "Hide" from the widget's own menu means "turn the plugin off", so the
		// Settings toggle and the widget never disagree about whether it is on.
		a.mutateWidgetConfig(func(p *WidgetConfig) { p.Enabled = false })
		a.widget.Stop()
	default:
		logDebug("widget", "ignoring unknown event type %q", ev.Type)
	}
}

// mutateWidgetConfig applies a change to the persisted widget block and broadcasts
// the new plugin config so Settings stays in sync with the widget's own menu.
func (a *App) mutateWidgetConfig(mutate func(*WidgetConfig)) {
	cur := a.cfgStore.Get()
	mutate(&cur.Plugins.Widget)
	if err := a.cfgStore.Set(cur); err != nil {
		logWarn("widget", "persist config failed: %v", err)
		return
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "plugin-config-changed", cur.Plugins)
	}
}
