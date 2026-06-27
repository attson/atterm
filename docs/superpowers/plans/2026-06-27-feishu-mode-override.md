# Feishu Mode Override Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit per-machine `FeishuModePref` (`auto` / `local` / `relay`) that lets users override the implicit "follow Relay login state" behavior, plus a small UI surface to set it and see the effective mode.

**Architecture:** A single persisted string on `appConfig`, consumed by `feishuServiceConfig` to resolve an effective mode. The `auto` path is taught to treat `RelayPaused=true` (UPLINK off) as "not effectively logged in". A new `SetFeishuModePref` Wails method writes the pref and triggers the existing `reconcileFeishuMode` hot-swap. UI is a three-state dropdown plus an "effective mode" status line.

**Tech Stack:** Go (desktop wails app), Vue 3 + TypeScript (frontend), Wails RPC.

**Source spec:** `docs/superpowers/specs/2026-06-27-feishu-mode-override-design.md` (commit `48f9fa2`).

---

## File map

### Modified
- `desktop/config.go` — add `FeishuModePref string` field to `appConfig` + `FeishuModePrefOrDefault() string` helper.
- `desktop/app.go` — rewrite `feishuServiceConfig` to consult the pref + `RelayPaused`; add `GetFeishuModePref` / `SetFeishuModePref` / `GetFeishuEffectiveMode` Wails methods.
- `desktop/frontend/wailsjs/go/main/App.js` — hand-written stubs for the 3 new methods.
- `desktop/frontend/wailsjs/go/main/App.d.ts` — TS type declarations for the 3 new methods.
- `desktop/frontend/src/lib/api.ts` — extend `AppBindings` interface; add wrappers `getFeishuModePref`, `setFeishuModePref`, `getFeishuEffectiveMode`.
- `desktop/frontend/src/i18n/messages/en.ts` — `settings.feishu.mode.*` keys.
- `desktop/frontend/src/i18n/messages/zh-CN.ts` — same keys in zh-CN.
- `desktop/frontend/src/components/SettingsFeishu.vue` — replace the static "模式: relay" line with a dropdown + effective-mode status + fallback warning.
- `desktop/frontend/src/components/SettingsFeishu.test.ts` — extend to cover mode dropdown.

### Created
- `desktop/app_feishu_mode_test.go` — Go truth-table tests for `feishuServiceConfig` and integration tests for `SetFeishuModePref`.

### Untouched (do not modify)
- `desktop/feishu/service.go` (Service builder stays as-is)
- `desktop/feishu/longconn.go` (no LongConn behavior change here)
- `desktop/feishu/dispatcher.go` (dispatcher unchanged)
- `internal/userstore/feishu_bindings.go` — the pref is NOT stored in `feishu_bindings`

---

## Task 1: appConfig field + default helper

**Files:**
- Modify: `desktop/config.go`
- Test: `desktop/app_feishu_mode_test.go` (new — first test goes here)

- [ ] **Step 1: Find the right insertion point in `appConfig`**

Open `desktop/config.go`. Locate the `appConfig` struct (around line 46). The new field sits alongside `AINotificationsOnly` (around line 119) since both are Feishu-integration prefs.

- [ ] **Step 2: Write the failing test**

Create `desktop/app_feishu_mode_test.go`:

```go
package main

import "testing"

func TestFeishuModePrefOrDefault_EmptyReturnsAuto(t *testing.T) {
	c := appConfig{}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, "auto")
	}
}

func TestFeishuModePrefOrDefault_KnownValuesPassthrough(t *testing.T) {
	for _, v := range []string{"auto", "local", "relay"} {
		c := appConfig{FeishuModePref: v}
		if got := c.FeishuModePrefOrDefault(); got != v {
			t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, v)
		}
	}
}

func TestFeishuModePrefOrDefault_UnknownFallsBackToAuto(t *testing.T) {
	c := appConfig{FeishuModePref: "garbage"}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q (defensive default)", got, "auto")
	}
}
```

- [ ] **Step 3: Run test, expect FAIL**

Run: `cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal && go test ./desktop/ -run TestFeishuModePrefOrDefault -v`

Expected: compile error (`FeishuModePref` undefined on `appConfig`; `FeishuModePrefOrDefault` undefined).

- [ ] **Step 4: Add the field and helper**

In `desktop/config.go`, inside the `appConfig` struct, add after the `AINotificationsOnly` block (use the same comment style as the existing field):

```go
	// FeishuModePref is the user's explicit choice for Feishu integration
	// mode. "auto" (default) follows the relay login state; "local" forces
	// the local LongConn path; "relay" requests relay-borrowed mode and
	// falls back to local when not effectively logged in.
	FeishuModePref string `json:"feishu_mode_pref,omitempty"`
```

Add the helper at the bottom of `desktop/config.go` near the other `*OrDefault` helpers:

```go
// FeishuModePrefOrDefault returns the persisted preference, normalizing
// unset / unknown values to "auto" (preserves the historical auto-resolve
// behavior).
func (c appConfig) FeishuModePrefOrDefault() string {
	switch c.FeishuModePref {
	case "auto", "local", "relay":
		return c.FeishuModePref
	default:
		return "auto"
	}
}
```

- [ ] **Step 5: Run test, expect PASS**

Run: `go test ./desktop/ -run TestFeishuModePrefOrDefault -v`

Expected: PASS for all three tests.

- [ ] **Step 6: Sanity-run the broader desktop suite**

Run: `go test ./desktop/ -count=1 -run "TestFeishuModePref|TestGetAINotificationsOnly|TestSetAINotificationsOnly" -v`

Expected: all PASS; no regression on neighbouring pref tests.

- [ ] **Step 7: Commit**

```bash
git add desktop/config.go desktop/app_feishu_mode_test.go
git commit -m "feat(desktop): add FeishuModePref to appConfig with auto default"
```

---

## Task 2: `feishuServiceConfig` three-state resolution

**Files:**
- Modify: `desktop/app.go:2129` (the `feishuServiceConfig` function)
- Test: `desktop/app_feishu_mode_test.go` (extend)

- [ ] **Step 1: Write the failing truth-table test**

Append to `desktop/app_feishu_mode_test.go`:

```go
func TestFeishuServiceConfig_TruthTable(t *testing.T) {
	cases := []struct {
		name        string
		pref        string
		relayURL    string
		relayToken  string
		relayPaused bool
		want        string // expected effective mode
	}{
		{"default empty no login", "", "", "", true, "local"},
		{"auto + logged in + uplink on", "auto", "wss://r", "tok", false, "relay"},
		{"auto + logged in + uplink paused", "auto", "wss://r", "tok", true, "local"},
		{"auto + no creds", "auto", "", "", false, "local"},
		{"local override while logged in", "local", "wss://r", "tok", false, "local"},
		{"local override no creds", "local", "", "", true, "local"},
		{"relay + logged in + uplink on", "relay", "wss://r", "tok", false, "relay"},
		{"relay fallback when uplink paused", "relay", "wss://r", "tok", true, "local"},
		{"relay fallback when no creds", "relay", "", "", false, "local"},
		{"unknown pref treated as auto", "garbage", "wss://r", "tok", false, "relay"},
	}

	a := newAppWithTempCfg(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appConfig{
				FeishuModePref:    tc.pref,
				RelayURL:          tc.relayURL,
				RelaySessionToken: tc.relayToken,
				RelayPaused:       tc.relayPaused,
			}
			_, mode := a.feishuServiceConfig(cfg)
			if mode != tc.want {
				t.Errorf("mode = %q; want %q", mode, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test, expect FAIL on the new sub-cases**

Run: `go test ./desktop/ -run TestFeishuServiceConfig_TruthTable -v`

Expected: most sub-tests FAIL because the current implementation ignores `FeishuModePref` AND ignores `RelayPaused`.

- [ ] **Step 3: Read the current `feishuServiceConfig`**

Open `desktop/app.go` and read the function at line 2129. The current body is approximately:

```go
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
	if cfg.RelayURL != "" && cfg.RelaySessionToken != "" {
		return feishu.ServiceConfig{
			Mode:     feishu.ModeRelay,
			RelayURL: relayHTTPBase(cfg.RelayURL),
			RelayToken: func() string {
				if a.cfgStore == nil {
					return ""
				}
				return a.cfgStore.Get().RelaySessionToken
			},
			RelayHTTPClient: relayHTTPClient(cfg.AllowInsecureRelay, 10*time.Second),
			Sessions:        a.host,
		}, "relay"
	}
	return feishu.ServiceConfig{Mode: feishu.ModeLocal, Sessions: a.host}, "local"
}
```

- [ ] **Step 4: Replace the function body with the three-state resolution**

```go
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
	loggedIn := cfg.RelayURL != "" && cfg.RelaySessionToken != "" && !cfg.RelayPaused

	effective := "local"
	switch cfg.FeishuModePrefOrDefault() {
	case "local":
		effective = "local"
	case "relay":
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
			log.Printf("desktop: feishu mode=relay requested but not effectively logged in (RelayPaused=%v); falling back to local", cfg.RelayPaused)
		}
	default: // "auto"
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
		}
	}

	if effective == "relay" {
		return feishu.ServiceConfig{
			Mode:     feishu.ModeRelay,
			RelayURL: relayHTTPBase(cfg.RelayURL),
			RelayToken: func() string {
				if a.cfgStore == nil {
					return ""
				}
				return a.cfgStore.Get().RelaySessionToken
			},
			RelayHTTPClient: relayHTTPClient(cfg.AllowInsecureRelay, 10*time.Second),
			Sessions:        a.host,
		}, "relay"
	}
	return feishu.ServiceConfig{Mode: feishu.ModeLocal, Sessions: a.host}, "local"
}
```

(The `log` package is already imported in `desktop/app.go`; no new import.)

- [ ] **Step 5: Run truth-table test, expect PASS**

Run: `go test ./desktop/ -run TestFeishuServiceConfig_TruthTable -v`

Expected: all 10 sub-tests PASS.

- [ ] **Step 6: Run any test that touches `feishuServiceConfig` to catch regressions**

Run: `go test ./desktop/ -count=1 -v 2>&1 | tail -30`

Expected: all tests still PASS (or only the pre-existing `desktop/hookinstall/TestEmbeddedBinaryRunnable` failure that we've documented as unrelated).

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/app_feishu_mode_test.go
git commit -m "feat(desktop): feishuServiceConfig honors FeishuModePref + RelayPaused"
```

---

## Task 3: Wails methods for the pref

**Files:**
- Modify: `desktop/app.go` (add the 3 methods near the existing AI-only methods)
- Test: `desktop/app_feishu_mode_test.go` (extend)

- [ ] **Step 1: Write the failing tests for the 3 methods**

Append to `desktop/app_feishu_mode_test.go`:

```go
func TestGetFeishuModePref_DefaultsToAuto(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetFeishuModePref(); got != "auto" {
		t.Errorf("GetFeishuModePref() = %q; want %q", got, "auto")
	}
}

func TestSetFeishuModePref_PersistsAndRoundTrips(t *testing.T) {
	a := newAppWithTempCfg(t)
	for _, v := range []string{"local", "relay", "auto"} {
		if err := a.SetFeishuModePref(v); err != nil {
			t.Fatalf("SetFeishuModePref(%q): %v", v, err)
		}
		if got := a.GetFeishuModePref(); got != v {
			t.Errorf("after Set(%q), Get() = %q", v, got)
		}
	}
}

func TestSetFeishuModePref_RejectsInvalid(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetFeishuModePref("garbage"); err == nil {
		t.Fatal("expected error for invalid pref; got nil")
	}
	// Persisted value untouched.
	if got := a.GetFeishuModePref(); got != "auto" {
		t.Errorf("after rejected Set, Get() = %q; want %q", got, "auto")
	}
}

func TestGetFeishuEffectiveMode_EmptyBeforeInit(t *testing.T) {
	a := newAppWithTempCfg(t)
	// startFeishu / reconcileFeishuMode have not run, so feishuMode is unset.
	if got := a.GetFeishuEffectiveMode(); got != "" {
		t.Errorf("GetFeishuEffectiveMode() before init = %q; want %q", got, "")
	}
}
```

- [ ] **Step 2: Run tests, expect FAIL (compile error)**

Run: `go test ./desktop/ -run "TestGetFeishuModePref|TestSetFeishuModePref|TestGetFeishuEffectiveMode" -v`

Expected: compile error — `GetFeishuModePref`, `SetFeishuModePref`, `GetFeishuEffectiveMode` undefined.

- [ ] **Step 3: Add the three methods in `desktop/app.go`**

Insert immediately after `SetAINotificationsOnly` (around line 1649). Copy the existing AI-only style verbatim:

```go
// GetFeishuModePref returns the persisted Feishu mode preference
// ("auto" | "local" | "relay"). Empty / unknown values resolve to "auto".
func (a *App) GetFeishuModePref() string {
	if a.cfgStore == nil {
		return "auto"
	}
	return a.cfgStore.Get().FeishuModePrefOrDefault()
}

// SetFeishuModePref persists the new preference and triggers a hot
// reconcile of the running Feishu service. Validates against the three
// known values; rejects anything else without mutating state.
func (a *App) SetFeishuModePref(pref string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	switch pref {
	case "auto", "local", "relay":
	default:
		return fmt.Errorf("invalid feishu mode preference %q (want auto|local|relay)", pref)
	}
	cfg := a.cfgStore.Get()
	cfg.FeishuModePref = pref
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("feishu_mode_pref")
	a.reconcileFeishuMode(a.ctx, cfg)
	return nil
}

// GetFeishuEffectiveMode returns the currently-running Feishu mode
// ("local" | "relay"), or "" before startFeishu has run. Independent
// of the persisted preference — reflects the actual swapped state.
func (a *App) GetFeishuEffectiveMode() string {
	a.feishuMu.RLock()
	defer a.feishuMu.RUnlock()
	return a.feishuMode
}
```

(`fmt` is already imported in `desktop/app.go`.)

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./desktop/ -run "TestGetFeishuModePref|TestSetFeishuModePref|TestGetFeishuEffectiveMode" -v`

Expected: all four tests PASS.

- [ ] **Step 5: Run the full Go suite to catch regressions**

Run: `go test ./desktop/ -count=1 2>&1 | tail -10`

Expected: clean (modulo the unrelated `desktop/hookinstall/TestEmbeddedBinaryRunnable`).

- [ ] **Step 6: Commit**

```bash
git add desktop/app.go desktop/app_feishu_mode_test.go
git commit -m "feat(desktop): add GetFeishuModePref / SetFeishuModePref / GetFeishuEffectiveMode"
```

---

## Task 4: Frontend Wails stubs + AppBindings interface + api.ts wrappers

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/src/lib/api.ts`

There is no useful unit test at this layer (these are pure thin wrappers around generated Wails RPC). The cross-check is `tsc --noEmit` plus the SettingsFeishu UI test in Task 6.

- [ ] **Step 1: Add JS stubs in `desktop/frontend/wailsjs/go/main/App.js`**

Find the existing `GetAINotificationsOnly` / `SetAINotificationsOnly` block (around line 49 and 241). Add three new exported functions following the exact same style. Place them grouped near the other Feishu methods (search for `GetFeishuRemoteTerminalSettings` to find the cluster):

```js
export function GetFeishuModePref() {
  return window['go']['main']['App']['GetFeishuModePref']();
}

export function SetFeishuModePref(arg1) {
  return window['go']['main']['App']['SetFeishuModePref'](arg1);
}

export function GetFeishuEffectiveMode() {
  return window['go']['main']['App']['GetFeishuEffectiveMode']();
}
```

- [ ] **Step 2: Add TS declarations in `desktop/frontend/wailsjs/go/main/App.d.ts`**

Find the existing `GetAINotificationsOnly` line (around line 30) and the `SetAINotificationsOnly` line (around line 126). Add three new declarations alongside them (alphabetic-ish ordering by method name is fine — match what surrounds them):

```ts
export function GetFeishuModePref():Promise<string>;
export function SetFeishuModePref(arg1:string):Promise<void>;
export function GetFeishuEffectiveMode():Promise<string>;
```

- [ ] **Step 3: Extend the `AppBindings` interface in `desktop/frontend/src/lib/api.ts`**

Find the `AppBindings` interface and the `SetAINotificationsOnly` entry inside it (around line 311 per Task 20's review). Insert these three lines after `SetAINotificationsOnly`:

```ts
  GetFeishuModePref(): Promise<string>;
  SetFeishuModePref(pref: string): Promise<void>;
  GetFeishuEffectiveMode(): Promise<string>;
```

This is the line Phase 1 Task 20 missed; **make sure it's added here AND in `App.d.ts`** above.

- [ ] **Step 4: Add the wrapper functions in `desktop/frontend/src/lib/api.ts`**

Find the existing `getAINotificationsOnly` / `setAINotificationsOnly` wrappers (around line 665). Append three new exported wrappers:

```ts
export function getFeishuModePref(): Promise<string> {
  return bindings().GetFeishuModePref();
}

export function setFeishuModePref(pref: string): Promise<void> {
  return bindings().SetFeishuModePref(pref);
}

export function getFeishuEffectiveMode(): Promise<string> {
  return bindings().GetFeishuEffectiveMode();
}
```

- [ ] **Step 5: Type-check the frontend**

Run:

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal/desktop/frontend
npx tsc --noEmit 2>&1 | head -20
```

Expected: 0 errors related to the new methods. (Any pre-existing errors unrelated to this work — e.g. canvas / jsdom — can be ignored, same as Phase 1.)

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal
git add desktop/frontend/wailsjs/go/main/App.js desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/src/lib/api.ts
git commit -m "feat(frontend): wails stubs + api wrappers for FeishuModePref methods"
```

---

## Task 5: i18n keys

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Locate the existing Feishu settings key block**

Open `desktop/frontend/src/i18n/messages/zh-CN.ts`. Search for `settings.feishu` or for the existing remote-terminal labels (Task 20 added `remoteTerminal.enable` etc.). The new keys nest as siblings of `remoteTerminal`.

- [ ] **Step 2: Add the new keys in `zh-CN.ts`**

Inside `settings.feishu` (the same object that holds `remoteTerminal`), add a new `mode` object:

```ts
mode: {
  label: '模式',
  options: {
    auto:  '自动（跟随 Relay 登录态）',
    local: '强制 Local（本机 LongConn）',
    relay: '强制 Relay（依赖 Relay 转发）',
  },
  effective: {
    label: '生效模式',
    relayLoggedAs: '已连接 {email}',
    fallbackWarn: '已请求 relay 但未登录 / UPLINK 关闭',
  },
},
```

- [ ] **Step 3: Mirror the same keys in `en.ts`**

Open `desktop/frontend/src/i18n/messages/en.ts`. Add the matching block under `settings.feishu`:

```ts
mode: {
  label: 'Mode',
  options: {
    auto:  'Auto (follow Relay login)',
    local: 'Force Local (local LongConn)',
    relay: 'Force Relay (depend on Relay)',
  },
  effective: {
    label: 'Effective mode',
    relayLoggedAs: 'connected as {email}',
    fallbackWarn: 'Relay requested but not effectively logged in',
  },
},
```

- [ ] **Step 4: Verify both files still parse**

Run:

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal/desktop/frontend
npx tsc --noEmit 2>&1 | head -10
```

Expected: no new errors. (Any pre-existing unrelated errors carry through.)

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(feishu): add mode dropdown + effective-mode keys (en + zh-CN)"
```

---

## Task 6: SettingsFeishu.vue mode dropdown + effective-mode display

**Files:**
- Modify: `desktop/frontend/src/components/SettingsFeishu.vue`
- Modify: `desktop/frontend/src/components/SettingsFeishu.test.ts`

- [ ] **Step 1: Read the existing structure**

Open `desktop/frontend/src/components/SettingsFeishu.vue`. Find:
- The Hook install row (around line 7).
- The static `模式: relay` line (search for `mode` or the `模式` literal).
- The AI-only toggle (around line 26).
- The script setup block (around line 129).

The mode section sits between the Hook row and the AI-only toggle.

- [ ] **Step 2: Add reactive refs and a loader**

In the `<script setup>` block, near the existing `aiOnlyNotifications` ref, add:

```ts
import {
  getFeishuModePref,
  setFeishuModePref,
  getFeishuEffectiveMode,
  // ... existing imports
} from '@shared/lib/api'

const feishuModePref = ref<'auto' | 'local' | 'relay'>('auto')
const feishuEffectiveMode = ref('')
```

In the existing `onMounted` block where `aiOnlyNotifications.value` is loaded, also load the mode state:

```ts
feishuModePref.value = (await getFeishuModePref()) as 'auto' | 'local' | 'relay'
feishuEffectiveMode.value = await getFeishuEffectiveMode()
```

(Search for the existing `aiOnlyNotifications.value = await getAINotificationsOnly()` line to find the right place.)

- [ ] **Step 3: Add the change handler**

In the `<script setup>` block, add:

```ts
async function onFeishuModeChange(e: Event) {
  const next = (e.target as HTMLSelectElement).value
  try {
    await setFeishuModePref(next)
    feishuModePref.value = next as 'auto' | 'local' | 'relay'
    // Reconcile may have swapped the running mode synchronously; refresh.
    feishuEffectiveMode.value = await getFeishuEffectiveMode()
  } catch (err) {
    // Rollback the dropdown to the persisted value on failure.
    feishuModePref.value = (await getFeishuModePref()) as 'auto' | 'local' | 'relay'
    console.error('SetFeishuModePref failed', err)
  }
}
```

- [ ] **Step 4: Replace the static `模式: relay` line in the template**

Find the existing `<div class="hook-install__mode">...</div>` (or whatever wrapper renders `模式: relay` today — search the template for `mode` / `模式`). Replace that whole element with:

```vue
<div class="feishu-mode">
  <label class="feishu-mode__label">
    {{ t('settings.feishu.mode.label') }}
    <select
      class="feishu-mode__select"
      :value="feishuModePref"
      @change="onFeishuModeChange"
    >
      <option value="auto">{{ t('settings.feishu.mode.options.auto') }}</option>
      <option value="local">{{ t('settings.feishu.mode.options.local') }}</option>
      <option value="relay">{{ t('settings.feishu.mode.options.relay') }}</option>
    </select>
  </label>
  <p class="feishu-mode__effective">
    <span>{{ t('settings.feishu.mode.effective.label') }}:</span>
    <strong>{{ feishuEffectiveMode || '—' }}</strong>
    <span
      v-if="feishuModePref === 'relay' && feishuEffectiveMode === 'local'"
      class="feishu-mode__warn"
    >
      ⚠ {{ t('settings.feishu.mode.effective.fallbackWarn') }}
    </span>
  </p>
</div>
```

- [ ] **Step 5: Add minimal styling at the bottom of `<style scoped>`**

```css
.feishu-mode { margin: 8px 0; }
.feishu-mode__label { display: flex; gap: 8px; align-items: center; font-size: 13px; }
.feishu-mode__select { padding: 2px 6px; }
.feishu-mode__effective { margin: 4px 0 0; font-size: 12px; color: var(--fg-dim, #999); }
.feishu-mode__warn { color: var(--warn, #d89614); margin-left: 8px; }
```

(Match whichever `--fg-dim` / `--warn` token the file already uses — search the existing `<style scoped>` for `var(--`.)

- [ ] **Step 6: Extend `SettingsFeishu.test.ts` with mode-dropdown coverage**

Open `desktop/frontend/src/components/SettingsFeishu.test.ts`. Find the existing remote-terminal tests added in Task 20. Add three new source-text assertions in the same style:

```ts
test('renders Feishu mode dropdown with all three options', async () => {
  const src = await Bun.file('src/components/SettingsFeishu.vue').text()
  expect(src).toContain("settings.feishu.mode.options.auto")
  expect(src).toContain("settings.feishu.mode.options.local")
  expect(src).toContain("settings.feishu.mode.options.relay")
})

test('binds Feishu mode dropdown to setFeishuModePref via a change handler', async () => {
  const src = await Bun.file('src/components/SettingsFeishu.vue').text()
  expect(src).toContain('onFeishuModeChange')
  expect(src).toContain('setFeishuModePref')
})

test('renders effective-mode line and a fallback warning', async () => {
  const src = await Bun.file('src/components/SettingsFeishu.vue').text()
  expect(src).toContain("settings.feishu.mode.effective.label")
  expect(src).toContain("settings.feishu.mode.effective.fallbackWarn")
})
```

(Match the test framework + file-read style used by the existing Task 20 tests in this file — if they use `import('fs')` or a different reader, mirror that. The above uses Bun.file because Task 20's report referenced "source-text assertions".)

- [ ] **Step 7: Run the test file**

Run:

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal/desktop/frontend
npm test -- SettingsFeishu 2>&1 | tail -15
```

Expected: the three new tests PASS; existing SettingsFeishu tests still PASS.

- [ ] **Step 8: Type-check end-to-end**

Run: `npx tsc --noEmit 2>&1 | head -20`

Expected: 0 new errors.

- [ ] **Step 9: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal
git add desktop/frontend/src/components/SettingsFeishu.vue desktop/frontend/src/components/SettingsFeishu.test.ts
git commit -m "feat(ui): Feishu mode dropdown + effective-mode display"
```

---

## Task 7: Integration smoke + final verification

**Files:**
- (No file changes — verification only.)

- [ ] **Step 1: Re-run the full Go suite**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/feishu-as-terminal
go test ./... -count=1 2>&1 | grep -E "^(ok|FAIL)" | tail -30
```

Expected: only the pre-existing `desktop/hookinstall/TestEmbeddedBinaryRunnable` failure (unrelated, needs built artifact). Every other package PASS.

- [ ] **Step 2: Re-run the frontend suite**

```bash
cd desktop/frontend
npm test 2>&1 | tail -5
```

Expected: all tests PASS (or the pre-existing unrelated jsdom canvas failure carries through).

- [ ] **Step 3: Final type-check sweep**

```bash
npx tsc --noEmit 2>&1 | head -30
```

Expected: no new errors. Anything that fails must be unrelated to the methods / interfaces added in this plan.

- [ ] **Step 4: Manual smoke checklist (record findings as comments, do not block on this if you cannot run the desktop binary)**

Build / start the desktop app (`wails dev` or your usual flow). Open Settings → 飞书集成. Walk through:

- [ ] Default `auto`: with relay logged in + UPLINK on, effective mode shows `relay`.
- [ ] Flip UPLINK off: effective mode flips to `local` automatically (no preference change needed).
- [ ] Change pref to `local`: dropdown updates, effective mode stays `local` even when UPLINK is back on.
- [ ] Change pref to `relay` with UPLINK off: dropdown updates, effective mode is `local`, warning line visible.
- [ ] Change pref back to `auto`: effective mode follows UPLINK as before.
- [ ] Restart the app: persisted preference round-trips (read from `~/.config/atterm/config.json`).

If the binary cannot be built in this environment, write a single line in the final commit message noting the manual smoke is deferred to the maintainer.

- [ ] **Step 5: Tag the spec checkpoint**

```bash
git tag feishu-mode-override-v1
```

- [ ] **Step 6: Confirm clean working tree and summarise**

Run: `git status && git log --oneline 48f9fa2..HEAD`

Expected: working tree clean; 6 commits since the spec (one per Task 1–6).

---

## Self-review against the spec

Spec F-features → plan tasks:

| Feature | Task |
|---|---|
| F1 `appConfig.FeishuModePref` + default | Task 1 |
| F2 `feishuServiceConfig` three-state + `RelayPaused` | Task 2 |
| F3 Wails methods (`Get/SetFeishuModePref`, `GetFeishuEffectiveMode`) | Task 3 |
| F4 UI dropdown + effective-mode display | Task 6 |
| F5 i18n keys | Task 5 |
| F6 Fallback warning (inline) | Task 6 (Step 4 includes the `v-if` warn line) |
| F7 Reconcile trigger from `SetFeishuModePref` | Task 3 (`a.reconcileFeishuMode(a.ctx, cfg)` in `SetFeishuModePref`) |

Spec §Tests → plan tasks:

| Spec test | Task |
|---|---|
| Unit truth-table on `feishuServiceConfig` (10 cases) | Task 2 |
| Integration `SetFeishuModePref` round-trip + invalid rejection | Task 3 |
| UI extension on `SettingsFeishu.test.ts` | Task 6 |
| Regression on existing SettingsFeishu tests | Task 6 (Step 7) |

Frontend wiring discipline (Phase 1 Task 20 lesson): `AppBindings` interface entries are explicitly listed in Task 4 alongside the `App.d.ts` declarations.

No placeholder text in the plan body. Method names verified consistent across tasks (`GetFeishuModePref`, `SetFeishuModePref`, `GetFeishuEffectiveMode`, `FeishuModePrefOrDefault`).
