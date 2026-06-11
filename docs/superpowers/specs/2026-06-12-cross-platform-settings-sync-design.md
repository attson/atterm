# Cross-platform settings sync

## Problem

Desktop, mobile, and web each persist a user's preferences independently — sometimes in inconsistent storage layers — and there is no mechanism for changes on one device to propagate to another. Users who customize their locale, quick templates, or notification settings on one device have to redo the same configuration on every other device.

The relay account is the natural identity. The relay server already authenticates users and is reachable from all three clients, but it currently stores zero user preferences. This spec adds a small set of synced preferences keyed by the relay user id.

## Scope

### Synced fields (5)

| Key | Type | Source today |
|---|---|---|
| `locale_preference` | string (`system` / `en` / `zh-CN`) | desktop `config.json`; mobile/web localStorage `atterm.locale` |
| `quick_templates` | array of template objects | desktop `config.json`; mobile/web localStorage `atterm.templates` |
| `notifications_enabled` | bool | desktop `config.json` only |
| `command_notify_threshold_seconds` | int | desktop `config.json` only |
| `shell_integration_enabled` | bool | desktop `config.json` only |

### Out of scope

- All device-local capability/UI fields stay device-local and never sync: terminal theme, default shell, logging, auto-update, plugins, window/sidebar geometry, aux-keys, WebGL renderer, relay url/token/auth, `relay_paused`, `remote_permission`.
- Real-time push (WebSocket) — not needed for settings; pull-on-foreground + push-on-change is sufficient.
- Sync history / undo, schema versioning beyond a server-side key whitelist, encrypted preference storage.

## Decisions

- **Source of truth**: relay server, but only when the user is logged in. Logged-out desktop continues to read/write `config.json` with no sync.
- **Conflict policy**: per-field last-write-wins by `updated_at` (millisecond epoch).
- **Sync trigger**: PULL on app start / foreground / login success; PUSH on every change (500 ms debounce).
- **Server storage shape**: per-key KV rows — `user_preferences(user_id, key, value_json, updated_at)`.

## Architecture

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│   Desktop    │    │    Mobile    │    │     Web      │
│ Go config +  │    │ LS + Keychain│    │     LS       │
│  Vue front   │    │  (Capacitor) │    │              │
└──────┬───────┘    └──────┬───────┘    └──────┬───────┘
       │  REST/JSON         │  REST/JSON        │  REST/JSON
       │  Bearer token      │                   │
       └────────────────────┴───────────────────┘
                            │
                  ┌─────────▼──────────┐
                  │   atterm-relay     │
                  │  /api/preferences  │
                  │  sqlite KV table   │
                  └────────────────────┘
```

Each client's existing local storage stays the on-disk authority for offline use. A new sync engine in each client reconciles local state with the server, using a small amount of metadata (`updated_at_local`, `dirty`) stored next to each synced value.

## Server: data + API

### Table

New SQLite table in the relay's userstore:

```sql
CREATE TABLE user_preferences (
  user_id     TEXT    NOT NULL,
  key         TEXT    NOT NULL,
  value_json  TEXT    NOT NULL,
  updated_at  INTEGER NOT NULL,  -- ms epoch
  PRIMARY KEY (user_id, key)
);
CREATE INDEX user_preferences_user ON user_preferences(user_id);
```

`value_json` is the JSON-encoded value for that key (a string, bool, number, or array). The server does not interpret values beyond a basic type check against the whitelist.

### Endpoints

Both endpoints require an authenticated relay session (bearer token), and operate on the calling user only.

```
GET  /api/preferences
  → 200 {
      "items": [
        { "key": "locale_preference",
          "value": "zh-CN",
          "updated_at": 1733900000123 },
        ...
      ]
    }
```

Returns every row the server has for this user. Returns an empty list if the user has never synced.

```
PUT  /api/preferences
     body: {
       "items": [
         { "key": "locale_preference",
           "value": "zh-CN",
           "client_updated_at": 1733900001000 },
         ...
       ]
     }
  → 200 {
      "items": [ ...full current server state, one entry per key the user has... ]
    }
```

For each incoming item:

1. Reject (400) if `key` is not in the whitelist or `value` fails its type check.
2. Load the row's current `updated_at` (default 0 if no row exists).
3. If `client_updated_at > current.updated_at`: write the new value, with `updated_at = max(client_updated_at, now_ms)` (clock-skew safety — `updated_at` is monotonic per row).
4. If `client_updated_at <= current.updated_at`: ignore this item (server is newer).
5. Always include the row's resulting current state in the response, so the client can reconcile rejected pushes.

The response also includes any keys the server has that the client did not send — so a PUT doubles as a PULL refresh.

### Whitelist + forward compat

The server hard-codes the current 5 keys and their types. Unknown keys → 400 on PUT, never returned in GET. New fields require a relay version bump.

Old clients that GET an unknown key (because the server has been upgraded with new fields) **must ignore that key** without writing it into local cache or wiping it on next PUT. The PUT body sends only keys the client knows; unsent keys are left untouched server-side.

## Client: sync engine

Every client implements the same state machine. Per synced field the client stores:

```
{ value, updated_at_local, dirty }
```

`updated_at_local` is millisecond epoch; `dirty` means "has a local change that hasn't been confirmed by the server".

### PULL (app start / foreground / login success)

```
GET /api/preferences
for each item in response:
  if item.updated_at > local.updated_at_local:
    local.value = item.value
    local.updated_at_local = item.updated_at
    local.dirty = false
  else if local.dirty and local.updated_at_local > item.updated_at:
    leave local alone — push will reconcile
  else:
    no-op
```

Keys the server doesn't return are left alone locally (first sync from a fresh device leaves nothing to compare).

### Local change

```
local.value = new_value
local.updated_at_local = client_now_ms()
local.dirty = true
schedule debounce(500ms) → PUSH
```

The UI reflects the new value immediately; sync is asynchronous.

### PUSH

```
PUT /api/preferences { items: [all dirty fields] }

on 2xx:
  for each item in response:
    if local field is in response and local.dirty was set for it:
      local.value = item.value           # server may have accepted or overridden
      local.updated_at_local = item.updated_at
      local.dirty = false
    elif local field is in response and not dirty:
      reconcile as in PULL

on network error / 5xx:
  keep dirty, retry on next foreground or login
  backoff 1s → 5s → 30s, only ticks while app is foreground

on 401:
  fire 'relay:session-expired' event, stop sync; do not clear dirty

on 400 (validation):
  log; drop dirty flag for that field to avoid infinite retries
  (this means a schema mismatch — user must update client)
```

### Trigger points per platform

- **Desktop**: Wails `OnFrontendLoaded` → PULL. The Go-side `AppSettings.Update*` mutators for the 5 synced fields emit a `prefChanged(key)` event consumed by `internal/prefssync`, which debounces and pushes.
- **Mobile** (Capacitor): `App.appStateChange → isActive=true` → PULL. Vue `useSettingsStore` setters call `prefsSync.localUpdate(key, value)` which marks dirty + debounces.
- **Web**: page load + `window.focus` → PULL. Same store setter path as mobile.
- **All**: relay login success → PULL once.

### Local + logout/login

- **Not logged in**: sync engine is idle. Local storage works as today.
- **Logout**: stop the sync engine; clear `dirty` flags and `updated_at_local` metadata; leave `value`s alone so the user keeps their current settings.
- **Login**: trigger a PULL. Any local value that was changed while logged out keeps its current state and will be pushed up on the next user-driven change or on next foreground via the seed-upload path described below.
- **Switch accounts** (logout then login as different user): on logout, drop all sync metadata as above. On login, PULL the new user's prefs. Server fields overwrite local; locally-only fields are untouched (so we don't accidentally bleed account A's settings up to account B).

### First-time login seed upload

On the first PULL after a user logs in on this device (detected via a `prefssync_seeded:<userId>` marker stored next to local prefs), if the server returns no entry for a known key but the local value differs from the field's hard-coded default, mark that field dirty so the next debounce flushes it up. After the seed pass completes, write the marker.

## Per-platform implementation map

### Desktop (Go + Vue)

- New package `internal/prefssync/` with:
  - `Engine` struct holding a relay client, a local-cache adapter wrapping `AppSettings`, dirty queue, debounce timer, retry state.
  - `Start(ctx)` runs on app boot; subscribes to `AppSettings` change events.
  - `OnLoginChange(loggedIn bool)` from `relay/client.go` triggers PULL or stops the engine.
- `internal/appsettings/appsettings.go`:
  - Add sidecar `prefs_meta` map persisted in `config.json` storing `{key: {updated_at_local, dirty}}` for the 5 synced fields only.
  - Mutators for synced fields publish change events.
- Frontend: no UI changes. The Vue settings panel keeps using the existing wails RPC to read/write.

### Mobile / Web (shared TS)

- New module `desktop/frontend/src/lib/prefsSync.ts`:
  - State machine per field; reads/writes from existing `useSettingsStore`.
  - Metadata sidecars in localStorage: `atterm.<key>.meta` → `{ updated_at_local, dirty }`.
  - HTTP via existing `relay/api.ts` (adds two helpers: `getPreferences`, `putPreferences`).
- `useSettingsStore` setters wrapped to call `prefsSync.localUpdate(key, value)` for synced fields. Non-synced fields continue current path.
- Lifecycle wiring:
  - Web: `prefsSync.start()` called from `main.ts` after `useSettingsStore` is hydrated; `window.addEventListener('focus', () => prefsSync.pull())`.
  - Mobile: `Capacitor.App.addListener('appStateChange', s => s.isActive && prefsSync.pull())`.

### Relay

- New table migration (see Schema above).
- `internal/userstore/preferences.go`: CRUD with whitelist + type check + LWW.
- `atterm-relay/api/preferences.go`: HTTP handlers; reuse existing auth middleware.

## Error handling summary

| Situation | Behavior |
|---|---|
| Network failure on PUSH | Keep dirty; retry on next foreground / login; backoff 1s → 5s → 30s |
| 401 | Stop engine, surface `relay:session-expired` event, keep dirty |
| 400 on PUSH | Drop dirty flag for that key (schema mismatch); log |
| Server returns key client doesn't recognize | Ignore in cache, leave server-side untouched on next PUT |
| Client sends a key server doesn't know | Server returns 400; client logs and drops dirty for that field |
| Concurrent edits on two devices | Server-side LWW; loser's PULL/PUSH response overwrites local |
| Clock skew (client ahead) | Server uses `max(client_updated_at, now)`, monotonic |
| Clock skew (client behind) | Server still accepts if `client_updated_at > stored_updated_at`; this is fine |

## Testing

- **Relay (Go)**
  - `internal/userstore/preferences_test.go` — CRUD, whitelist, type checks, LWW arbitration including equal-timestamp ties.
  - `atterm-relay/api/preferences_test.go` — HTTP integration with mock auth, including PUT with mixed accepted/rejected items.
- **Desktop sync (Go)**
  - `internal/prefssync/sync_test.go` — PULL/PUSH/dirty/retry/login/logout/account-switch using a stub relay client; covers first-login seed upload.
  - `internal/appsettings/appsettings_test.go` — `prefs_meta` round-trip, change-event firing for synced fields only.
- **Shared client (TS)**
  - `desktop/frontend/src/lib/prefsSync.test.ts` — state machine, debounce, reconciliation, focus/foreground PULL, schema-mismatch handling. Uses Vitest with mocked fetch.
- **Manual two-device check**
  - Desktop + Web side-by-side: change locale on desktop → bring Web tab to foreground → expect new locale.
  - Web + Mobile: edit a quick template on Web → background then foreground mobile → expect updated list.
  - Offline-then-online: airplane-mode desktop, change two fields, reconnect, expect both pushed.

## Migration

- New SQLite migration adds `user_preferences`. No data backfill — the server starts empty.
- Existing on-device config keeps its current values; sidecar metadata is initialized to `{ updated_at_local: 0, dirty: false }`. First-login seed upload (above) handles getting current customizations onto the server.
- The mobile/web localStorage keys (`atterm.locale`, `atterm.templates`) are reused; new sidecar keys (`atterm.locale.meta`, `atterm.templates.meta`) live alongside them.

## Open follow-ups (deferred)

- Sync status indicator in the settings UI (`✓ synced` / `syncing…` / `offline`). Useful but not blocking.
- WebSocket push for sub-second propagation across devices.
- Sync more fields once the framework is proven (`terminal_theme` will need it once mobile gets a terminal view).
