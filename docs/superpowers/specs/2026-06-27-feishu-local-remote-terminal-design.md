# Feishu Local-Mode Remote Terminal — Design

**Date:** 2026-06-27
**Status:** Draft, pending review
**Related work:**
- 2026-06-26 feishu-as-terminal (anchor card / autoAttach foundation)
- 2026-06-27 feishu-mode-override (explicit local/relay mode preference)
- 2026-06-17 feishu-app-integration

## Summary

The "enable Feishu remote terminal" toggle and the anchor-card auto-attach guard
both read and write the **relay** binding stored in `sqliteStore`. In local mode
the binding lives in the OS keychain (`LocalKeychainBindingStore`), so the
sqlite `feishu_bindings` table has no row for the user. The result, observed in
the UI:

- Saving the toggle issues `UPDATE ... WHERE user_id = ?` which affects 0 rows →
  returns `ErrFeishuBindingNotFound` → the frontend rolls the checkbox back. The
  toggle **cannot be saved** in local mode.
- The anchor-card guard (`relay_host.go` `attachFeishuSubscriberForAutoAttach`)
  fails its `GetFeishuBinding` lookup and silently returns → anchor cards
  **never fire** in local mode.

The send path itself works in local mode (the dispatcher is injected by `app.go`
in both modes), so this is purely a "settings/state read the wrong store"
problem. This spec routes the remote-terminal settings through the binding-store
abstraction so each mode reads/writes its own store, and decouples the anchor
guard from the concrete `sqliteStore` by having `app.go` inject the state by
effective mode.

## Goals & non-goals

### Goals

- Persist the remote-terminal toggle + autoAttach mode in local mode (keychain).
- Make anchor cards fire in local mode under the same guards as relay mode.
- Keep relay mode behavior unchanged.

### Non-goals (this spec)

- No change to how anchor cards render or stream (Shell/AI chunker untouched).
- No new UI — the existing toggle + autoAttach dropdown drive both modes.
- No bulk re-attach semantics change (`OnRemoteTerminalToggle` is reused as-is).

## Architecture

Three layers, matching the binding-store split that already separates local and
relay everywhere else in the Feishu integration.

### 1. Storage: extend the keychain blob

`localBindingBlob` (`desktop/feishu/binding_store_local.go`) gains two fields:

```go
RemoteTerminalEnabled bool   `json:"remote_terminal_enabled,omitempty"`
SessionAutoAttach     string `json:"session_auto_attach,omitempty"`
```

`BindingView` (`desktop/feishu/binding_store.go`) gains the same two fields so
readers see them uniformly.

A new method on the **concrete** `LocalKeychainBindingStore` (not on the
`BindingStore` interface):

```go
func (s *LocalKeychainBindingStore) SetRemoteTerminalSettings(
    ctx context.Context, enabled bool, autoAttach string) error
```

- validates `autoAttach ∈ {ai, all, none}`; reads the current blob (creates an
  empty one if none exists); sets the two fields; writes back.
- does **not** depend on credentials being present — the remote-terminal setting
  is logically independent of whether AppID/AppSecret are filled in.

**Why not on the interface:** the relay store's state-mutating methods
(`SetBound`, `SetDisabled`, …) already return `ErrRelayManagedBoundState`
because relay-side state is managed elsewhere (the embedded sqlite), not through
the store. Remote-terminal settings follow the same split. Adding the method to
the interface would force the relay store to return "unsupported" and `app.go`
to special-case relay anyway — so `app.go` dispatches by mode directly instead:

- **local**: type-assert `svc.Store()` to `*feishu.LocalKeychainBindingStore`
  and call the new method (keychain).
- **relay**: keep current behavior — write/read `a.host.sqliteStore`
  (`SetRemoteTerminalSettings` / `GetFeishuBinding`), unchanged.

`GetFeishuRemoteTerminalSettings` reads the two new fields off the local blob in
local mode, and off `sqliteStore.GetFeishuBinding` in relay mode (unchanged).

### 2. Anchor guard: inject state by mode

`relayHost` currently hard-reads `h.sqliteStore.GetFeishuBinding` at
`attachFeishuSubscriberForAutoAttach`. Replace that with an injected callback:

```go
// set by app.go; returns the remote-terminal gate state for the live mode.
feishuRemoteTermState func(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool)
```

`app.go` installs this callback in `startFeishu` and re-installs it in
`reconcileFeishuMode` on a mode switch, reading from the correct store:

- **local**: read the keychain blob → `(enabled, openID, autoAttach)`.
- **relay**: read the sqlite binding → same triple.

The guard calls the callback; `ok == false` (no binding / not injected yet) →
return without attaching, matching today's "couldn't read binding → skip"
behavior. `relayHost` no longer references `sqliteStore` for this path.

### 3. Toggle side effect: reused unchanged

`OnRemoteTerminalToggle` (`relay_host.go`) operates only on the in-memory
`feishuSubs` map (tears down subscribers + archives anchors on disable). It
touches no store and is mode-agnostic, so it is reused without modification.

## Data flow

**Save (user clicks the toggle)**
```
frontend onRemoteTerminalToggleChange
  → SetFeishuRemoteTerminalSettings(enabled, autoAttach)   [app.go, dispatch by mode]
      ├─ local:  LocalKeychainBindingStore.SetRemoteTerminalSettings(...)
      │            → write keychain blob fields (create blob if absent)
      └─ relay:  a.host.sqliteStore.SetRemoteTerminalSettings(...) (unchanged)
  → if enabled flipped, h.OnRemoteTerminalToggle(enabled)  [mode-agnostic, reused]
```

**Read (open settings page)**
```
GetFeishuRemoteTerminalSettings → svc.Store().Get() new fields
  binding absent → defaults {enabled:false, autoAttach:"ai"} (unchanged)
```

**Anchor trigger (new / AI session)**
```
attachFeishuSubscriberForAutoAttach
  → h.feishuRemoteTermState(ctx)   [callback injected by app.go]
      ├─ local:  keychain blob → (enabled, openID, autoAttach)
      └─ relay:  sqlite binding  → same
  → guard logic unchanged (enabled? openID!=""? autoAttach matches trigger?)
```

## Edge cases

1. **Local blob absent on save**: `SetRemoteTerminalSettings` creates a blob with
   only the two fields. Rationale: the remote-terminal setting must not require
   credentials to exist first.
2. **autoAttach validation**: reuse the `ai|all|none` check; reject invalid
   values in the local store write too.
3. **Callback not yet injected (startup race)**: guard sees `ok == false` and
   returns — no anchor card, identical to today's skip-on-missing-binding.
4. **Mode switch**: `reconcileFeishuMode` re-installs the callback so the guard
   reads the store for the new mode.

## Testing

- **Local store** (`binding_store_local_test.go`): `SetRemoteTerminalSettings`
  persists both fields when a blob exists and when it does not; `Get` reads them
  back; invalid autoAttach rejected.
- **app.go** (`app_feishu_*_test.go`): `Set`/`GetFeishuRemoteTerminalSettings`
  round-trip in local mode via a file-keyring + `ModeLocal` service; relay mode
  regression stays green.
- **Anchor guard**: the local-mode callback returns the correct triple;
  `enabled=false` / `openID=""` suppress attachment.
- **Regression**: existing relay-mode remote-terminal + anchor tests pass.
