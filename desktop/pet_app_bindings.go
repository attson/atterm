package main

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Bindings for the companion ("AI 宠物") window. The frontend owns the state
// projection (lib/petState.ts) and the enable flag (PluginConfig.Pet.Enabled);
// Go owns only the child process.

// StartPet spawns the companion window. Idempotent — the frontend reconciles
// on every plugin-config change and calls this unconditionally when enabled.
func (a *App) StartPet() error {
	cfg := a.cfgStore.Get()
	return a.pet.Start(petBootstrap{
		Collapsed: cfg.Plugins.Pet.Collapsed,
		X:         cfg.Plugins.Pet.WindowX,
		Y:         cfg.Plugins.Pet.WindowY,
		Locale:    cfg.LocalePreferenceOrDefault(),
	})
}

// StopPet terminates the companion window. Idempotent.
func (a *App) StopPet() {
	a.pet.Stop()
}

// PushPetState forwards a serialized PetState snapshot to the companion
// window. The payload is opaque here on purpose: mirroring the projection's
// shape in Go would create a second definition to keep in sync with
// lib/petState.ts, which is where it is unit-tested.
func (a *App) PushPetState(payload string) {
	if err := a.pet.PushState(payload); err != nil {
		logWarn("pet", "state push failed: %v", err)
	}
}

// handlePetEvent routes a child→parent event. Runs on the pipe reader
// goroutine, so it must not block.
func (a *App) handlePetEvent(ev petEvent) {
	switch ev.Type {
	case "activate":
		if a.ctx == nil || ev.SessionID == "" {
			return
		}
		// Raise the main window first: the click happened in another process's
		// window, so this one is in the background and would otherwise route
		// to a tab the user cannot see.
		wailsruntime.WindowShow(a.ctx)
		wailsruntime.WindowUnminimise(a.ctx)
		// Reuse the notification deep-link event verbatim so "click a pet row"
		// and "click a notification" share one tab/pane routing path in
		// App.vue rather than growing a second, drifting one.
		if a.eventsEmitter != nil {
			a.eventsEmitter(a.ctx, notificationClickEvent, map[string]interface{}{
				"session_id": ev.SessionID,
				"kind":       "pet",
			})
		}
	case "collapse":
		a.mutatePetConfig(func(p *PetConfig) { p.Collapsed = ev.Collapsed })
	case "move":
		a.mutatePetConfig(func(p *PetConfig) {
			p.WindowX = ev.X
			p.WindowY = ev.Y
		})
	case "mute":
		a.mutatePetConfig(func(p *PetConfig) { p.MutedUntilUnix = ev.MutedUntilUnix })
	case "hide":
		// "Hide" from the pet's own menu means "turn the plugin off", so the
		// Settings toggle and the pet never disagree about whether it is on.
		a.mutatePetConfig(func(p *PetConfig) { p.Enabled = false })
		a.pet.Stop()
	default:
		logDebug("pet", "ignoring unknown event type %q", ev.Type)
	}
}

// mutatePetConfig applies a change to the persisted pet block and broadcasts
// the new plugin config so Settings stays in sync with the pet's own menu.
func (a *App) mutatePetConfig(mutate func(*PetConfig)) {
	cur := a.cfgStore.Get()
	mutate(&cur.Plugins.Pet)
	if err := a.cfgStore.Set(cur); err != nil {
		logWarn("pet", "persist config failed: %v", err)
		return
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "plugin-config-changed", cur.Plugins)
	}
}
