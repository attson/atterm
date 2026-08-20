package main

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/attson/atterm/internal/prefssync"
)

// This file is the only place in desktop/ allowed to call a method on
// a.prefsSync. prefssync.Engine's own doc comment says it is "NOT safe for
// concurrent calls — wire it into a serial goroutine via the desktop boot
// code"; before this file existed nothing did. The engine was reachable
// from four independent goroutines: the startup pull, the post-login
// pull/seed/push, every markPrefDirtyAndPush (i.e. every settings setter),
// and the relay watcher's pull. Push in particular is a check-then-act
// across a network round trip its own comment says "can take seconds": it
// records sentAt[key], PUTs, then re-reads the meta and writes value+meta
// only if the stamp is unchanged. Two overlapping pushes can both observe
// an unchanged stamp and both write — change three settings quickly and
// that used to mean three racing PUTs. Every caller now goes through
// enqueueSync or enqueuePostLoginSeed instead of touching a.prefsSync
// directly.

// prefsSyncEngine is the surface this file needs from the cross-device sync
// engine; *prefssync.Engine already satisfies it. It exists so tests can
// substitute a fake without internal/prefssync growing any desktop-only
// test scaffolding — internal/ stays desktop-free, and that package has its
// own reasons (a later task widens Pull's signature) to change on its own
// schedule.
type prefsSyncEngine interface {
	Pull(ctx context.Context) (prefssync.PullResult, error)
	Push(ctx context.Context) error
	MarkDirty(key string, updatedAtLocalMs int64)
	SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64)
}

// syncRequest is one queued unit of work for the serial loop below. Both
// flags may be set — the loop always runs pull before push when both are
// true, so a caller that wants "make sure we're current, then make sure
// we've pushed" gets that ordering for free rather than having to issue two
// requests and hope they land in order.
type syncRequest struct {
	pull bool
	push bool
}

// startPrefsSyncLoop starts the one goroutine that is ever allowed to call
// into a.prefsSync. Call once, immediately after a.prefsSync is constructed
// (see app.go's startup) — enqueueSync and enqueuePostLoginSeed are no-ops
// until this has run.
func (a *App) startPrefsSyncLoop() {
	a.prefsSyncCh = make(chan syncRequest, 1)
	a.prefsSyncTaskCh = make(chan func(prefsSyncEngine), 1)
	a.prefsSyncLoopDone = make(chan struct{})
	go a.runPrefsSyncLoop()
}

// runPrefsSyncLoop serialises every engine call. A channel rather than a
// mutex is deliberate: enqueueSync's coalescing means "is a request
// currently queued" is externally observable (a later status indicator
// reads it as "changes waiting") — a mutex guarding direct calls would hide
// that queue depth instead of exposing it.
func (a *App) runPrefsSyncLoop() {
	// prefsSyncLoopDone exists purely so tests can wait for this goroutine
	// to actually exit instead of guessing a sleep long enough for
	// scheduling; production code never reads it.
	defer close(a.prefsSyncLoopDone)
	for {
		select {
		case <-a.ctx.Done():
			return
		case req := <-a.prefsSyncCh:
			a.setSyncBusy(true)
			a.runSyncRequest(req)
			a.setSyncBusy(false)
		case task := <-a.prefsSyncTaskCh:
			a.setSyncBusy(true)
			a.runSyncTask(task)
			a.setSyncBusy(false)
		}
	}
}

// enqueueSync submits req to the serial loop without blocking the caller.
// If a request is already queued — the loop is still busy with a previous
// one — the two are coalesced by OR-ing their flags in place rather than
// queuing a second: a burst of pref changes arriving while one push is in
// flight collapses into at most one extra round trip after it, not one per
// change. Safe to call before startPrefsSyncLoop has run (a.prefsSyncCh is
// still nil) or after a.ctx has been cancelled (the loop has returned and
// stopped draining the channel) — both degrade to "merge and move on"
// rather than blocking or panicking.
func (a *App) enqueueSync(req syncRequest) {
	if a.prefsSyncCh == nil {
		return
	}
	for {
		select {
		case a.prefsSyncCh <- req:
			return
		default:
		}
		select {
		case pending := <-a.prefsSyncCh:
			// Merge and retry the send above. The buffer has capacity 1, so
			// once drained here the send is expected to succeed on the next
			// spin — except for the benign race below.
			req.pull = req.pull || pending.pull
			req.push = req.push || pending.push
		default:
			// The full check above and this drain are two separate,
			// non-atomic operations: another enqueueSync call (or the loop
			// itself starting work) can drain the buffer in between,
			// leaving nothing here to merge. Loop back to the top and try
			// the direct send again rather than treating this as an error.
		}
	}
}

// dirtyPrefKeys returns the sorted set of keys currently marked dirty in
// local config -- the same set prefssync.Engine.Push is about to collect
// through its own adapter to build the PUT body. Reading it here from
// a.cfgStore, rather than having Push report it, keeps this diagnostic out
// of internal/prefssync entirely (redline #5): the sync package never learns
// desktop cares about logging attribution.
func (a *App) dirtyPrefKeys() []string {
	if a.cfgStore == nil {
		return nil
	}
	meta := a.cfgStore.Get().PrefsMeta
	keys := make([]string, 0, len(meta))
	for key, entry := range meta {
		if entry.Dirty {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// runSyncRequest executes one coalesced request on the loop goroutine. A
// panicking or erroring engine call must not take the loop down — the next
// enqueued request still has to run — so both are contained here instead of
// left to propagate.
func (a *App) runSyncRequest(req syncRequest) {
	engine := a.prefsSync
	if engine == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logWarn("prefssync", "sync loop: recovered panic: %v", r)
		}
	}()
	if req.pull {
		result, err := engine.Pull(a.ctx)
		a.recordSyncOutcome(err)
		if err != nil {
			logWarn("prefssync", "pull: %v", err)
		} else {
			a.emitPrefsChanged()
			a.emitSyncPulledIfNonEmpty(result)
		}
	}
	if req.push {
		// Push batches every currently-dirty key into one PUT (see
		// prefssync.Engine.Push), so by the time a coalesced request reaches
		// here there is no single "the key that changed" left to name the
		// way the old per-goroutine log line in markPrefDirtyAndPush did.
		// Naming the setter that happened to trigger the enqueue would be worse
		// than nothing: the trigger need not be the key the relay rejected, so
		// snapshot the dirty set going into this push instead -- it is exactly
		// the batch Push collects through the adapter, and names every key that
		// could actually have caused the failure.
		// What must not regress is that guarantee's point: a failed push is
		// never silent. An unknown_key/invalid_value 400 poisons every
		// dirty key riding along in the same batch, and staying silent
		// about it is exactly how ssh_hosts_encrypted stayed broken for
		// months after joining syncedKeys — nothing ever surfaced the 400.
		dirty := a.dirtyPrefKeys()
		err := engine.Push(a.ctx)
		a.recordSyncOutcome(err)
		if err != nil {
			logWarn("prefssync", "push failed (dirty: %v): %v", dirty, err)
		} else {
			a.emitPrefsChanged()
		}
	}
}

// runSyncTask runs a compound, multi-call unit of work (see
// enqueuePostLoginSeed) with the same crash containment as runSyncRequest.
func (a *App) runSyncTask(task func(prefsSyncEngine)) {
	engine := a.prefsSync
	if engine == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logWarn("prefssync", "sync loop: recovered panic in task: %v", r)
		}
	}()
	task(engine)
}

// emitPrefsChanged notifies the frontend that synced preferences may have
// changed. Guarded on eventsEmitter alone, the same pattern
// emitSnippetProgress uses — tests substitute a no-op emitter rather than
// requiring a live Wails context.
func (a *App) emitPrefsChanged() {
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "prefs:changed")
}

// markPrefDirtyAndPush stamps the meta for key with the current ms, then
// enqueues a push. MarkDirty stays a synchronous call on this goroutine
// (not deferred into the loop): it is a local write, and updatePref's
// caller relies on the dirty stamp already being in place by the time this
// function returns, before the setter's own return. Only the network side
// — the push — is handed to the serial loop.
//
// The two calls below must stay in this order. enqueueSync hands the push
// off to a different goroutine that can start running it immediately; if
// MarkDirty ran second, that goroutine could call Push before this key's
// dirty stamp exists, and the push already in flight would simply not
// include this edit — silently, since Push doesn't know an edit is coming.
// Swap the lines and nothing here fails to compile; only the loss of the
// edit would give it away later.
//
// A failed push is not fatal to the local write (the key stays dirty and
// the next push retries it), but it must not be silent: an
// unknown_key/invalid_value 400 poisons every subsequent push (every dirty
// key rides along in the same batch and fails with it), and that silence is
// exactly why ssh_hosts_encrypted stayed broken for months after joining
// syncedKeys — nothing ever surfaced the 400. runSyncRequest logs at warn so
// that failure mode stays visible instead of invisible.
func (a *App) markPrefDirtyAndPush(key string) {
	if a.prefsSync == nil {
		return
	}
	a.prefsSync.MarkDirty(key, time.Now().UnixMilli())
	a.enqueueSync(syncRequest{push: true})
}

// enqueuePostLoginSeed submits the post-login pull -> seed -> push sequence
// to the serial loop as one exclusive unit via prefsSyncTaskCh rather than
// prefsSyncCh: unlike an ordinary pull/push, this sequence has to hold the
// engine across all three calls without an unrelated enqueueSync(...) --
// say, a setter firing while the user is still logging in -- interleaving
// its own PUT in the middle of the seed's Push. Called by
// LoginRemoteRelay's post-login step; see app_relay.go. Non-blocking: the
// caller is expected to invoke this with `go`, matching the fire-and-forget
// goroutine this sequence used to run on directly.
func (a *App) enqueuePostLoginSeed() {
	if a.prefsSync == nil || a.prefsSyncTaskCh == nil {
		return
	}
	task := func(engine prefsSyncEngine) {
		result, err := engine.Pull(a.ctx)
		a.recordSyncOutcome(err)
		if err != nil {
			return
		}
		a.emitSyncPulledIfNonEmpty(result)
		cfg := a.cfgStore.Get()
		userID := cfg.RelaySessionUserID
		if userID == "" || cfg.PrefsSeedMarkerFor(userID) {
			a.emitPrefsChanged()
			return
		}
		engine.SeedFromLocal(isPrefCustomized(cfg), time.Now().UnixMilli())
		// Only mark the seed as done when Push actually succeeded. A failed
		// Push leaves the seeded keys dirty locally, so marking the marker
		// here anyway would make the seed permanently un-retryable: next
		// launch would see PrefsSeedMarkerFor(userID)==true, skip
		// SeedFromLocal entirely, and Pull would adopt whatever's on the
		// relay (nothing, if this Push never landed) over the local values
		// that never got a second chance to upload.
		if err := engine.Push(a.ctx); err != nil {
			a.recordSyncOutcome(err)
			logWarn("prefssync", "seed push for user %s: %v", userID, err)
			a.emitPrefsChanged()
			return
		}
		a.recordSyncOutcome(nil)
		cfg2 := a.cfgStore.Get()
		if cfg2.PrefsSeedMarkers == nil {
			cfg2.PrefsSeedMarkers = map[string]bool{}
		}
		cfg2.PrefsSeedMarkers[userID] = true
		_ = a.cfgStore.Set(cfg2)
		a.emitPrefsChanged()
	}
	select {
	case a.prefsSyncTaskCh <- task:
	case <-a.ctx.Done():
	}
}

// SyncStatus is the frontend-facing view of the sync loop's state, returned
// by GetSyncStatus and pushed as the "sync:status" event payload on every
// observable transition. State is one of "idle" | "syncing" | "offline" |
// "error" -- see syncOffline and GetSyncStatus for how those are told apart.
type SyncStatus struct {
	State        string `json:"state"`
	LastSyncedAt int64  `json:"last_synced_at"` // ms, 0 = never
	PendingKeys  int    `json:"pending_keys"`
	LastError    string `json:"last_error,omitempty"`
}

// syncOffline reports whether cross-device sync cannot currently run at
// all -- no relay configured, no session, or the relay is paused. This is
// the exact condition applyRelayPrefsWatch (prefs_watch.go) already uses to
// decide whether to even start the prefs watch; reused verbatim here rather
// than restated so the two can never drift apart. It is deliberately not
// folded into "error": "not configured" is not a failure, and GetSyncStatus
// checks this first so an unconfigured relay never renders as the red
// "error" state, no matter what the last engine call happened to return.
func (a *App) syncOffline(cfg appConfig) bool {
	return cfg.RelayURL == "" || cfg.RelayPaused || cfg.RelaySessionToken == ""
}

// GetSyncStatus reports the current state of cross-device preference sync
// for the frontend. Safe to call from any goroutine (the Wails call
// dispatcher in particular): it only reads a.cfgStore, a.dirtyPrefKeys, and
// the syncStatusMu-guarded fields written by recordSyncOutcome/setSyncBusy
// on the loop goroutine.
func (a *App) GetSyncStatus() SyncStatus {
	var cfg appConfig
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	pending := len(a.dirtyPrefKeys())

	a.syncStatusMu.Lock()
	busy := a.syncBusy
	lastSyncedAt := a.syncLastSyncedAt
	lastError := a.syncLastError
	a.syncStatusMu.Unlock()

	state := "idle"
	switch {
	case a.prefsSync == nil || a.syncOffline(cfg):
		state = "offline"
	case busy:
		state = "syncing"
	case lastError != "":
		state = "error"
	}

	return SyncStatus{
		State:        state,
		LastSyncedAt: lastSyncedAt,
		PendingKeys:  pending,
		LastError:    lastError,
	}
}

// SyncNow enqueues an immediate pull-then-push and returns without waiting
// for it to finish -- the outcome arrives later via the "sync:status" and
// "sync:pulled" events. It only ever errors for "cannot start": no relay
// configured (see syncOffline). A failure of the sync itself (a bad PUT, a
// network error) is not returned here; that is what LastError /
// "sync:status" are for.
func (a *App) SyncNow() error {
	var cfg appConfig
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	if a.prefsSync == nil || a.syncOffline(cfg) {
		return errors.New("sync is offline: no relay configured")
	}
	a.enqueueSync(syncRequest{pull: true, push: true})
	return nil
}

// recordSyncOutcome updates the stored result of one engine call (a single
// Pull or Push, not a whole coalesced request) and emits "sync:status" if
// that changed the externally visible state. err == nil clears any
// previously stored error and stamps LastSyncedAt with now; a non-nil err
// overwrites LastError with its text. Called once per engine call rather
// than once per request so that, within one pull-then-push request, a
// failing push after a successful pull still ends the request in the
// "error" state -- the later call's outcome always wins.
func (a *App) recordSyncOutcome(err error) {
	a.syncStatusMu.Lock()
	if err != nil {
		a.syncLastError = err.Error()
	} else {
		a.syncLastError = ""
		a.syncLastSyncedAt = time.Now().UnixMilli()
	}
	a.syncStatusMu.Unlock()
	a.emitSyncStatusIfChanged()
}

// setSyncBusy marks whether the loop is currently inside a
// runSyncRequest/runSyncTask call (runPrefsSyncLoop toggles this around
// both) and emits "sync:status" if that changed the externally visible
// state. Wrapped around both request kinds, not inside each individually,
// so the post-login pull/seed/push sequence reads as one "syncing" span
// rather than flickering between calls.
func (a *App) setSyncBusy(busy bool) {
	a.syncStatusMu.Lock()
	a.syncBusy = busy
	a.syncStatusMu.Unlock()
	a.emitSyncStatusIfChanged()
}

// emitSyncStatusIfChanged recomputes SyncStatus and emits it over
// "sync:status" only if State differs from the last emitted value --
// recordSyncOutcome and setSyncBusy both call this unconditionally, and
// without the dedup a single request would emit far more than one event per
// actual transition (e.g. a successful pull immediately followed by the
// busy-false transition, both computing to the same "idle").
func (a *App) emitSyncStatusIfChanged() {
	status := a.GetSyncStatus()
	a.syncStatusMu.Lock()
	changed := status.State != a.syncLastEmittedState
	a.syncLastEmittedState = status.State
	a.syncStatusMu.Unlock()
	if changed {
		a.emitSyncStatus(status)
	}
}

// emitSyncStatus pushes status to the frontend. Guarded on eventsEmitter
// alone, the same pattern emitPrefsChanged uses.
func (a *App) emitSyncStatus(status SyncStatus) {
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "sync:status", status)
}

// emitSyncPulledIfNonEmpty pushes result over "sync:pulled" only when Pull
// actually adopted or conflicted on at least one key -- a Pull that touched
// nothing (the common case: nothing changed on another device since the
// last pull) has nothing worth telling the user about.
func (a *App) emitSyncPulledIfNonEmpty(result prefssync.PullResult) {
	if len(result.Adopted) == 0 && len(result.Conflict) == 0 {
		return
	}
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "sync:pulled", result)
}
