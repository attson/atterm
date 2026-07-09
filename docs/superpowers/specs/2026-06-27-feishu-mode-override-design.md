# Feishu Mode Override — Design

**Date:** 2026-06-27
**Status:** Draft, pending review
**Related work:**
- 2026-06-26 feishu-as-terminal (Phase 1–3 foundation)
- 2026-06-26 feishu-ai-only-notifications
- 2026-06-17 feishu-app-integration

## Summary

Decouple the Feishu integration's "mode" (local LongConn vs relay-borrowed) from
the relay login state. Today the mode is implicit: any logged-in relay account
auto-switches Feishu to relay mode, even when UPLINK is off. This forces users
who want their atterm sessions local-only into a Feishu config they can't opt
out of, and (worse) makes the new remote-terminal feature unusable for them —
since the relay process doesn't yet route anchor-card events back to the
desktop process (see Task 10 of the Phase 1 plan).

This spec adds an explicit per-machine `FeishuModePref` setting (`auto` /
`local` / `relay`) plus a small UI surface in the Feishu integration settings
page. The default (`auto`) preserves today's behavior, with one targeted
correction: the auto path now treats UPLINK-off as "not effectively logged in"
so flipping UPLINK off naturally falls back to local mode.

## Goals & non-goals

### Goals

- Let a logged-in user explicitly force local mode for Feishu (the screenshot
  pain point).
- Let a user explicitly force relay mode when needed (forward-compat for
  Sub-spec 2's LongConn-on-relay architecture).
- Surface the effective mode in the UI so silent fallbacks (e.g. `pref=relay`
  but not logged in) don't confuse users.
- UPLINK toggle naturally drives the auto path: turning UPLINK off shifts auto
  to local without code-path divergence.

### Non-goals (this spec)

- Sub-spec 2's LongConn-on-relay architecture (relay holds the LongConn and
  routes events to the right machine). That's a separate spec.
- Per-relay-account preference (one preference per machine is fine for
  single-user scope).
- Syncing `FeishuModePref` across machines via the relay account (it's a
  machine-local preference by design).
- A "test connection" button for the chosen mode (the existing "测试发送"
  test buttons already cover this).

## Architecture overview

The change is contained to three modules; no new packages.

```
┌──────────────────────────────┐
│ appConfig.FeishuModePref     │ (persisted to config.json)
│   "auto" | "local" | "relay" │
└──────────────┬───────────────┘
               │
               ▼
┌────────────────────────────────────────────┐
│ feishuServiceConfig(cfg)                   │
│   loggedIn := RelayURL!="" &&              │
│               RelaySessionToken!="" &&     │
│               !RelayPaused                 │   ◄── auto path now respects UPLINK
│   effectiveMode := resolve(Pref, loggedIn) │
│   returns (svcCfg, effectiveMode)          │
└──────────────┬─────────────────────────────┘
               │
               ▼
┌────────────────────────────────────────────┐
│ startFeishu / reconcileFeishuMode          │  (existing; reused)
│   builds & swaps Service                   │
│   trigger added: SetFeishuModePref         │
└────────────────────────────────────────────┘
```

UI surfaces three Wails methods (`Get/SetFeishuModePref`,
`GetFeishuEffectiveMode`) consumed by a new mode dropdown in
`SettingsFeishu.vue`.

## Data model

### `appConfig.FeishuModePref` (new field)

```go
type appConfig struct {
    // ... existing fields ...
    FeishuModePref string `json:"feishu_mode_pref"` // "auto" | "local" | "relay"; default "auto"
}
```

- Lives in the existing `desktop/config.go` `appConfig` (alongside
  `AINotificationsOnly`).
- Persisted as JSON in the desktop config file (`config.json`).
- Default `"auto"`. Unknown values fall through to `"auto"` semantics.
- Not stored in `feishu_bindings` (SQLite). Rationale: this preference exists
  independently of whether the user has bound a Feishu app yet.

## Mode resolution

### Rules

```
loggedIn = (RelayURL != "" AND RelaySessionToken != "" AND !RelayPaused)

(RelayPaused is the persisted UPLINK toggle: true = uplink off / paused.)

pref       loggedIn    effectiveMode    notes
─────────  ─────────   ─────────────    ────────────────────────────────────
"auto"     true        relay            preserved auto behavior
"auto"     false       local            preserved auto behavior
"local"    *           local            user override; UPLINK irrelevant
"relay"    true        relay            user override
"relay"    false       local            graceful fallback; UI shows mismatch
unknown    *           same as "auto"   defensive default
```

### `feishuServiceConfig` (modified)

In `desktop/app.go`, the current function:

```go
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
    if cfg.RelayURL != "" && cfg.RelaySessionToken != "" {
        return relayConfig, "relay"
    }
    return localConfig, "local"
}
```

becomes:

```go
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
    loggedIn := cfg.RelayURL != "" && cfg.RelaySessionToken != "" && !cfg.RelayPaused

    var effective string
    switch cfg.FeishuModePref {
    case "local":
        effective = "local"
    case "relay":
        if loggedIn {
            effective = "relay"
        } else {
            effective = "local"
            log.Printf("desktop: feishu mode=relay requested but not effectively logged in; falling back to local")
        }
    default: // "auto" or unknown
        if loggedIn {
            effective = "relay"
        } else {
            effective = "local"
        }
    }

    if effective == "relay" {
        // ... existing relay ServiceConfig build ...
        return relaySvcCfg, "relay"
    }
    return feishu.ServiceConfig{Mode: feishu.ModeLocal, Sessions: a.host}, "local"
}
```

### Hot-swap path

`reconcileFeishuMode` already rebuilds the Service when the resolved mode
changes (long-conn / dispatcher / store / token source swap; HookServer stays
to preserve `ATTERM_HOOK_ENDPOINT` in already-open PTYs). We add a second
trigger:

```go
// Wails method, called by the UI on dropdown change.
func (a *App) SetFeishuModePref(ctx context.Context, pref string) error {
    if pref != "auto" && pref != "local" && pref != "relay" {
        return fmt.Errorf("invalid feishu mode preference %q", pref)
    }
    cfg := a.cfgStore.Update(func(c *appConfig) { c.FeishuModePref = pref })
    a.reconcileFeishuMode(ctx, cfg)
    return nil
}
```

The reconcile callsite list becomes:
1. After relay login state changes (existing — `applyRelayConfig`).
2. After UPLINK toggle (existing path or new — verify which method triggers it).
3. After `SetFeishuModePref` (new).

## UI surface

In `desktop/frontend/src/components/SettingsFeishu.vue`, between the Hook
install line and the AI-only toggle, add a mode section:

```
─────────────────────────────────────────
● Hook 已安装且健康              [✓] 自动安装 Claude Code Hook

模式：[ 自动（跟随 Relay 登录态）  ▾ ]
       生效模式: relay · 已连接 attson1@outlook.com

[✓] 仅 AI 会话通知
[ ] 启用飞书远程接管

─────────────────────────────────────────
```

### Dropdown options

| Value | Label (zh-CN) | Behavior |
|---|---|---|
| `auto` | `自动（跟随 Relay 登录态）` | Resolves per the auto path above. |
| `local` | `强制 Local（本机 LongConn）` | Always local. |
| `relay` | `强制 Relay（依赖 Relay 转发）` | Relay when effectively logged in; falls back to local with a toast otherwise. |

### "Effective mode" line

Always shows the actual running mode (read from `GetFeishuEffectiveMode`),
independently of the preference. Format:

- Effective `local`: `生效模式: local`
- Effective `relay`: `生效模式: relay · 已连接 {email}`

When `pref=relay` and effective is `local` (fallback), display a small
inline warning rather than just the effective line:

```
生效模式: local · ⚠ 已请求 relay 但未登录 / UPLINK 关闭
```

This makes the fallback visible at a glance without modal noise.

### Replacing the existing `模式: relay` static line

Today (per the screenshot) the page renders a static line `模式: relay`. That
line is replaced by the new "effective mode" line, which carries the same
information plus richer context.

## Wails / API surface

Three new methods on `App`:

```go
// GetFeishuModePref returns the persisted preference ("auto" | "local" | "relay").
// Returns "auto" when unset.
func (a *App) GetFeishuModePref() string

// SetFeishuModePref persists the new preference and triggers a hot reconcile.
// Validates the pref against the three known values; returns an error otherwise.
func (a *App) SetFeishuModePref(ctx context.Context, pref string) error

// GetFeishuEffectiveMode returns the actual running mode ("local" | "relay"),
// as set by the most recent startFeishu / reconcileFeishuMode call.
// Returns "" before first init.
func (a *App) GetFeishuEffectiveMode() string
```

These need:
- Hand-written stubs added to `desktop/frontend/wailsjs/go/main/App.{js,d.ts}`
  (`models.ts` unchanged — no new struct types).
- Entries in the TypeScript `AppBindings` interface in `desktop/frontend/src/lib/api.ts`.
- Thin client wrappers in `desktop/frontend/src/lib/api.ts`.

## i18n keys (new)

In `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts`:

```ts
settings.feishu.mode = {
  label: '模式' / 'Mode',
  options: {
    auto:  '自动（跟随 Relay 登录态）' / 'Auto (follow Relay login)',
    local: '强制 Local（本机 LongConn）' / 'Force Local (local LongConn)',
    relay: '强制 Relay（依赖 Relay 转发）' / 'Force Relay (depend on Relay)',
  },
  effective: {
    label: '生效模式' / 'Effective mode',
    relayLoggedAs: '已连接 {email}' / 'connected as {email}',
    fallbackWarn: '已请求 relay 但未登录 / UPLINK 关闭' / 'Relay requested but not effectively logged in',
  },
}
```

## Failure modes

| Scenario | Behavior |
|---|---|
| `SetFeishuModePref("garbage")` | Returns `error`; UI rolls back the dropdown to the persisted value. |
| Service rebuild fails during reconcile (e.g. local LongConn can't reach Feishu) | Existing reconcile logging path; UI doesn't crash; effective mode still updates to reflect the attempted mode. |
| Reconcile mid-flight with open AskQuestion cards | HookServer is preserved, so in-flight hook POSTs keep working. The dispatcher is swapped; in-flight Feishu callbacks routed via the old dispatcher complete on the old service goroutines (existing reconcile semantics). |
| `pref=relay` + user logs out of relay at runtime | Reconcile triggers; effective mode falls back to local; "warn" line shown until the user either logs back in or switches preference. |

## Tests

### Unit (`desktop/app_feishu_mode_test.go` — new)

Truth table coverage of `feishuServiceConfig`:

| `Pref` | `RelayURL?` | `Token?` | `RelayPaused` | Expected effective |
|---|---|---|---|---|
| `""` (default) | "" | "" | true | local |
| `"auto"` | set | set | false | relay |
| `"auto"` | set | set | true | local (UPLINK paused) |
| `"auto"` | "" | "" | false | local |
| `"local"` | set | set | false | local (override) |
| `"local"` | "" | "" | true | local |
| `"relay"` | set | set | false | relay |
| `"relay"` | set | set | true | local (fallback) |
| `"relay"` | "" | "" | false | local (fallback) |
| `"garbage"` | set | set | false | relay (defensive: treat as auto) |

### Integration

- `TestSetFeishuModePref_TriggersReconcile`: starts with `pref=auto` + logged in →
  effective `relay`. Call `SetFeishuModePref("local")` → effective `local`.
  Verify by reading `GetFeishuEffectiveMode()` after the call.
- `TestSetFeishuModePref_RejectsInvalid`: `SetFeishuModePref("garbage")` → error;
  persisted value unchanged.

### UI (`desktop/frontend/src/components/SettingsFeishu.test.ts` — extend)

- Mode dropdown renders all three options.
- Default state shows the persisted pref via `GetFeishuModePref`.
- Selecting `local` calls `SetFeishuModePref("local")` and reads back
  `GetFeishuEffectiveMode()`.
- Fallback warning line renders when pref `relay` + effective `local`.

### Regression

Existing `SettingsFeishu` tests must continue to pass — the mode section is
additive, not a replacement of any existing toggle.

## In-scope feature set (F1–F7)

| # | Feature |
|---|---|
| F1 | `appConfig.FeishuModePref` field + default `"auto"` |
| F2 | `feishuServiceConfig` three-state resolution + `RelayPaused`-aware `loggedIn` |
| F3 | Wails methods: `GetFeishuModePref`, `SetFeishuModePref`, `GetFeishuEffectiveMode` |
| F4 | UI mode dropdown + effective-mode line in `SettingsFeishu.vue` |
| F5 | i18n keys for the dropdown labels + effective line |
| F6 | Fallback warning (inline text) when pref `relay` + effective `local` |
| F7 | Reconcile trigger from `SetFeishuModePref` |

## Delivery estimate

| Item | Estimate |
|---|---|
| F1 + F2 + F3 (Go side) | ~half day |
| F4 + F5 (Vue + i18n) | ~half day |
| F6 + F7 (polish + trigger plumbing) | ~half day |
| Tests + edge cases | ~half day |
| **Total** | **~2 days** |

## Open questions deferred to plan stage

- Whether the AppBindings interface entries (lessons from Phase 1 Task 20)
  need an automated `tsc --noEmit` gate or hand-discipline is enough.
- Whether to surface the "warning" on the fallback in another way (toast?
  notification badge?) — current spec uses inline text for simplicity; the
  plan stage can pick a final treatment.
- Whether the existing `applyRelayConfig` path that calls
  `reconcileFeishuMode` after relay login state changes also needs to fire
  when UPLINK alone toggles. Inspecting the current callsite during plan
  stage will confirm.
