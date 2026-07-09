# Signed-in Devices — Design

**Date:** 2026-07-09
**Status:** Draft, pending review
**Related work:**
- 2026-05-15 saas-user-accounts (relay `/api/me/sessions` endpoints)
- 2026-06-10 relay-settings-auth-state (Settings → Relay layout)
- 2026-07-08 clear-relay-info (`SettingsDialog.vue` tab structure & danger-zone pattern)

## Summary

Add a new **已登录设备** (Signed-in Devices) tab to the desktop's Settings dialog
that mirrors the web app's `/settings#sessions` panel: list every browser /
desktop / mobile device currently signed into the same relay account,
identify the current desktop, and let the user revoke individual sessions
or sign out every other device with one click.

The feature reuses the relay's existing `/api/me/sessions*` endpoints (the
web app already consumes them via `web/src/shared/api/me.ts`). No relay
changes required.

## Goals & non-goals

### Goals

- One new Settings tab labelled **已登录设备** listing every active session
  for the currently-logged-in relay account.
- Each row shows the session's User-Agent, the login timestamp, and the
  first-N IP prefix (as the relay reports it).
- The row for the current desktop shows a dim "当前" tag instead of a
  revoke button, matching the web behavior.
- A "登出其他设备" button at the top of the panel revokes every session
  except the current one, guarded by a `window.confirm`.
- A refresh icon at the top re-fetches the list on demand; the list also
  auto-refreshes after any successful revoke or sign-out-others action.
- When the user is not signed in to a relay account (no token), the panel
  shows a friendly "log in first" copy instead of an empty table + error
  banner.

### Non-goals

- No password change tab. If needed later, it lands in its own tab
  ("账号 → 修改密码") — leaves this tab focused.
- No account deletion (danger zone). Same rationale.
- No IP geolocation lookup or user-agent parsing prettification. Show the
  raw values the relay returns.
- No polling. Explicit refresh only.
- No mobile / Capacitor implementation. `SettingsDialog.vue` is desktop-only.

## Architecture

Three new Wails-bound Go verbs + one new Vue component + one new tab in
`SettingsDialog.vue` + i18n copy.

```
SettingsDialog.vue
  └─ Tab bar gains "devices" between "relay" and "plugins"
       └─ activeTab === 'devices' renders <SettingsDevices />
            ├─ onMounted → reload()
            │    └─ listRelaySessions()
            │         └─ App.ListRelaySessions() → []RelaySessionRow
            │              └─ GET <relay>/api/me/sessions
            │                   Authorization: Bearer <cfg.RelaySessionToken>
            │
            ├─ Header row
            │    ├─ Hint copy
            │    ├─ ⟳ refresh button → reload()
            │    └─ "登出其他设备" button
            │         → window.confirm → signOutOtherRelaySessions()
            │              → POST <relay>/api/me/sessions/sign-out-others
            │              → reload()
            │
            └─ Per-row: user_agent / login-line / [revoke | "当前" tag]
                 revoke → window.confirm → revokeRelaySession(id_hash)
                      → DELETE <relay>/api/me/sessions/{id_hash}
                      → reload()
```

## Backend

### New file: `desktop/relay_sessions.go`

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
)

// RelaySessionRow mirrors the relay's SessionRow shape used by
// GET /api/me/sessions.
type RelaySessionRow struct {
    IDHash    string `json:"id_hash"`
    UserAgent string `json:"user_agent"`
    IPPrefix  string `json:"ip_prefix"`
    CreatedAt int64  `json:"created_at"` // unix ms
    ExpiresAt int64  `json:"expires_at"` // unix ms
    IsCurrent bool   `json:"is_current"`
}

// SignOutOthersResult mirrors the response of POST
// /api/me/sessions/sign-out-others.
type SignOutOthersResult struct {
    Deleted int `json:"deleted"`
}

// meSessionsGET issues GET /api/me/sessions and parses the response.
func (a *App) meSessionsGET(ctx context.Context, base, token string, allowInsecure bool) ([]RelaySessionRow, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/api/me/sessions", nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    body, err := a.doRelayHTTP(req, allowInsecure)
    if err != nil {
        return nil, err
    }
    var rows []RelaySessionRow
    if err := json.Unmarshal(body, &rows); err != nil {
        return nil, fmt.Errorf("parse sessions response: %w", err)
    }
    return rows, nil
}

// meSessionDELETE revokes one session by id_hash.
func (a *App) meSessionDELETE(ctx context.Context, base, token, idHash string, allowInsecure bool) error {
    u := strings.TrimRight(base, "/") + "/api/me/sessions/" + url.PathEscape(idHash)
    req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    _, err = a.doRelayHTTP(req, allowInsecure)
    return err
}

// meSessionsSignOutOthers revokes every session except the current one.
func (a *App) meSessionsSignOutOthers(ctx context.Context, base, token string, allowInsecure bool) (SignOutOthersResult, error) {
    req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/me/sessions/sign-out-others", nil)
    if err != nil {
        return SignOutOthersResult{}, err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    body, err := a.doRelayHTTP(req, allowInsecure)
    if err != nil {
        return SignOutOthersResult{}, err
    }
    var out SignOutOthersResult
    if err := json.Unmarshal(body, &out); err != nil {
        return SignOutOthersResult{}, fmt.Errorf("parse sign-out-others response: %w", err)
    }
    return out, nil
}

// doRelayHTTP is a thin helper that issues the request with the
// same TLS / insecure-mode policy the rest of the desktop's relay
// callers use. Returns the response body on 2xx, or an error whose
// message the frontend can surface directly.
func (a *App) doRelayHTTP(req *http.Request, allowInsecure bool) ([]byte, error) {
    // Same TLS + proxy policy as FetchRelayMe. Timeout=0 uses the
    // relayHTTPClient's built-in default (see relay_httpclient.go).
    client := relayHTTPClient(allowInsecure, 0)
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode == 401 {
        return nil, fmt.Errorf("session expired, please log in again")
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("relay returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
    }
    return body, nil
}
```

Existing helpers used verbatim: `relayHTTPClient(allowInsecure bool, timeout
time.Duration) *http.Client` (in `desktop/relay_httpclient.go:22`) and
`relayHTTPBase(relayURL string) string` (in `desktop/app.go:2143`). Pass
`timeout=0` for default. Pattern matches `FetchRelayMe` at `app.go:1919`.

### Three new App methods (in `desktop/app.go`)

```go
// ListRelaySessions returns every active session for the currently
// logged-in relay account. Bound to Settings → Signed-in Devices.
func (a *App) ListRelaySessions() ([]RelaySessionRow, error) {
    if a.cfgStore == nil {
        return nil, fmt.Errorf("config store not ready")
    }
    cfg := a.cfgStore.Get()
    if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
        return nil, fmt.Errorf("not authenticated")
    }
    return a.meSessionsGET(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}

// RevokeRelaySession revokes one session by id_hash.
func (a *App) RevokeRelaySession(idHash string) error {
    idHash = strings.TrimSpace(idHash)
    if idHash == "" {
        return fmt.Errorf("id_hash is empty")
    }
    if a.cfgStore == nil {
        return fmt.Errorf("config store not ready")
    }
    cfg := a.cfgStore.Get()
    if cfg.RelaySessionToken == "" {
        return fmt.Errorf("not authenticated")
    }
    return a.meSessionDELETE(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, idHash, cfg.AllowInsecureRelay)
}

// SignOutOtherRelaySessions revokes every session except the current one.
func (a *App) SignOutOtherRelaySessions() (SignOutOthersResult, error) {
    if a.cfgStore == nil {
        return SignOutOthersResult{}, fmt.Errorf("config store not ready")
    }
    cfg := a.cfgStore.Get()
    if cfg.RelaySessionToken == "" {
        return SignOutOthersResult{}, fmt.Errorf("not authenticated")
    }
    return a.meSessionsSignOutOthers(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}
```

### API surface summary

| Wails binding | New | Semantics |
|---|---|---|
| `ListRelaySessions() ([]RelaySessionRow, error)` | ✓ | GET /api/me/sessions |
| `RevokeRelaySession(idHash string) error` | ✓ | DELETE /api/me/sessions/{idHash} |
| `SignOutOtherRelaySessions() (SignOutOthersResult, error)` | ✓ | POST /api/me/sessions/sign-out-others |

## Frontend

### Wails bindings + `lib/api.ts` extensions

`desktop/frontend/wailsjs/go/main/App.d.ts` + `App.js` gain three
generated declarations adjacent to `FetchRelayMe`.

`desktop/frontend/src/lib/api.ts` gains:

```ts
export interface RelaySessionRow {
  id_hash: string;
  user_agent: string;
  ip_prefix: string;
  created_at: number;   // unix ms
  expires_at: number;   // unix ms
  is_current: boolean;
}

export interface SignOutOthersResult {
  deleted: number;
}
```

Add to `AppBindings`:
```ts
ListRelaySessions(): Promise<RelaySessionRow[]>;
RevokeRelaySession(idHash: string): Promise<void>;
SignOutOtherRelaySessions(): Promise<SignOutOthersResult>;
```

Top-level wrappers:
```ts
export function listRelaySessions(): Promise<RelaySessionRow[]> {
  return bindings().ListRelaySessions();
}
export function revokeRelaySession(idHash: string): Promise<void> {
  return bindings().RevokeRelaySession(idHash);
}
export function signOutOtherRelaySessions(): Promise<SignOutOthersResult> {
  return bindings().SignOutOtherRelaySessions();
}
```

### New component: `desktop/frontend/src/components/SettingsDevices.vue`

Reactive state:
```ts
const rows = ref<RelaySessionRow[]>([])
const loading = ref(true)
const refreshing = ref(false)
const signingOutOthers = ref(false)
const revoking = ref<Record<string, boolean>>({})
const error = ref('')
const notAuthed = ref(false)
```

Load / reload / handlers:
```ts
async function reload(silent = false) {
  if (!silent) loading.value = true
  refreshing.value = true
  error.value = ''
  notAuthed.value = false
  try {
    rows.value = await listRelaySessions()
  } catch (e: any) {
    const msg = e?.message ?? String(e)
    if (msg.includes('not authenticated')) {
      notAuthed.value = true
    } else {
      error.value = msg
    }
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

onMounted(reload)

async function onRefresh() { await reload(true) }

async function onRevoke(row: RelaySessionRow) {
  const ua = row.user_agent || t('settings.devices.unknownUA')
  if (!window.confirm(t('settings.devices.revokeConfirm', { ua }))) return
  revoking.value = { ...revoking.value, [row.id_hash]: true }
  try {
    await revokeRelaySession(row.id_hash)
    await reload(true)
  } catch (e: any) {
    error.value = e?.message ?? String(e)
  } finally {
    const next = { ...revoking.value }
    delete next[row.id_hash]
    revoking.value = next
  }
}

async function onSignOutOthers() {
  if (!window.confirm(t('settings.devices.signOutOthersConfirm'))) return
  signingOutOthers.value = true
  try {
    await signOutOtherRelaySessions()
    await reload(true)
  } catch (e: any) {
    error.value = e?.message ?? String(e)
  } finally {
    signingOutOthers.value = false
  }
}

function formatTime(unixMs: number): string {
  const d = new Date(unixMs)
  return t('settings.devices.timeFormat', {
    y: d.getFullYear(),
    m: d.getMonth() + 1,
    d: d.getDate(),
    hh: String(d.getHours()).padStart(2, '0'),
    mm: String(d.getMinutes()).padStart(2, '0'),
  })
}
```

Template (structure):
```html
<div class="tab-pane">
  <div v-if="loading" class="dim">{{ t('common.loading') }}</div>
  <template v-else>
    <div class="header-row">
      <p class="hint">{{ t('settings.devices.hint') }}</p>
      <div class="header-actions" v-if="!notAuthed">
        <button
          type="button"
          class="icon-btn"
          :disabled="refreshing"
          :aria-label="t('settings.devices.refresh')"
          @click="onRefresh"
        >
          {{ refreshing ? '…' : '⟳' }}
        </button>
        <button
          type="button"
          class="secondary"
          :disabled="signingOutOthers || rows.filter(r => !r.is_current).length === 0"
          @click="onSignOutOthers"
        >
          {{ signingOutOthers
              ? t('settings.devices.signingOut')
              : t('settings.devices.signOutOthers') }}
        </button>
      </div>
    </div>

    <p v-if="notAuthed" class="hint">{{ t('settings.devices.notAuthenticated') }}</p>
    <p v-else-if="error" class="error">{{ error }}</p>
    <p v-else-if="rows.length === 0" class="dim">{{ t('settings.devices.empty') }}</p>

    <div v-else class="device-list">
      <div v-for="row in rows" :key="row.id_hash" class="device-row">
        <div class="device-info">
          <div class="ua">{{ row.user_agent || t('settings.devices.unknownUA') }}</div>
          <div class="meta">
            {{ t('settings.devices.loginLine', { time: formatTime(row.created_at), ip: row.ip_prefix }) }}
          </div>
        </div>
        <span v-if="row.is_current" class="current-tag">{{ t('settings.devices.currentTag') }}</span>
        <button
          v-else
          type="button"
          class="danger-btn"
          :disabled="!!revoking[row.id_hash]"
          @click="onRevoke(row)"
        >
          {{ revoking[row.id_hash]
              ? t('settings.devices.revoking')
              : t('settings.devices.revoke') }}
        </button>
      </div>
    </div>
  </template>
</div>
```

Scoped styles: `.header-row` (flex space-between), `.header-actions` (flex
gap 8px), `.icon-btn` (28×28 square, transparent, border 1px), `.secondary`
(reuse from Relay tab pattern), `.device-list` (border-top hairline),
`.device-row` (flex align-center, padding 10px 0, border-top 1px on all but
first), `.ua` (font-size 13, fg), `.meta` (font-size 12, fg-dim, mt 2px),
`.current-tag` (fg-dim, small pill, border 1px, padding 2px 8px), reuse
`.danger-btn` from `SettingsRelay.vue`'s existing pattern.

### `SettingsDialog.vue` integration

Add a new tab entry `{ id: 'devices', label: t('settings.tabs.devices') }`
into the tab list. Position: after `relay`, before `plugins`. Guard with
`v-if="caps.relay ?? true"` (or the same gate the `relay` tab uses; if the
relay tab is unconditional, so is this).

Add `<SettingsDevices v-if="activeTab === 'devices'" />` inside the panel
region alongside the other `v-if="activeTab === ..."` renders.

### i18n keys (en + zh-CN)

Under `settings.tabs.*`:

| key | en | zh-CN |
|---|---|---|
| `devices` | `Signed-in devices` | `已登录设备` |

Under `settings.devices.*`:

| key | en | zh-CN |
|---|---|---|
| `hint` | `Devices signed in to this relay account.` | `此账号已登录的浏览器 / 桌面 / 移动设备。` |
| `refresh` | `Refresh` | `刷新` |
| `signOutOthers` | `Sign out other devices` | `登出其他设备` |
| `signingOut` | `Signing out…` | `登出中…` |
| `signOutOthersConfirm` | `Sign out every device except this one?` | `确定要登出除本机外的所有设备吗？` |
| `revoke` | `Revoke` | `撤销` |
| `revoking` | `Revoking…` | `撤销中…` |
| `revokeConfirm` | `Revoke this device ({ua})?` | `确定要撤销此设备 ({ua}) 吗？` |
| `currentTag` | `Current` | `当前` |
| `loginLine` | `Signed in {time} · {ip}` | `登录于 {time} · {ip}` |
| `timeFormat` | `{y}-{m}-{d} {hh}:{mm}` | `{y}年{m}月{d}日 {hh}:{mm}` |
| `empty` | `No signed-in devices yet.` | `尚未有登录设备。` |
| `unknownUA` | `Unknown device` | `未知设备` |
| `notAuthenticated` | `Log in to your relay account first (Settings → Relay).` | `请先在"Relay"页登录 relay 账号。` |

## Data flow — end-to-end

Happy path A: user opens the tab while signed in.

1. `SettingsDialog` switches `activeTab='devices'`; `SettingsDevices` mounts.
2. `onMounted → reload()` sets `loading=true` and calls `listRelaySessions()`.
3. Backend `ListRelaySessions` reads token from `cfg`, GETs
   `/api/me/sessions`, parses response, returns.
4. Frontend renders one `device-row` per entry. The row whose `is_current`
   is true shows the "当前" tag instead of the revoke button.

Happy path B: user revokes another device.

1. Click revoke → `window.confirm` → OK.
2. `revoking[id_hash]=true`, spinner label on the row's button.
3. `revokeRelaySession(id_hash)` → backend `DELETE /api/me/sessions/{id}`.
4. Success → `reload(silent=true)` — refetches without the full-panel
   loading placeholder.
5. Row disappears (relay's next list no longer contains it).

Happy path C: sign out all others.

1. Click "登出其他设备" → `window.confirm` → OK.
2. `signingOutOthers=true`, button shows "登出中…".
3. `signOutOtherRelaySessions()` → backend POST.
4. Success → `reload(silent=true)`. List shrinks to just the current row.

Failure paths:

- **Not authenticated** (token empty or 401): backend returns
  `"not authenticated"` (empty token) or `"session expired, please log
  in again"` (401). Frontend maps both to the `notAuthed` state; header
  actions hide; a friendly hint copy appears telling the user to log in
  from the Relay tab.
- **Network error / relay 5xx**: `error.value = message`; refresh button
  stays visible; user can retry.
- **Revoke fails**: same error surface; row button re-enabled (finally
  block clears `revoking[id_hash]`).

## Testing

### Backend

`desktop/relay_sessions_test.go` (new file):

- `TestListRelaySessions_ParsesRows`: httptest server returns
  `[{"id_hash":"h1","user_agent":"UA","ip_prefix":"1.2.3","created_at":1700000000000,"expires_at":1710000000000,"is_current":true}]`;
  seed cfg with URL + token; call `ListRelaySessions()`; assert one row
  with those exact fields.
- `TestListRelaySessions_EmptyTokenReturnsError`: cfg has token=""; assert
  the returned error mentions "not authenticated" AND the httptest server
  was NOT hit (use a `t.Fatal(...)`-handler pattern).
- `TestListRelaySessions_401_ReturnsFriendlyError`: httptest returns 401;
  assert error string contains "session expired" or "log in again".
- `TestRevokeRelaySession_DELETEsCorrectPath`: httptest handler asserts
  `r.Method == "DELETE"` and `r.URL.Path == "/api/me/sessions/abc"`;
  responds 204; call `RevokeRelaySession("abc")`; assert no error.
- `TestSignOutOthers_ParsesDeletedCount`: httptest returns `{"deleted":3}`;
  call `SignOutOtherRelaySessions()`; assert `result.Deleted == 3`.

### Frontend

`desktop/frontend/src/components/__tests__/SettingsDevices.test.ts` (new):

- `renders one row per session; is_current shows the Current tag`: mock
  `listRelaySessions` to return two rows, one with `is_current=true`;
  mount; assert `.current-tag` on the correct row and `.danger-btn` on
  the other.
- `clicking refresh calls listRelaySessions again`: mount, wait for
  initial load, click refresh, assert spy called twice.
- `revoke confirm=true → API called then reload`: stub confirm=true; click
  the revoke button on the non-current row; assert `revokeRelaySession`
  called with the correct `id_hash`; assert `listRelaySessions` called
  twice total (initial + post-revoke).
- `revoke confirm=false → API not called`: same setup with confirm=false;
  assert `revokeRelaySession` not called.
- `sign-out-others confirm=true → API + reload`: assert
  `signOutOtherRelaySessions` called; assert `listRelaySessions` called
  twice.
- `not-authenticated error switches to hint copy`: mock
  `listRelaySessions` to reject with `Error("not authenticated")`; mount;
  assert the panel shows `settings.devices.notAuthenticated` key and no
  header action buttons.

## Risks & mitigations

- **URL path encoding**: revoke uses `url.PathEscape(idHash)` on the Go
  side. The idHash is short base64-style; PathEscape is safe.
- **Token races**: if the user logs out from the Relay tab while the
  Devices tab is open, the next reload will get 401 → `notAuthed=true`.
  Acceptable.
- **Revoking the current session**: the current row shows no revoke
  button, so this cannot happen via the UI. If a caller invokes the
  binding directly, the relay's own endpoint refuses (returns 4xx),
  which we surface as an error.
- **Empty user_agent**: the relay may return empty string for
  server-to-server tokens. UI falls back to `unknownUA`.
- **Time formatting**: uses local time via `new Date(unixMs)`. No TZ
  parameter — matches the web behavior.
- **Existing `relayHTTPClient` / `relayHTTPBase` signatures**: the
  implementer must verify these exact names in
  `desktop/relay_httpclient.go` before landing. If the actual helpers
  are named differently (e.g. `newRelayHTTPClient`), the plan adjusts.
