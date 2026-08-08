package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/attson/atterm/internal/appdir"
)

// LoadRecoverySnapshot returns the most recent snapshot, or a zero value
// when there's nothing to recover. Side effect: rewrites the on-disk file
// with CleanShutdown=false so a crash during the recovery dialog is caught
// next launch.
//
// All return paths normalize Tabs to a non-nil slice — Go's `nil` slice
// marshals to JSON `null`, and the frontend's check
// `recoverySnap.tabs.length > 0` throws on `null` (uncaught, because the
// check sits outside the boot try/catch). Keeping Tabs as `[]` here makes
// that wire shape impossible to regress.
func (a *App) LoadRecoverySnapshot() (RecoverySnapshot, error) {
	if a.recoveryStore == nil {
		return RecoverySnapshot{Tabs: []TabSnapshot{}}, nil
	}
	snap, err := a.recoveryStore.Load()
	if err != nil {
		return RecoverySnapshot{Tabs: []TabSnapshot{}}, err
	}
	if snap.Tabs == nil {
		snap.Tabs = []TabSnapshot{}
	}
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	return snap, nil
}

// SaveRecoverySnapshot accepts a JSON-encoded RecoverySnapshot from the
// frontend (debounce-driven). Validates by unmarshalling into the typed
// struct so malformed payloads fail loudly. Server-side overrides
// Version/HostID/SavedAtUnix so the frontend can't lie about them.
func (a *App) SaveRecoverySnapshot(payload string) error {
	if a.recoveryStore == nil {
		return nil
	}
	var snap RecoverySnapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return fmt.Errorf("decode recovery snapshot: %w", err)
	}
	snap.Version = recoverySnapshotVersion
	snap.HostID = a.host.hostID
	snap.SavedAtUnix = a.recoveryStore.nowUnix()
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	if appdir.IsDev() {
		logDebug("recovery", "save %s", summarizeRecoverySnapshot(snap))
	}
	return a.recoveryStore.Save(snap)
}

// summarizeRecoverySnapshot renders a one-line view of what's being persisted
// — per pane: workload type, the atterm session id, and the captured AI
// session id (or "-"). Used by the dev save log so recovery state can be
// confirmed live (tail the log) without restarting.
func summarizeRecoverySnapshot(snap RecoverySnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tabs=%d", len(snap.Tabs))
	for _, t := range snap.Tabs {
		for _, p := range t.Panes {
			ai := "-"
			if p.AI != nil && p.AI.SessionID != "" {
				ai = p.AI.SessionID
			}
			fmt.Fprintf(&b, " [type=%s ai=%s cwd=%s]", p.SessionType, ai, p.LastCwd)
		}
	}
	return b.String()
}

// DiscardRecoverySnapshot removes recovery.json. Used by the dialog's
// "discard" / close-X paths.
func (a *App) DiscardRecoverySnapshot() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	a.lastSnapshot = RecoverySnapshot{}
	a.mu.Unlock()
	return a.recoveryStore.Discard()
}

// MarkCleanShutdown is called from OnBeforeClose right before the wails
// runtime tears the window down. It rewrites the latest snapshot with
// CleanShutdown=true so the next launch's dialog can render "last clean
// exit" copy. No-op when nothing has been saved this session.
func (a *App) MarkCleanShutdown() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	snap := a.lastSnapshot
	a.mu.Unlock()
	return a.recoveryStore.MarkCleanShutdown(snap)
}

// GetRecoveryDialogEnabled mirrors appConfig.RecoveryDialogEnabledOrDefault
// for the frontend Settings → General toggle.
func (a *App) GetRecoveryDialogEnabled() bool {
	return a.cfgStore.Get().RecoveryDialogEnabledOrDefault()
}

// SetRecoveryDialogEnabled persists the user's choice. true re-enables the
// startup recovery dialog; false skips it.
func (a *App) SetRecoveryDialogEnabled(enabled bool) error {
	cfg := a.cfgStore.Get()
	cfg.RecoveryDialogEnabled = &enabled
	return a.cfgStore.Set(cfg)
}
