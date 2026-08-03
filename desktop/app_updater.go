package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetUpdateState returns the current updater state. The frontend polls
// this from its existing 2s session-poll loop.
func (a *App) GetUpdateState() UpdateState {
	if a.updater == nil {
		return UpdateState{Current: "dev"}
	}
	return a.updater.State()
}

// CheckUpdate forces a fresh GitHub fetch, bypassing the 1h cache.
// Triggered by Settings > Updates > "Check now".
func (a *App) CheckUpdate() error {
	if a.updater == nil {
		return nil
	}
	return a.updater.Check(a.ctx, true)
}

// StartDownload begins fetching the platform asset to the cache dir.
// Idempotent if already running.
func (a *App) StartDownload() error {
	if a.updater == nil {
		return nil
	}
	return a.updater.Download(a.ctx)
}

// DownloadVersion downloads a specific version (the chosen update line's
// latest tag) instead of the default latest.
func (a *App) DownloadVersion(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.DownloadVersion(a.ctx, tag)
}

// CancelDownload interrupts an in-flight update download (if any) and
// clears the download state so the UI reverts to the pre-download
// primary button. Bound to Settings → Updates "Cancel (N%)" button.
func (a *App) CancelDownload() {
	if a.updater == nil {
		return
	}
	a.updater.Cancel()
}

// ForceRedownload deletes any existing archive for tag and downloads
// fresh. Bound to Settings → Updates "Redownload" button and the
// "redownload?" confirm prompt.
func (a *App) ForceRedownload(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.ForceRedownload(a.ctx, tag)
}

// InstallUpdate spawns the install helper detached and quits the app.
// The helper waits for our PID to exit then replaces the install and
// relaunches.
func (a *App) InstallUpdate() error {
	if a.updater == nil {
		return fmt.Errorf("updater not initialized")
	}
	if err := a.updater.InstallAndQuit(); err != nil {
		return err
	}
	// Quit ourselves so the helper's wait-for-PID-exit loop unblocks.
	go func() {
		// Tiny delay so this RPC return reaches the frontend before we exit.
		time.Sleep(200 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// GetAutoCheckUpdates reports the persisted preference (default true).
func (a *App) GetAutoCheckUpdates() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().AutoCheckUpdatesOrDefault()
}

// SetAutoCheckUpdates persists the preference and starts/stops the
// background loop accordingly.
func (a *App) SetAutoCheckUpdates(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.AutoCheckUpdates = &enabled
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if a.updater == nil {
		return nil
	}
	if enabled {
		a.updater.Start(a.ctx)
	} else {
		a.updater.Stop()
	}
	return nil
}

// GetUpdateGHProxyURL reports the optional GitHub release download proxy.
func (a *App) GetUpdateGHProxyURL() string {
	if a.cfgStore == nil {
		return ""
	}
	return a.cfgStore.Get().UpdateGHProxyURL
}

// SetUpdateGHProxyURL persists the optional GitHub release download proxy.
// Empty disables proxying. Non-empty values must be absolute http(s) URLs.
func (a *App) SetUpdateGHProxyURL(proxyURL string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	normalized, err := normalizeUpdateGHProxyURL(proxyURL)
	if err != nil {
		return err
	}
	cfg := a.cfgStore.Get()
	cfg.UpdateGHProxyURL = normalized
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if a.updater != nil {
		a.updater.SetGHProxyURL(normalized)
	}
	return nil
}

func normalizeUpdateGHProxyURL(proxyURL string) (string, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return "", nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("GitHub proxy URL must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("GitHub proxy URL must use http or https")
	}
	return proxyURL, nil
}
