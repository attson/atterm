package main

import (
	"encoding/base64"
	"log"
)

// setAccountKey stores key as the current in-memory account_key AND persists
// it under the currently-configured (relay URL, user id). Used by callers
// whose config already reflects the right identity (e.g. logout clearing with
// nil). Login/register instead split the two steps — setAccountKeyInMemory
// early (so the uplink seals on its first announce) then persistAccountKey
// after the new URL + user id are committed — because persisting against the
// stale identity would write the keychain entry under the wrong (or empty)
// account name and lose it on the next launch.
func (a *App) setAccountKey(key []byte) {
	a.setAccountKeyInMemory(key)
	a.persistAccountKey(key)
}

// setAccountKeyInMemory updates the in-memory account_key and notifies the
// frontend WITHOUT touching the keychain. Concurrent callers see the most
// recent value via accountKeySnapshot.
func (a *App) setAccountKeyInMemory(key []byte) {
	a.accountKeyMu.Lock()
	if len(key) == 0 {
		a.accountKey = nil
	} else {
		a.accountKey = append([]byte(nil), key...)
	}
	a.accountKeyMu.Unlock()
	a.emitAccountKeyChanged()
}

// emitAccountKeyChanged notifies the frontend so the platform-layer
// cache (wails.ts setAccountKeyProvider) refreshes. Routed through
// the injectable a.eventsEmitter so unit tests that wire a plain
// context.Background() do not crash on wailsruntime's strict context
// check.
func (a *App) emitAccountKeyChanged() {
	if a.ctx == nil {
		return
	}
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "account-key:changed")
}

// persistAccountKey writes (or clears) the account_key for the currently
// configured (relay URL, user ID). Failures are logged but never
// returned — losing the persistence is a UX regression (requires
// re-login on app restart) but not a correctness one (in-memory key is
// still set / cleared). Callers MUST hold accountKeyMu released so the
// keychain syscall doesn't lengthen the critical section.
func (a *App) persistAccountKey(key []byte) {
	if a.cfgStore == nil {
		return
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionUserID == "" {
		return
	}
	if err := saveAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID, key); err != nil {
		log.Printf("desktop: persist account_key failed: %v", err)
	}
}

// accountKeySnapshot returns a defensive copy of the current account_key
// (or nil if unlocked). The uplink consumes this to derive per-session
// frame keys once frame-level encryption ships in M2.
func (a *App) accountKeySnapshot() []byte {
	a.accountKeyMu.Lock()
	defer a.accountKeyMu.Unlock()
	if len(a.accountKey) == 0 {
		return nil
	}
	return append([]byte(nil), a.accountKey...)
}

// agentSealAccountKey is the closure handed to newUplink. It returns
// the live account_key for seal operations EXCEPT when the user has
// flipped the per-desktop DisableE2EE toggle — in that case it returns
// nil, which is the existing "no key = no encryption" code path. Every
// seal site in uplink.go / uplink_seal_*.go falls through to plaintext
// automatically without any additional branching. Hot-toggleable: the
// closure consults the latest cfg on every call, so flipping the flag
// in Settings takes effect on the next frame without restart.
//
// Distinct from accountKeySnapshot, which is also used by the JS-side
// GetAccountKey binding for decrypting frames originating from OTHER
// desktops. The toggle only suppresses THIS desktop's sealing; cross-
// desktop decrypt keeps working so a paused-encryption desktop still
// reads its other devices' sealed sessions correctly.
func (a *App) agentSealAccountKey() []byte {
	if a.cfgStore != nil && a.cfgStore.Get().DisableE2EE {
		return nil
	}
	return a.accountKeySnapshot()
}

// HasAccountKey reports whether an account_key is currently unlocked in
// memory. The frontend uses this to decide whether to surface a "unlock"
// prompt vs assume the user just needs to re-authenticate.
func (a *App) HasAccountKey() bool {
	return len(a.accountKeySnapshot()) > 0
}

// GetAccountKey returns the unlocked E2EE account_key as a standard
// base64 string, or the empty string when no key is available (user
// not logged in, bootstrap-admin path, etc.).
//
// Threat model: this binding sits entirely inside the desktop's own
// process boundary — the JS side runs in the same OS user's Wails
// host. Exposing the key to JS lets the connection layer decrypt
// MetaPayload.Sealed / SessionInfo.Sealed in the WebSocket hot path
// without an async round-trip per frame. The same key would have
// been derivable by anything running in this process anyway (via
// Wails' own ipc binding mechanism), so the surface area does not
// change.
//
// Note: do NOT log the return value. Do NOT persist it. The Go side
// already has a Keychain copy under M1f; this binding is for the JS
// runtime to cache once at platform-init and discard on logout.
func (a *App) GetAccountKey() string {
	key := a.accountKeySnapshot()
	if len(key) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(key)
}
