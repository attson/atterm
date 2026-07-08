# Clear Relay Info — Design

**Date:** 2026-07-08
**Status:** Draft, pending review
**Related work:**
- 2026-06-23 desktop-relay-password-persistence (keychain slot layout)
- 2026-06-15 relay-e2ee (E2EE `account_key` keychain persistence)
- 2026-06-10 relay-settings-auth-state (Settings → Relay layout, status pill)
- 2026-05-12 relay-owner-permissions-admin-config (`RemotePermission` field)

## Summary

Add a "Clear relay info" action in Settings → Relay that wipes every piece of
relay state this desktop has persisted — URL, cached email, session token, the
password in the OS keychain, the E2EE `account_key` in the OS keychain, and
the auxiliary flags (`allow_insecure_relay`, `disable_e2ee`, `relay_paused`,
`remote_permission`) — then stops the uplink without touching any local
terminal session. Semantics: "reset Settings → Relay back to the pristine,
unconfigured state".

The action is a single new Wails-bound backend method `ClearRelayConfig()` plus
a danger-zone button at the bottom of `SettingsRelay.vue`. A native
`window.confirm` guards the click. Local ptys and non-relay settings are
untouched.

## Goals & non-goals

### Goals

- One click removes all persisted relay identifiers so the next Settings open
  looks exactly like a fresh install's relay tab.
- The OS keychain slots for the currently-configured `(origin, email)`
  password and `(origin, userID)` `account_key` are both deleted, not
  orphaned.
- The in-memory `account_key` is zeroed so the running desktop can no longer
  decrypt frames from other desktops on the same account.
- The live uplink is stopped as part of the action; no restart required.
- The action is atomic from the user's perspective: either every piece is
  cleared and the UI reflects "not configured", or an error is surfaced and
  nothing changes.
- Local terminal sessions, pairing peer records, and unrelated settings
  (theme, shell, plugins, tasks, notifications, etc.) are unaffected.

### Non-goals

- No sign-out RPC against the relay server. The token is discarded locally;
  the relay's session table cleans itself up on expiry. Adding an
  `/api/auth/logout` roundtrip is a separate spec.
- No changes to `PairingPanel`'s persisted peer state. The user explicitly
  chose to keep pairing untouched.
- No changes to the E2EE `account_key` **derivation**. Clear wipes the
  current-session key from RAM and the keychain slot; the next successful
  login re-derives from the user's password via the existing OPAQUE flow.
- No mobile / Capacitor implementation in the first cut. Mobile renders
  `MobileApp.vue`, which does not include `SettingsRelay.vue`, so no new
  Capacitor code path is needed. iOS uses the in-process mini-relay, so
  "clear relay" has a different meaning there and will land as a follow-up
  spec if ever needed.

## Architecture

Single new backend verb; the frontend does UI + confirmation + reload.

```
SettingsRelay.vue
  └─ Danger zone: [ 清理 relay 信息 ]
       └─ window.confirm(t('settings.relay.clearConfirm'))
            └─ clearRelayConfig()               (lib/api.ts)
                 └─ App.ClearRelayConfig()      (desktop/app.go)
                      ├─ snapshot (oldURL, oldEmail, oldUserID)
                      ├─ a.setAccountKey(nil)   (in-memory + keychain delete)
                      ├─ mutate & save appConfig (9 Relay* fields → zero)
                      ├─ clearRelayPasswordFor(oldURL, oldEmail)  (keychain)
                      ├─ a.applyRelayConfig(cfg)                     (uplink stop)
                      └─ emit "relay-config-changed",
                             "e2ee-mode-changed{disabled:false}",
                             "relay:auth-info{user_id:''}"
       └─ (on success) reload() — reuse the onMounted load path
```

Alignment with existing verbs (`SetRelayConfig`, `SetRelayDisableE2EE`,
`SetUplinkPaused`, `LoginRemoteRelay`): each cross-field action is its own
Wails-bound method with a specific verb. `ClearRelayConfig` follows the same
pattern instead of overloading `SetRelayConfig` with an "empty URL means
clear" special case.

## Backend

### `App.ClearRelayConfig() error` — in `desktop/app.go`

Placement: immediately below `SetRelayConfig`, so reviewers see the pair
together. Follows the same locking discipline as `SetRelayConfig` (that
method does not take an app-level mutex; the underlying `cfgStore` is
concurrent-safe via its own `Get` / `Set` methods, and the keychain calls do
their own syscalls without holding any Go lock).

Steps:

1. Guard: if `a.cfgStore == nil`, return `fmt.Errorf("config store not
   ready")` (mirrors `SetRelayConfig`).
2. `cfg := a.cfgStore.Get()`.
3. Snapshot for keychain cleanup **before** mutating:
   - `oldURL := cfg.RelayURL` — the keychain key used at write time is the
     stored URL verbatim (see `saveRelayPassword(wsURL, email, password)` at
     `app.go:623/705/877` and `saveAccountKey(cfg.RelayURL, userID, key)` at
     `app.go:534/767`); no `relayHTTPBase` conversion is applied.
   - `oldEmail := cfg.RelayLastEmail`.
   - `oldUserID := cfg.RelaySessionUserID` — needed for the `account_key`
     slot lookup.
4. **Clear the E2EE `account_key` first, while cfg still points at the old
   `(URL, userID)` pair**: call `a.setAccountKey(nil)`. That helper zeros the
   in-memory slice, emits `account-key:changed`, and calls
   `saveAccountKey(cfg.RelayURL, cfg.RelaySessionUserID, nil)` which
   short-circuits to `clearAccountKeyFor` for the empty-key case. Doing this
   *before* the URL/userID zeroing is deliberate — if we cleared cfg first,
   `persistAccountKey` would early-return on `RelayURL == ""` and leave the
   keychain entry orphaned.
5. Zero the 9 relay fields on `cfg`:
   ```
   RelayURL              = ""
   RelaySessionToken     = ""
   RelaySessionExpiresAt = 0
   RelayLastEmail        = ""
   RelaySessionUserID    = ""
   AllowInsecureRelay    = false
   DisableE2EE           = false
   RemotePermission      = ""
   RelayPaused           = false
   ```
   Every other field (`LocalePreference`, `TerminalTheme`, `LocalAdminPassword`,
   `Plugins`, `QuickTemplates`, `PrefsMeta`, `PrefsSeedMarkers`, `TaskPreset`,
   …) is left untouched.
6. `a.cfgStore.Set(cfg)`. On error, return it — the in-memory `account_key`
   is already gone and the keychain `account_key` slot is already deleted,
   but the persisted config is unchanged. That state is recoverable (next
   login re-derives the key) and does not leak credentials, so returning the
   error to the UI is safe.
7. Best-effort keychain delete: `clearRelayPasswordFor(oldURL, oldEmail)`.
   The helper already treats `ErrNotFound` as success. Any other error is
   logged via `log.Printf("desktop: ...")` (matching the tone of the
   `persistAccountKey` failure log a few functions above) and swallowed — the
   persisted config is already gone, so "cleared with a stray keychain
   entry" is strictly better than "aborted midway".
8. `a.applyRelayConfig(cfg)` — existing method; with empty URL it stops the
   uplink cleanly.
9. Emit three events on the platform bus (matching what `SetRelayConfig` /
   `SetRelayDisableE2EE` already emit):
   - `relay-config-changed` — the umbrella event other components listen for.
   - `e2ee-mode-changed { disabled: false }` — resets any TitleBar warning
     chip and syncs the checkbox if Settings is already open. Emitted via the
     existing `a.emitE2EEModeChanged(false)` helper (see `SetRelayConfig`).
   - `relay:auth-info { user_id: "" }` — clears the "connected as X" pill.
     Emitted through the same `a.eventsEmitter` path that
     `emitAccountKeyChanged` uses.
10. Return `nil`.

**Contract**: idempotent. Calling it twice in a row is safe: the second call
finds all fields already empty; `a.setAccountKey(nil)` on an already-empty
in-memory slot is a no-op, `saveAccountKey("", "", nil)` short-circuits on
empty account, and `clearRelayPasswordFor("", "")` short-circuits too.

### Wails binding

`desktop/app.go` exports the method; wails-cli's generator picks it up
automatically. The generated `desktop/frontend/wailsjs/go/main/App.d.ts` will
gain a `ClearRelayConfig(): Promise<void>` entry. Nothing else on the Go side
needs to change.

### No new keychain code

`clearRelayPasswordFor` (in `desktop/relay_password_store.go`) and
`clearAccountKeyFor` (in `desktop/account_key_store.go`, invoked implicitly
via `setAccountKey(nil) → saveAccountKey(url, uid, nil)`) already exist and
handle the exact deletions we need. This spec is intentional about not adding
a "clear all passwords/keys for all origins" mode: we only know the
currently-configured `(origin, email)` and `(origin, userID)` pairs, and
clearing others silently would be surprising.

## Frontend

### `lib/api.ts` binding

Add one line adjacent to the existing relay bindings:

```ts
export function clearRelayConfig(): Promise<void> {
  return bindings().ClearRelayConfig();
}
```

### `platform/types.ts` interface

Extend the `Bindings` interface with:

```ts
ClearRelayConfig(): Promise<void>;
```

### `platform/wails.ts`

Add a passthrough that forwards to `window.go.main.App.ClearRelayConfig()`.

### `platform/capacitor.ts`

Add a stub that throws `Error("clear-relay-not-implemented")`. Rationale:
Capacitor's `bindings()` interface must stay type-compatible with `wails.ts`,
but `SettingsRelay.vue` is only mounted from `App.vue` (desktop entry), not
from `MobileApp.vue` (Capacitor entry), so this stub is unreachable in
normal use. Keeping a stub avoids `never`-typed interfaces and preserves the
"add binding here too" reminder for future contributors.

### `SettingsRelay.vue`

Two extractions and one new section:

1. **Extract `reload()`**: pull the body of the current `onMounted` callback
   (lines 131–197: `getRelayConfig` + email prefill + `loadSavedRelayPassword`
   + `fetchRelayMe`) into a standalone `async function reload()`. `onMounted`
   becomes `reload(); platform.events.on(...)`. This gives Clear a single call
   site to re-sync UI state and keeps the two paths identical.

2. **Add reactive state**:
   ```ts
   const clearing = ref(false);
   ```

3. **Add handler**:
   ```ts
   async function handleClear() {
     if (!window.confirm(t('settings.relay.clearConfirm'))) return;
     clearing.value = true;
     error.value = "";
     try {
       await clearRelayConfig();
       await reload();
       snapshotPersisted();
       emit("relay-config-changed");
     } catch (e: any) {
       error.value = t('settings.relay.clearFailed', {
         reason: e?.message ?? String(e),
       });
     } finally {
       clearing.value = false;
     }
   }
   ```

4. **Template**: below `<PairingPanel />`, add:
   ```html
   <div class="danger-zone">
     <div class="danger-zone-text">
       <div class="field-label">{{ t('settings.relay.clearTitle') }}</div>
       <p class="hint">{{ t('settings.relay.clearHint') }}</p>
     </div>
     <button
       type="button"
       class="danger-btn"
       :disabled="saving || clearing || loading"
       @click="handleClear"
     >
       {{ clearing ? t('settings.relay.clearing') : t('settings.relay.clearAction') }}
     </button>
   </div>
   ```

5. **Styles** (scoped, appended to `<style scoped>`):
   ```css
   .danger-zone {
     display: flex;
     align-items: flex-start;
     justify-content: space-between;
     gap: 12px;
     margin-top: 8px;
     padding-top: 12px;
     border-top: 1px solid var(--border);
   }
   .danger-zone-text { flex: 1; min-width: 0; }
   .danger-btn {
     align-self: flex-start;
     background: transparent;
     color: var(--bad);
     border: 1px solid var(--bad);
     border-radius: 6px;
     padding: 6px 12px;
     font-size: 13px;
     cursor: pointer;
   }
   .danger-btn:hover:not(:disabled) {
     background: color-mix(in srgb, var(--bad) 8%, transparent 92%);
   }
   .danger-btn:disabled { opacity: 0.5; cursor: not-allowed; }
   ```

### i18n keys

Add to both `desktop/frontend/src/i18n/messages/en.ts` and
`desktop/frontend/src/i18n/messages/zh-CN.ts`, under the existing
`settings.relay.*` namespace:

| key | en | zh-CN |
|---|---|---|
| `clearTitle` | `Clear relay info` | `清理 relay 信息` |
| `clearHint` | `Removes the saved relay address, account, and login info from this device. Open terminals are not affected.` | `这将清除本机保存的 relay 地址、账号和登录信息。已经打开的本地终端不受影响。` |
| `clearAction` | `Clear` | `清理` |
| `clearing` | `Clearing…` | `清理中…` |
| `clearConfirm` | `Clear all relay info saved on this device?` | `确定要清除本机保存的 relay 信息吗？` |
| `clearFailed` | `Clear failed: {reason}` | `清理失败：{reason}` |

## Data flow — end-to-end

Happy path, user with URL + email + saved password + saved `account_key` +
active uplink:

1. Click → `window.confirm` → OK.
2. `clearing = true`, error cleared.
3. `ClearRelayConfig()` roundtrip.
4. Backend: in-memory `account_key` cleared and its keychain slot deleted;
   config saved with 9 fields zeroed; keychain slot for `(oldURL,
   oldEmail)` password deleted; `applyRelayConfig` stops the uplink; three
   events fire.
5. Frontend receives the promise resolve → `reload()`:
   - `getRelayConfig` returns empty strings / false — form fields become empty,
     `disableE2EE = false`, `paused = false`.
   - `email` is empty → `loadSavedRelayPassword` is skipped by the existing
     guard (`if (email.value)`).
   - `cfg.token` is empty → `fetchRelayMe` is skipped by the existing guard.
   - `connectedEmail`, `connectedUserID` stay empty.
6. `snapshotPersisted()` zeros the dirty comparison so closing Settings
   doesn't pop "unsaved changes".
7. Status pill flips to "not configured" via the existing `statusPill`
   computed.

Failure paths:

- **`cfgStore.Set` fails**: backend returns the error to the frontend. By this
  point the in-memory `account_key` has already been zeroed and its keychain
  slot removed. Persisted config is unchanged. Frontend catches, renders the
  localized `clearFailed` message. This is degraded but safe — the user's
  relay identity on disk is intact, and the missing `account_key` will be
  re-derived on next login. We do not attempt to roll back the account_key
  clear, since that would require re-persisting a key the user just asked us
  to forget.
- **Keychain password delete fails after config save**: backend logs a
  warning and returns nil. UI shows success. The stray keychain password
  entry is invisible to the user and will be overwritten the next time they
  log in with the same `(origin, email)` pair.
- **`applyRelayConfig` fails**: `applyRelayConfig` today returns void and is
  called from `SetRelayConfig` without check; we match that behavior. If the
  uplink shutdown misbehaves, the persisted config is still cleared, and the
  user's next restart resolves it.

## Testing

### Backend

New file `desktop/app_clear_relay_test.go`:

- `TestClearRelayConfig_ZerosAllRelayFields`: seed a config with all 9 fields
  set to non-zero values plus a couple unrelated fields (theme, shell). Call
  `ClearRelayConfig`. Reload config from disk; assert the 9 relay fields are
  zero-value and the unrelated fields survive.
- `TestClearRelayConfig_DeletesPasswordKeychainSlot`: seed a
  `(origin, email)` slot via `saveRelayPassword`, seed a matching config.
  Call `ClearRelayConfig`. Assert `loadRelayPassword(oldURL, oldEmail)`
  returns "" without error.
- `TestClearRelayConfig_DeletesAccountKeyKeychainSlot`: seed a
  `(origin, userID)` account_key slot via `saveAccountKey`, seed a matching
  config. Call `ClearRelayConfig`. Assert `loadAccountKey(oldURL,
  oldUserID)` returns nil bytes without error.
- `TestClearRelayConfig_ZerosInMemoryAccountKey`: seed via
  `a.setAccountKeyInMemory(nonNilKey)`. Call `ClearRelayConfig`. Assert
  `a.accountKeySnapshot()` returns empty.
- `TestClearRelayConfig_TolerantOfMissingKeychainSlots`: no keychain
  pre-seeding. Assert `ClearRelayConfig` returns nil.
- `TestClearRelayConfig_Idempotent`: call twice back to back; second call
  returns nil and does not error on empty `(origin, email, userID)`.
- `TestClearRelayConfig_EmitsEvents`: use the same `eventsEmitter` fake that
  `app_e2ee_toggle_test.go` uses; assert `relay-config-changed`,
  `e2ee-mode-changed{disabled:false}`, `relay:auth-info{user_id:""}`, and
  `account-key:changed` (side-effect of `setAccountKey(nil)`) all fired.

### Frontend

Extend `desktop/frontend/src/components/__tests__/SettingsRelay.test.ts`:

- `renders the danger-zone button`: mount the component; assert the button is
  present.
- `cancel on confirm does not call clearRelayConfig`: stub
  `window.confirm` to return false; click; assert the api spy has zero calls.
- `confirm calls clearRelayConfig and clears the form`: stub `confirm=true`
  and `getRelayConfig` to return an empty config on the reload roundtrip;
  assert form fields become empty, `statusPill` renders `notConfigured`, and
  `error` stays empty.
- `surface backend error`: make `clearRelayConfig` reject with
  `Error('boom')`; assert `.error` renders `Clear failed: boom` (or its
  zh-CN equivalent depending on the i18n harness's default locale).

Use the existing test harness in `SettingsRelay.test.ts` (already mocks the
Wails bindings module and platform layer).

## Risks & mitigations

- **Race with an in-flight `LoginRemoteRelay`**: if the user is in the middle
  of a save and clicks Clear, the `saving` disable already blocks Clear.
  `clearing` mirrors it the other way. The buttons never overlap.
- **Keychain permission prompt on macOS**: `safekeyring.Delete` may prompt.
  This matches the write path's existing behavior; users who denied writes
  won't have data to delete anyway.
- **`RelaySessionUserID` used as a key for `PrefsSeedMarkers`**: seed markers
  are keyed by user id in a map that lives on `appConfig`. Clearing the user
  id does not touch that map, so the next login (same or different user)
  correctly gets a "first login" seed marker check against the new id. Verified
  by re-reading `config.go` around the `PrefsSeedMarkers` field.
- **Diagnostics may reference cleared values**: `diagnostics.ts` reads relay
  URL / token-redacted from `GetDesktopStatus`. Those are computed from the
  live config, so they'll simply show empty after clear — no code change
  required.
