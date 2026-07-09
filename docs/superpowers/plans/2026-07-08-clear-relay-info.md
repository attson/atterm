# Clear Relay Info Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a "Clear relay info" action in Settings → Relay that wipes every piece of persisted relay state on the desktop — URL, email, session token, keychain password, keychain `account_key` (+ in-memory), auxiliary flags — and stops the uplink, without touching local sessions.

**Architecture:** One new Wails-bound Go verb `App.ClearRelayConfig()` performs all backend side effects atomically. The frontend adds a danger-zone button at the bottom of `SettingsRelay.vue`, guarded by `window.confirm`, that calls the binding and then re-runs the existing config-load path to refresh the form. i18n keys land in both `en` and `zh-CN`.

**Tech Stack:** Go (`desktop/` package, `internal/safekeyring`), Vue 3 + TypeScript (`desktop/frontend/`), Wails v2.12.0 bindings, vitest for the frontend.

**Spec:** `docs/superpowers/specs/2026-07-08-clear-relay-info-design.md`

## Global Constraints

- Preserve every non-`Relay*` field in `appConfig` (`LocalePreference`, `TerminalTheme`, `LocalAdminPassword`, `Plugins`, `QuickTemplates`, `PrefsMeta`, `PrefsSeedMarkers`, `TaskPreset`, …). Only the 9 listed relay fields get zeroed.
- The 9 fields to zero: `RelayURL`, `RelaySessionToken`, `RelaySessionExpiresAt`, `RelayLastEmail`, `RelaySessionUserID`, `AllowInsecureRelay`, `DisableE2EE`, `RemotePermission`, `RelayPaused`.
- Ordering invariant: call `a.setAccountKey(nil)` **before** zeroing the config. `persistAccountKey` short-circuits on empty `RelayURL`; if the order flips, the keychain `account_key` slot is orphaned.
- The keychain slot key is `cfg.RelayURL` verbatim (no `relayHTTPBase` transform) for both password and `account_key`.
- Do not touch pairing peer state.
- Frontend user-facing prose is Chinese; code, commit messages, and comments stay English.

---

## File Structure

| File | New / Modified | Purpose |
|---|---|---|
| `desktop/app.go` | MODIFIED | Add `App.ClearRelayConfig() error` method immediately after `SetRelayConfig`. |
| `desktop/app_clear_relay_test.go` | NEW | 7 unit tests covering field zeroing, keychain deletion (password + account_key), in-memory account_key zeroing, tolerance to missing slots, idempotency, event emission. |
| `desktop/frontend/wailsjs/go/main/App.d.ts` | MODIFIED | Add generated `ClearRelayConfig(): Promise<void>` declaration. |
| `desktop/frontend/wailsjs/go/main/App.js` | MODIFIED | Add generated `ClearRelayConfig` export. |
| `desktop/frontend/src/platform/types.ts` | MODIFIED | Add `ClearRelayConfig(): Promise<void>` to the `Bindings` interface. |
| `desktop/frontend/src/platform/wails.ts` | MODIFIED | Passthrough to `window.go.main.App.ClearRelayConfig()`. |
| `desktop/frontend/src/platform/capacitor.ts` | MODIFIED | Stub throwing `Error("clear-relay-not-implemented")`. |
| `desktop/frontend/src/lib/api.ts` | MODIFIED | Export `clearRelayConfig(): Promise<void>` alongside other relay helpers. |
| `desktop/frontend/src/components/SettingsRelay.vue` | MODIFIED | Extract `reload()` from `onMounted`, add danger-zone section with confirm-guarded button, add `clearing` ref + `handleClear()` handler, add scoped `.danger-zone` / `.danger-btn` styles. |
| `desktop/frontend/src/i18n/messages/en.ts` | MODIFIED | Add 6 `settings.relay.clear*` keys. |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | MODIFIED | Add 6 `settings.relay.clear*` keys. |
| `desktop/frontend/src/components/__tests__/SettingsRelay.test.ts` | MODIFIED | Add 4 test cases: button renders, confirm-cancel is a no-op, confirm-ok clears form + shows notConfigured pill, backend error surfaces via `.error`. |

---

## Task 1: Backend `App.ClearRelayConfig()` + Go tests

**Files:**
- Modify: `desktop/app.go` (append new method after `SetRelayConfig`, around line 546)
- Create: `desktop/app_clear_relay_test.go`

**Interfaces:**
- Consumes: `a.cfgStore.Get() / Set()` (existing), `a.setAccountKey([]byte)` (existing at `desktop/app.go:719`), `a.applyRelayConfig(appConfig)` (existing at `desktop/app.go:365`), `a.emitE2EEModeChanged(bool)` (existing at `desktop/app.go:480`), `a.eventsEmitter(ctx, name, ...data)` (existing field), `clearRelayPasswordFor(url, email string) error` (existing at `desktop/relay_password_store.go:75`), `saveRelayPassword` / `loadRelayPassword` / `saveAccountKey` / `loadAccountKey` (existing helpers used by tests only).
- Produces: `func (a *App) ClearRelayConfig() error` — wails-bound method; parameter-free; returns nil on success, an error only when `a.cfgStore == nil` or `a.cfgStore.Set(cfg)` fails.

- [ ] **Step 1: Write the failing tests**

Create `desktop/app_clear_relay_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

// seededRelayApp returns an App with the cfgStore pre-populated with every
// Relay* field set to a non-zero value plus two unrelated fields (theme,
// shell) that must survive the clear.
func seededRelayApp(t *testing.T) *App {
	t.Helper()
	a := newRelayTestApp(t)
	// Use safekeyring's file-backed test store so keychain calls don't hit
	// the real OS keychain.
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	if err := a.cfgStore.Set(appConfig{
		RelayURL:              "wss://r.example.com",
		RelaySessionToken:     "atk_test",
		RelaySessionExpiresAt: 1_700_000_000,
		RelayLastEmail:        "u@example.com",
		RelaySessionUserID:    "user-abc",
		AllowInsecureRelay:    true,
		DisableE2EE:           true,
		RemotePermission:      "full",
		RelayPaused:           true,
		TerminalTheme:         "solarized",
		DefaultShell:          "/bin/zsh",
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	return a
}

func TestClearRelayConfig_ZerosAllRelayFields(t *testing.T) {
	a := seededRelayApp(t)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "" || cfg.RelaySessionToken != "" || cfg.RelaySessionExpiresAt != 0 ||
		cfg.RelayLastEmail != "" || cfg.RelaySessionUserID != "" ||
		cfg.AllowInsecureRelay || cfg.DisableE2EE || cfg.RemotePermission != "" || cfg.RelayPaused {
		t.Fatalf("relay fields not zeroed: %+v", cfg)
	}
	if cfg.TerminalTheme != "solarized" || cfg.DefaultShell != "/bin/zsh" {
		t.Fatalf("unrelated fields mutated: theme=%q shell=%q", cfg.TerminalTheme, cfg.DefaultShell)
	}
}

func TestClearRelayConfig_DeletesPasswordKeychainSlot(t *testing.T) {
	a := seededRelayApp(t)
	if err := saveRelayPassword("wss://r.example.com", "u@example.com", "hunter2"); err != nil {
		t.Fatalf("saveRelayPassword: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	got, err := loadRelayPassword("wss://r.example.com", "u@example.com")
	if err != nil {
		t.Fatalf("loadRelayPassword: %v", err)
	}
	if got != "" {
		t.Fatalf("password slot not cleared: %q", got)
	}
}

func TestClearRelayConfig_DeletesAccountKeyKeychainSlot(t *testing.T) {
	a := seededRelayApp(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := saveAccountKey("wss://r.example.com", "user-abc", key); err != nil {
		t.Fatalf("saveAccountKey: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	got, err := loadAccountKey("wss://r.example.com", "user-abc")
	if err != nil {
		t.Fatalf("loadAccountKey: %v", err)
	}
	if got != nil {
		t.Fatalf("account_key slot not cleared: got %d bytes", len(got))
	}
}

func TestClearRelayConfig_ZerosInMemoryAccountKey(t *testing.T) {
	a := seededRelayApp(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	a.setAccountKeyInMemory(key)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	if got := a.accountKeySnapshot(); len(got) != 0 {
		t.Fatalf("in-memory account_key not zeroed: %d bytes", len(got))
	}
}

func TestClearRelayConfig_TolerantOfMissingKeychainSlots(t *testing.T) {
	a := seededRelayApp(t)
	// No slot pre-seed; both keychain deletes must silently succeed.
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
}

func TestClearRelayConfig_Idempotent(t *testing.T) {
	a := seededRelayApp(t)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig #1: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig #2 (idempotent): %v", err)
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "" {
		t.Fatalf("second clear mutated URL: %q", cfg.RelayURL)
	}
}

func TestClearRelayConfig_EmitsEvents(t *testing.T) {
	a := seededRelayApp(t)
	var got []string
	a.eventsEmitter = func(_ context.Context, name string, _ ...interface{}) {
		got = append(got, name)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	want := map[string]bool{
		"account-key:changed":  false, // side-effect of setAccountKey(nil)
		"relay-config-changed": false,
		"e2ee-mode-changed":    false,
		"relay:auth-info":      false,
	}
	for _, n := range got {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("event %q not emitted; got %v", n, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./desktop -run TestClearRelayConfig -v`
Expected: FAIL with `a.ClearRelayConfig undefined` (or "has no field or method ClearRelayConfig").

- [ ] **Step 3: Implement `App.ClearRelayConfig()` in `desktop/app.go`**

Find `SetRelayConfig` (around line 492) and append the following method **immediately after its closing brace** (before the `LoginRemoteRelay` comment block around line 550):

```go
// ClearRelayConfig removes every persisted relay identifier from this
// desktop: the 9 Relay* fields on appConfig, the OS-keychain password slot
// (origin+email), and the OS-keychain account_key slot (origin+userID).
// The in-memory account_key is zeroed too, so this desktop stops sealing /
// decrypting frames for the just-forgotten identity. The uplink is stopped
// as part of applyRelayConfig (empty URL takes the "no uplink" branch).
//
// Local terminal sessions, pairing peer records, and every non-relay
// setting are left untouched.
func (a *App) ClearRelayConfig() error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	oldURL := cfg.RelayURL
	oldEmail := cfg.RelayLastEmail

	// Clear the E2EE account_key BEFORE zeroing cfg. setAccountKey(nil)
	// routes through persistAccountKey which reads cfg.RelayURL /
	// cfg.RelaySessionUserID; if we cleared cfg first, persistAccountKey
	// early-returns on the empty URL and the keychain slot is orphaned.
	a.setAccountKey(nil)

	cfg.RelayURL = ""
	cfg.RelaySessionToken = ""
	cfg.RelaySessionExpiresAt = 0
	cfg.RelayLastEmail = ""
	cfg.RelaySessionUserID = ""
	cfg.AllowInsecureRelay = false
	cfg.DisableE2EE = false
	cfg.RemotePermission = ""
	cfg.RelayPaused = false

	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}

	// Best-effort keychain delete for the password slot. clearRelayPasswordFor
	// swallows ErrNotFound; other errors are logged and swallowed because the
	// persisted config is already gone — "cleared with a stray keychain
	// entry" is strictly better than "aborted midway".
	if err := clearRelayPasswordFor(oldURL, oldEmail); err != nil {
		log.Printf("desktop: clear relay password keychain slot: %v", err)
	}

	a.applyRelayConfig(cfg)

	// emitE2EEModeChanged pushes e2ee-mode-changed unconditionally; the
	// existing helper does not skip when the value is already false, which
	// is what we want after a clear (the Settings checkbox needs the sync).
	a.emitE2EEModeChanged(false)

	if a.ctx != nil && a.eventsEmitter != nil {
		a.eventsEmitter(a.ctx, "relay:auth-info", map[string]any{"user_id": ""})
		a.eventsEmitter(a.ctx, "relay-config-changed")
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./desktop -run TestClearRelayConfig -v`
Expected: PASS (7 tests).

If `TestClearRelayConfig_EmitsEvents` fails on `e2ee-mode-changed`: check that `emitE2EEModeChanged` emits unconditionally. If it de-dupes (skips when the same value), replace the `a.emitE2EEModeChanged(false)` call with a direct `a.eventsEmitter(a.ctx, "e2ee-mode-changed", map[string]any{"disabled": false})` matching the payload shape used elsewhere.

- [ ] **Step 5: Verify the wider desktop test suite still passes**

Run: `go test ./desktop`
Expected: PASS (no regression in `TestSetRelayConfig_*`, `TestSetRelayDisableE2EE_*`, `TestSetUplinkPaused_*`, login/register tests).

- [ ] **Step 6: Commit**

```bash
git add desktop/app.go desktop/app_clear_relay_test.go
git commit -m "feat(desktop): App.ClearRelayConfig wipes relay config, keychain slots, uplink"
```

---

## Task 2: Wails binding + frontend platform wiring

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/lib/api.ts`

**Interfaces:**
- Consumes: `App.ClearRelayConfig` (Go side, from Task 1).
- Produces:
  - `Bindings.ClearRelayConfig(): Promise<void>` (platform interface).
  - `clearRelayConfig(): Promise<void>` (top-level `lib/api.ts` wrapper) — used by Task 3.

- [ ] **Step 1: Add the wailsjs generated declaration to `App.d.ts`**

The file is codegen but can be hand-edited to match; a subsequent `wails build` regenerates it identically. Add this line immediately after the existing `export function SetRelayConfig(...)` declaration (around line 148):

```ts
export function ClearRelayConfig():Promise<void>;
```

- [ ] **Step 2: Add the wailsjs runtime wrapper to `App.js`**

In `App.js`, immediately after the existing `SetRelayConfig` function (around line 285), add:

```js
export function ClearRelayConfig() {
  return window['go']['main']['App']['ClearRelayConfig']();
}
```

- [ ] **Step 3: Add the method to the `Bindings` interface in `platform/types.ts`**

Find the block starting with `GetRelayConfig(): Promise<RelayConfig>;` (around line 274). Immediately after `SetRelayConfig(cfg: RelayConfig): Promise<void>;` add:

```ts
ClearRelayConfig(): Promise<void>;
```

- [ ] **Step 4: Add the wails passthrough**

In `desktop/frontend/src/platform/wails.ts`, find the passthrough for `SetRelayConfig` and mirror it. Add near it:

```ts
ClearRelayConfig: (): Promise<void> => (window as any).go.main.App.ClearRelayConfig(),
```

(Adjust to match the file's existing style — some entries use `async` methods, some use arrow properties. Match the surrounding block.)

- [ ] **Step 5: Add the capacitor stub**

In `desktop/frontend/src/platform/capacitor.ts`, add a stub inside the `bindings()`-returning object matching the interface. Locate `SetRelayConfig` and add nearby:

```ts
async ClearRelayConfig(): Promise<void> {
  throw new Error('clear-relay-not-implemented')
},
```

Rationale: unreachable in normal use (SettingsRelay.vue is only mounted from desktop's `App.vue`, not from `MobileApp.vue`), but the interface requires it.

- [ ] **Step 6: Add the top-level wrapper in `lib/api.ts`**

Find the existing `setRelayConfig` wrapper (around line 445). Immediately after it, add:

```ts
export function clearRelayConfig(): Promise<void> {
  return bindings().ClearRelayConfig();
}
```

- [ ] **Step 7: TypeScript check passes**

Run: `cd desktop/frontend && npx tsc --noEmit`
Expected: no errors related to `ClearRelayConfig` or `clearRelayConfig`. (Pre-existing errors unrelated to this work are OK — note them for the review.)

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/wails.ts \
        desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/lib/api.ts
git commit -m "feat(desktop): wire ClearRelayConfig through wailsjs + platform bindings"
```

---

## Task 3: Settings UI, i18n, and frontend tests

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/__tests__/SettingsRelay.test.ts`

**Interfaces:**
- Consumes: `clearRelayConfig()` from `lib/api.ts` (Task 2), the existing platform `usePlatform().events`, the existing `useI18n().t` helper.
- Produces: user-visible "Clear relay info" behavior in Settings → Relay.

- [ ] **Step 1: Add the 6 i18n keys to `en.ts`**

Open `desktop/frontend/src/i18n/messages/en.ts`, find the `settings.relay.*` block (search for `saveConnect:` — a nearby existing key). Add these 6 entries inside the same object:

```ts
clearTitle: 'Clear relay info',
clearHint: 'Removes the saved relay address, account, and login info from this device. Open terminals are not affected.',
clearAction: 'Clear',
clearing: 'Clearing…',
clearConfirm: 'Clear all relay info saved on this device?',
clearFailed: 'Clear failed: {reason}',
```

- [ ] **Step 2: Add the same 6 keys to `zh-CN.ts`**

Open `desktop/frontend/src/i18n/messages/zh-CN.ts`, find the matching `settings.relay.*` block, add:

```ts
clearTitle: '清理 relay 信息',
clearHint: '这将清除本机保存的 relay 地址、账号和登录信息。已经打开的本地终端不受影响。',
clearAction: '清理',
clearing: '清理中…',
clearConfirm: '确定要清除本机保存的 relay 信息吗？',
clearFailed: '清理失败：{reason}',
```

- [ ] **Step 3: Write the failing frontend tests**

Open `desktop/frontend/src/components/__tests__/SettingsRelay.test.ts`. Append this describe block at the end of the file:

```ts
describe('SettingsRelay clear relay info', () => {
  let confirmSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    confirmSpy.mockRestore()
  })

  it('renders the clear button in the danger zone', async () => {
    const w = mount(SettingsRelay)
    await flushPromises()
    const btn = w.find('.danger-btn')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('settings.relay.clearAction')
  })

  it('cancelling the confirm does not call clearRelayConfig', async () => {
    confirmSpy.mockReturnValue(false)
    const clearSpy = vi.spyOn(api, 'clearRelayConfig').mockResolvedValue()
    const w = mount(SettingsRelay)
    await flushPromises()
    await w.find('.danger-btn').trigger('click')
    await flushPromises()
    expect(clearSpy).not.toHaveBeenCalled()
  })

  it('confirming calls clearRelayConfig then reloads to a not-configured state', async () => {
    const clearSpy = vi.spyOn(api, 'clearRelayConfig').mockResolvedValue()
    // First getRelayConfig (onMounted) returns the seeded config; the second
    // call — after clear — returns an empty one so the form goes blank.
    const empty = {
      url: '', token: '', session_expires_at: 0,
      allow_insecure_relay: false, disable_e2ee: false,
      remote_permission: 'full' as const, last_email: '', connected: false,
    }
    ;(api.getRelayConfig as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(baseRelayConfig() as never)
      .mockResolvedValueOnce(empty as never)
    const w = mount(SettingsRelay)
    await flushPromises()
    await w.find('.danger-btn').trigger('click')
    await flushPromises()
    expect(clearSpy).toHaveBeenCalledTimes(1)
    const host = w.find('#relay-host').element as HTMLInputElement
    expect(host.value).toBe('')
    const email = w.find('#relay-email').element as HTMLInputElement
    expect(email.value).toBe('')
    expect(w.text()).toContain('settings.relay.notConfigured')
    expect(w.find('.error').exists()).toBe(false)
  })

  it('surfaces a backend error via .error', async () => {
    vi.spyOn(api, 'clearRelayConfig').mockRejectedValue(new Error('boom'))
    const w = mount(SettingsRelay)
    await flushPromises()
    await w.find('.danger-btn').trigger('click')
    await flushPromises()
    const err = w.find('.error')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('settings.relay.clearFailed')
  })
})
```

Note: the fake `t()` in the top-of-file `vi.mock('../../i18n/useI18n', ...)` returns the key verbatim, which is why we assert against key strings (e.g. `settings.relay.clearAction`) rather than translated copy.

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsRelay.test.ts`
Expected: the new `SettingsRelay clear relay info` describe block fails — most likely `Cannot find .danger-btn` on the first test, then subsequent tests fail on the same missing button.

- [ ] **Step 5: Extract `reload()` in `SettingsRelay.vue`**

Open `desktop/frontend/src/components/SettingsRelay.vue`. Lines 131–197 currently contain an `onMounted(async () => { ... })` block whose body loads the config, prefills email/password, and calls `fetchRelayMe`. Refactor:

Replace:
```ts
onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    // ... existing body through the fetchRelayMe try/catch ...
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }

  platform.events.on('relay:auth-info', async (data) => {
    // ...
  });

  platform.events.on('e2ee-mode-changed', (data) => {
    // ...
  });
});
```

with:
```ts
async function reload() {
  loading.value = true;
  try {
    const cfg = await getRelayConfig();
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    host.value = stripScheme(cfg.url);
    token.value = cfg.token;
    disableE2EE.value = (cfg as any).disable_e2ee ?? false;
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();

    email.value = cfg.last_email ?? "";
    if (email.value) {
      try {
        password.value = await loadSavedRelayPassword();
      } catch {
        // Treat any binding failure as "no stored password".
      }
    } else {
      password.value = "";
    }

    connectedUserID.value = "";
    connectedEmail.value = "";
    if (cfg.token) {
      try {
        const me = await fetchRelayMe();
        connectedEmail.value = me.email || "";
        connectedUserID.value = me.user_id || "";
      } catch {
        // Token rejected or network error — pill stays empty.
      }
    }
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  await reload();

  platform.events.on('relay:auth-info', async (data) => {
    const { user_id } = data as { user_id: string };
    connectedUserID.value = user_id || "";
    if (!user_id) {
      connectedEmail.value = "";
      return;
    }
    try {
      const me = await fetchRelayMe();
      connectedEmail.value = me.email || "";
    } catch {
      // Ignore; status row falls back to showing the short user_id.
    }
  });

  platform.events.on('e2ee-mode-changed', (data) => {
    const next = (data as { disabled?: boolean })?.disabled;
    if (typeof next === 'boolean') {
      disableE2EE.value = next;
    }
  });
});
```

Notable adjustment: the extracted `reload()` also clears `password.value` when `email` is empty and clears `connectedUserID` / `connectedEmail` up-front, so the second call (after Clear) actually blanks the previous state instead of leaving it stale. The `relay:auth-info` handler also treats an empty `user_id` as "reset the pill" (Clear will emit this).

- [ ] **Step 6: Add `clearRelayConfig` import and clearing state to the `<script>` block**

At the top of `<script setup>` (currently importing `getRelayConfig`, `setRelayConfig`, etc. from `../lib/api`), append `clearRelayConfig` to the import list:

```ts
import { getRelayConfig, setRelayConfig, setRelayDisableE2EE, setUplinkPaused, fetchRelayMe, loginRemoteRelay, registerRemoteRelay, probeRelayVersion, loadSavedRelayPassword, rememberRelayPassword, clearRelayConfig } from "../lib/api";
```

Add the reactive ref alongside the other `ref(...)` declarations (near `saving`):

```ts
const clearing = ref(false);
```

- [ ] **Step 7: Add the `handleClear` function**

Immediately below `handleTogglePaused` (or any comparable handler near the end of the `<script>` block), add:

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

- [ ] **Step 8: Add the danger-zone markup to `<template>`**

Find `<PairingPanel />` (around line 540) and add immediately after it, before the closing `</template>`:

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

- [ ] **Step 9: Add the scoped styles**

Append to `<style scoped>` (near the bottom of the file):

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

- [ ] **Step 10: Run the tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsRelay.test.ts`
Expected: PASS (existing prefill/regression tests + 4 new tests).

If `confirming calls clearRelayConfig then reloads to a not-configured state` fails on `expect(host.value).toBe('')`: check that `reload()` actually re-assigns `host` when the second `getRelayConfig` returns an empty URL (the `stripScheme('')` should give `''`).

If the pill text assertion fails: check that `statusPill` re-computes when the ref values change; the existing `computed(...)` on `statusPill` (line 112) already depends on `fullUrl`, `connectedEmail`, `connectedUserID`, `paused` — after Clear, `fullUrl` becomes empty so the pill lands on `notConfigured`.

- [ ] **Step 11: Run the wider frontend test suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS, or at most the same pre-existing failures that were present before this task (note them but don't fix in this task).

- [ ] **Step 12: Manual smoke test (recommended, not required to gate the commit)**

Run: `make dev` from the repo root.
- Configure a relay in Settings → Relay (or reuse an existing config).
- Click the new "清理" button; hit OK on the confirm.
- Verify: the form fields blank out, the pill flips to "not configured", the "logged in as X" text disappears, no orphan console errors.
- Close and reopen Settings → Relay to confirm the state persisted.

- [ ] **Step 13: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts \
        desktop/frontend/src/components/SettingsRelay.vue \
        desktop/frontend/src/components/__tests__/SettingsRelay.test.ts
git commit -m "feat(desktop): Settings → Relay clear-info danger button + i18n + tests"
```

---

## Post-implementation checklist

- [ ] All three tasks committed as separate commits.
- [ ] `go test ./desktop` passes.
- [ ] `cd desktop/frontend && npx vitest run` passes for SettingsRelay tests.
- [ ] Manual smoke test succeeded (or noted as skipped).
- [ ] Branch is ready to be shipped via `superpowers:finishing-a-development-branch` (open PR to main, squash-merge).
