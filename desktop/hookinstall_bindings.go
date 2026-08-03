package main

import (
	"log"
	"sync"
	"time"

	"github.com/attson/atterm/desktop/hookinstall"
)

// hookInstallLastAttempt tracks when we last auto-repaired so the UI
// poll doesn't trigger a Check→Install loop while the underlying issue
// is permanent (e.g. read-only mount).
var (
	hookInstallLastAttempt   time.Time
	hookInstallLastAttemptMu sync.Mutex
)

const hookInstallRepairDebounce = 5 * time.Second

// GetHookInstallState returns the current health snapshot. When the
// surface is unhealthy and we haven't tried in the last 5 seconds,
// we kick a silent Install before returning the post-repair state.
func (a *App) GetHookInstallState() hookinstall.State {
	enabled := true
	if a.cfgStore != nil {
		enabled = a.cfgStore.Get().HookAutoInstallEnabledOrDefault()
	}
	s := hookinstall.Check(a.ctx, enabled)
	if !s.Healthy() && enabled && allowHookInstallRepair() {
		if err := hookinstall.Install(a.ctx); err != nil {
			log.Printf("hookinstall: auto-repair: %v", err)
		}
		s = hookinstall.Check(a.ctx, enabled)
	}
	return s
}

// SetHookInstallEnabled persists the toggle and either installs or
// uninstalls. Errors are returned to the frontend so the Retry button
// can surface them.
func (a *App) SetHookInstallEnabled(on bool) error {
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.HookAutoInstallEnabled = &on
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	if on {
		// Reset debounce so a manual toggle ALWAYS retries.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
		return hookinstall.Install(a.ctx)
	}
	return hookinstall.Uninstall(a.ctx)
}

func allowHookInstallRepair() bool {
	hookInstallLastAttemptMu.Lock()
	defer hookInstallLastAttemptMu.Unlock()
	if time.Since(hookInstallLastAttempt) < hookInstallRepairDebounce {
		return false
	}
	hookInstallLastAttempt = time.Now()
	return true
}
