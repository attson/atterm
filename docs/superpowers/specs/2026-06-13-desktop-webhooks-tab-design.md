# Desktop Webhooks Tab — Design

## Goal

Surface relay-side per-user webhook configuration inside the desktop
SettingsDialog (and, by extension, the iOS Capacitor build that bundles
`desktop/frontend`). Today webhook management lives only at
`web/src/settings/tabs/Webhooks.vue` served by the relay; desktop and
mobile clients have to open a browser to manage them. This puts the same
list/create/delete UI behind a new `Webhooks` tab that talks to the
relay's existing `/api/me/webhooks` endpoints.

Webhooks are user-scoped: they live in the relay sqlite `webhooks` table
keyed by `user_id`. The desktop never persists them locally — opening
the tab fetches live, logging out / unpairing immediately makes them
inaccessible.

## Non-goals

- No change to the relay backend (`internal/userstore/webhooks.go`,
  `internal/relay/auth_http.go`, `internal/webhook/*`). Contract is
  stable.
- No removal of the existing web settings Webhooks tab. Browser entry
  point stays.
- No "send test event" button. Scope creep; revisit later if needed.

## Architecture

```
[SettingsDialog tab "webhooks"]
        │
[SettingsWebhooks.vue]
        │
[platform.relay.webhooks.{list,create,delete}]   ← new WebhookBridge
   ┌────┴───────────────────┐
   │ wails                  │ capacitor
   ▼                        ▼
lib/api.ts              fetch(relayURL/api/me/webhooks, Bearer)
   │                        │
[Wails binding]              │
   │                        │
desktop/app_webhooks.go ─────┴──→ relay /api/me/webhooks {GET,POST,DELETE}
                                  ↓
                            internal/userstore/webhooks.go
```

The desktop wails path follows the same pattern as `App.FetchRelayMe`
and `prefssync_adapter.go`: Go side reads token + `allow_insecure_relay`
from `cfgStore`, builds the `http.Request`, returns a typed struct.

The Capacitor path follows `capacitor.ts → fetchMe / listRemoteSessions`:
read cfg from secure storage, `fetch(base + path, { Authorization: Bearer })`
directly. iOS ATS blocks insecure http; no `allow_insecure` plumbing on
that side.

## Components

### New files

| Path | Purpose |
| --- | --- |
| `desktop/app_webhooks.go` | `Webhook` struct; `ListWebhooks()`, `CreateWebhook(req)`, `DeleteWebhook(id)` methods on `*App`. Forwards to relay; uses `cfg.RelaySessionToken` Bearer; respects `cfg.AllowInsecureRelay`. |
| `desktop/app_webhooks_test.go` | `httptest` mock relay: Bearer header, URL routing, error code passthrough, insecure TLS. |
| `desktop/frontend/src/components/SettingsWebhooks.vue` | Tab body: list (cards + delete), create form (name / url / format select / allow_insecure checkbox), empty state, error banners. |
| `desktop/frontend/src/components/SettingsWebhooks.test.ts` | Vitest + @vue/test-utils component tests. |

### Modified files

| Path | Change |
| --- | --- |
| `desktop/app.go` | Register new bindings (next to `FetchRelayMe`). |
| `desktop/frontend/src/lib/api.ts` | Add types `Webhook`, `CreateWebhookReq`; `AppBindings` interface gains three methods; export thin `listWebhooks() / createWebhook() / deleteWebhook()` wrappers. |
| `desktop/frontend/src/platform/types.ts` | `RelayBridge.webhooks?: { list, create, delete }` optional sub-object. |
| `desktop/frontend/src/platform/wails.ts` | Wire `webhooks` to `lib/api`. |
| `desktop/frontend/src/platform/capacitor.ts` | Implement `webhooks.{list,create,delete}` with direct `fetch`; 401 → throw `'relay_unauthorized'` like `listRemoteSessions`. |
| `desktop/frontend/src/components/SettingsDialog.vue` | Add `webhooks` tab with `v-if="relayConfig?.url && relayConfig?.token"`. |
| `desktop/frontend/src/components/SettingsDialog.test.ts` | Verify tab visibility gating. |
| `desktop/frontend/src/i18n/messages/en.ts` & `zh-CN.ts` | Add `settings.tabs.webhooks` and `settings.webhooks.*`. |

### Unchanged

`internal/userstore/webhooks.go`, `internal/relay/auth_http.go`,
`internal/webhook/*`, `web/src/settings/tabs/Webhooks.vue`,
`web/src/shared/api/webhooks.ts`.

## Data flow

**List** (tab open): `onMounted → platform.relay.webhooks.list() → Webhook[]`.
Empty array renders empty state.

**Create**: form validate (non-empty name, URL parses as http(s)) →
`create(req)` → server returns the row → unshift into local ref, clear
form. Errors leave form filled, show inline banner.

**Delete**: NDialog confirm → `delete(id)` → splice from local ref on
success. Error leaves row in place with inline message.

**Auth state change**: `SettingsRelay` already emits
`relay-config-changed` on save. `SettingsDialog` re-reads `relayConfig`
on that event, so tab appears/disappears across login/logout without
a new event.

## Tab visibility

Tab renders only when `relayConfig.url && relayConfig.token` (a session
token is held). Uplink WS pause state is *not* checked — webhook config
is an account-level setting, manageable while uplink is paused.

## Error handling

| Layer | Behavior |
| --- | --- |
| HTTP 401 | Go: returns wrapped error string `relay_unauthorized`. Frontend clears local token via existing `clearRelayConfig` path and shows "go re-login" hint. Capacitor: throws `'relay_unauthorized'`, same handling. |
| HTTP 4xx with `{error: code}` | Pass code through. i18n: `settings.webhooks.errors.<code>` with fallback `…errors.generic`. |
| HTTP 5xx / network | `settings.webhooks.errors.network`. Button returns to idle, retry allowed. |
| TLS / `allow_insecure` | Wails Go side respects `cfg.AllowInsecureRelay` exactly like `prefssync_adapter`. Capacitor: not plumbed; iOS ATS already blocks plain-http. |

## Testing

**Go (`app_webhooks_test.go`)** — httptest mock relay:
- `ListWebhooks`: GET path, Bearer injection, JSON decode.
- `CreateWebhook`: POST body shape, 4xx error code passthrough.
- `DeleteWebhook`: path escaping, 404 mapping.
- insecure TLS: `allow_insecure_relay=true` connects to self-signed.

**Frontend (`SettingsWebhooks.test.ts`)** — Vitest:
- Empty state copy renders when list returns `[]`.
- N rows render for N-item list.
- Invalid URL disables submit; valid URL enables.
- Successful create prepends row, clears form.
- Failed create surfaces inline error.
- Delete confirmation + success removes row.

**Dialog (`SettingsDialog.test.ts`)** — add: tab absent when
`relayConfig.token` is empty; present when set.

No e2e — matches the granularity of existing Settings tab tests.

## i18n keys to add

```
settings.tabs.webhooks
settings.webhooks.title
settings.webhooks.empty
settings.webhooks.addTitle
settings.webhooks.fields.name
settings.webhooks.fields.url
settings.webhooks.fields.format
settings.webhooks.fields.allowInsecure
settings.webhooks.formats.generic
settings.webhooks.formats.feishu
settings.webhooks.actions.create
settings.webhooks.actions.delete
settings.webhooks.actions.confirmDelete
settings.webhooks.errors.generic
settings.webhooks.errors.network
settings.webhooks.errors.unauthorized
settings.webhooks.errors.invalid_url
settings.webhooks.errors.too_many
```

Match web/src/shared/i18n/messages copy where existing keys overlap, to
keep translations consistent across the two surfaces.
