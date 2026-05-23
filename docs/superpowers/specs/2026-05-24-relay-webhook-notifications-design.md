# Relay Outbound Webhook on Command-Finish — Design

Date: 2026-05-24
Status: Draft (pending implementation plan)

## Background

The product's core remote value is "leave your desk, get notified when the long AI task finishes." On the relay, command-finish is already detected: the desktop/agent reports OSC 133 command-finished events up the uplink, and `internal/relay/uplink_conn.go` decodes a `command_event` frame and calls `s.cfg.WebPush.DispatchCommandFinished(ownerUserID, …)` — fanning the event to the user's browser Web Push subscriptions.

Native mobile push (APNs/FCM) is expensive and untestable without Apple/Firebase accounts and devices. An **outbound webhook** sidesteps all of that: when a command finishes, the relay POSTs to a user-configured URL (e.g. a Feishu/Lark custom-bot webhook), and the user gets the message through that service's own app + push. It's server-side, fully testable (POST to a mock, assert the body), and delivers the away-from-desk value regardless of the mobile client's state.

This subsystem mirrors the existing `internal/webpush/` package and the `/api/me/tokens` per-user CRUD pattern. It is **independent of the mobile platform stack** (PR-A/B/C) and branches off `main`.

## Goals

- Per-user webhook configs, managed via `/api/me/webhooks` (GET/POST/DELETE), mirroring `/api/me/tokens`.
- On command-finish, POST to each of the session owner's webhooks, alongside the existing Web Push dispatch.
- Two render formats per webhook: **Feishu** custom-bot (`{"msg_type":"text","content":{"text":"…"}}`) and **generic** (raw JSON event).
- Async fan-out (never blocks the uplink read loop); per-POST timeout; failures logged, not retried in a storm.
- A `web/` settings "Webhooks" tab to add/delete webhooks (URL + format + name).
- Fully unit-testable in this environment (mock HTTP transport).

## Non-Goals

- Native mobile push (APNs/FCM) — separate effort, environment-blocked.
- Mobile (`desktop/frontend`) webhook-management UI — the browser settings is the per-user config home for now; the mobile client can surface it later.
- Retry/backoff/delivery guarantees — best-effort fire-and-log (matches web-push dispatch semantics).
- Webhook configs surviving as anything other than per-user rows (no admin-global webhook).
- Slack/Discord/other formats — only Feishu + generic now; the `format` field leaves room to add more.
- Inbound webhooks / webhook signature verification of incoming requests (this is outbound only).

## Constraints

- AGENTS.md red-line 4 (wire compat): no protocol change — `command_event` already exists; we add a consumer.
- AGENTS.md red-line 9 (relay security): webhook URLs are user-provided → an SSRF surface (see Security). Require `https://` by default; `http://` only with an explicit per-webhook insecure flag. The relay's outbound POST has a short timeout and no redirect-following beyond defaults.
- AGENTS.md "Store interface change" rule: adding webhook CRUD to `userstore.Store` requires updating the SQLite impl, the memory impl, and `store_iface_test.go`.
- Go 1.23; mirror existing `internal/webpush/` structure and `internal/userstore/apitokens.go` patterns.
- No new third-party Go dependencies (use `net/http` + `encoding/json`).

## Architecture

```
desktop/agent ──OSC133 command_event──▶ relay uplink_conn.go
                                              │ decode payload {ExitCode, ElapsedMS, Label}
                                              ├─▶ WebPush.DispatchCommandFinished(ownerUserID, ev)   (existing)
                                              └─▶ Webhook.DispatchCommandFinished(ownerUserID, ev)   (NEW)
                                                       │ Store.ListWebhooks(ownerUserID)
                                                       ▼  per webhook, in a goroutine:
                                                  render(format, ev) → POST url (timeout) → log result
```

### `internal/webhook/` package (new, mirrors `internal/webpush/`)

- `service.go` — `Service` holds a `Store` reference (for `ListWebhooks`) + an `*http.Client` with timeout. `New(store WebhookStore, opts…) *Service`.
- `dispatch.go` — `type CommandFinished struct { SessionID uuid.UUID; HostID uuid.UUID; ExitCode int; ElapsedMS int; Label string }` (same shape as `webpush.CommandFinished`) + `func (s *Service) DispatchCommandFinished(ownerUserID string, ev CommandFinished)` — returns immediately, fans out in goroutines.
- `render.go` — `func renderFeishu(ev CommandFinished) []byte` → `{"msg_type":"text","content":{"text":"<host> · <label> finished (exit <code>, <elapsed>)"}}`; `func renderGeneric(ev CommandFinished) []byte` → JSON of the event fields. Pure functions, unit-tested.
- `transport.go` — `func (s *Service) post(url string, body []byte) error` — `POST` with `Content-Type: application/json`, client timeout (e.g. 8s), returns error on non-2xx; caller logs.
- `WebhookStore` interface (the subset of `userstore.Store` the service needs): `ListWebhooks(ctx, userID) ([]Webhook, error)`. The relay passes the real `userstore.Store`.

### `internal/userstore/webhooks.go` (new, mirrors `apitokens.go`)

- `type Webhook struct { ID, UserID, URL, Format, Name string; AllowInsecure bool; CreatedAt int64 }`.
- `CreateWebhook(ctx, userID, url, format, name string, allowInsecure bool) (*Webhook, error)`.
- `ListWebhooks(ctx, userID) ([]Webhook, error)`.
- `DeleteWebhook(ctx, webhookID, userID) error` (ownership-scoped).
- Migration: a `webhooks` table (`id TEXT PK, user_id TEXT, url TEXT, format TEXT, name TEXT, allow_insecure INTEGER, created_at INTEGER`), cascade-delete on user delete (mirror `api_tokens`).
- Add the three methods to the `Store` interface; implement in SQLite + memory stores; extend `store_iface_test.go`.

### `/api/me/webhooks` HTTP (in `auth_http.go`, mirrors token routes)

- `GET /api/me/webhooks` → `[]Webhook` (no secrets to hide; URL is shown).
- `POST /api/me/webhooks` (CSRF) body `{url, format, name, allow_insecure}` → 201 `{id, url, format, name, created_at}`. Validates: `format ∈ {feishu, generic}`; URL parseable + scheme `https://` (or `http://` with `allow_insecure=true`). No private-IP/host blocklist — per the single-user self-hosted model (see Security), scheme validation is the only URL gate.
- `DELETE /api/me/webhooks/{id}` (CSRF) → 204.

### Dispatch wiring (`uplink_conn.go`)

At the existing site (currently ~line 368), after the `WebPush.DispatchCommandFinished` call, add:

```go
if s.cfg.Webhook != nil {
    s.cfg.Webhook.DispatchCommandFinished(ms.sess.OwnerUserID, webhook.CommandFinished{
        SessionID: f.SessionID, HostID: hostID,
        ExitCode: payload.ExitCode, ElapsedMS: payload.ElapsedMS, Label: payload.Label,
    })
}
```

`Server.Config` gains `Webhook *webhook.Service` (nil-safe, like `WebPush`).

### `cmd/atterm-relay/main.go`

Construct `webhook.New(store)` and assign `cfg.Webhook = whSvc`, beside the existing `cfg.WebPush = wpSvc`.

### `web/` settings "Webhooks" tab

- New `web/src/settings/tabs/Webhooks.vue` + a shared `web/src/shared/api/webhooks.ts` (apiFetch wrappers for the three endpoints).
- UI: list existing (name · format · url · delete); add form (name, format select [Feishu | Generic], URL, allow-insecure checkbox). Vitest + @vue/test-utils tested with mocked apiFetch.
- Wire the tab into `web/src/settings/App.vue` tab set.

## Data Flow

```
command finishes on host → agent emits OSC133 → uplink command_event → relay decodes
  → Webhook.DispatchCommandFinished(ownerUserID, ev)
      Store.ListWebhooks(ownerUserID) → [w1(feishu), w2(generic)]
      goroutine: post(w1.url, renderFeishu(ev))   → 200 → log ok  / non-2xx → log warn
      goroutine: post(w2.url, renderGeneric(ev))  → …
```

User configures via web settings:
```
Settings → Webhooks tab → add {name:"phone", format:"feishu", url:"https://open.feishu.cn/...", allow_insecure:false}
  → POST /api/me/webhooks (CSRF) → row persisted
```

## Error Handling Matrix

| Failure | Behavior |
|---|---|
| `ListWebhooks` DB error in dispatch | log error; dispatch is a no-op for this event (never blocks/crashes the uplink loop) |
| webhook POST non-2xx | log WARN with status; keep the webhook (user-owned URL, transient) |
| webhook POST timeout/network error | log WARN; no retry |
| `Webhook` service nil (not wired) | dispatch site guards `if s.cfg.Webhook != nil` |
| POST create: bad format | 400 `{"error":"invalid_format"}` |
| POST create: unparseable URL | 400 `{"error":"invalid_url"}` |
| POST create: http:// without allow_insecure | 400 `{"error":"insecure_url"}` |
| DELETE non-owned id | 404 (ownership scope, like tokens) |

## Security

- **SSRF**: outbound POST to a user-supplied URL. For a self-hosted relay the user targets their own chosen endpoint, so the risk is bounded, but the design (a) requires `https://` unless `allow_insecure` is explicitly set, (b) uses a short timeout, (c) does not expose the response body to the user (only logs status server-side, so it can't be used as a blind-SSRF oracle returning data). A stricter private-IP blocklist is **out of scope** for the single-user self-hosted model but noted as a future hardening if multi-tenant.
- **CSRF**: create/delete are CSRF-gated like all mutating `/api/me/*` routes.
- **Ownership**: dispatch only to the session **owner's** webhooks (`OwnerUserID`); delete scoped to the requesting user.
- **No secrets in list**: webhook URLs may embed a token (Feishu URLs do). `GET /api/me/webhooks` returns the full URL to its owner only (same trust level as the owner having set it). Not logged in plaintext server-side beyond debug.

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `webhook/render.go` | Feishu + generic body shape for a sample event | Go unit test, assert JSON |
| `webhook/transport.go` | POST sends body + Content-Type; non-2xx → error; timeout → error | `httptest.Server` |
| `webhook/dispatch.go` | fans out to all of the user's webhooks; returns immediately; DB error → no panic | Go test with a fake store + httptest |
| `userstore/webhooks.go` | Create/List/Delete; ownership; cascade on user delete | Go test (SQLite temp + memory) |
| `store_iface_test.go` | new methods covered by the shared iface conformance test | extend existing |
| `/api/me/webhooks` | GET/POST/DELETE happy + CSRF gate + validation 400s + ownership 404 | Go httptest, mirror token tests |
| dispatch wiring | command_event → webhook fired (with the service mocked/fake) | extend uplink_conn test |
| `web/` Webhooks.vue | render list, add form posts, delete, format validation | Vitest + @vue/test-utils, mocked apiFetch |
| `cmd/atterm-relay` | webhook service constructed + wired (smoke) | existing main_test or a light check |

## File Structure

**New (Go):** `internal/webhook/{service,dispatch,render,transport}.go` + tests; `internal/userstore/webhooks.go` + test + a migration in `internal/userstore/migrations/`.
**Modified (Go):** `internal/userstore/store.go` (iface + add to memory impl), `internal/userstore/store_iface_test.go`, `internal/relay/server.go` (`Config.Webhook`), `internal/relay/auth_http.go` (routes + handlers), `internal/relay/uplink_conn.go` (dispatch call), `cmd/atterm-relay/main.go` (wire service).
**New (web):** `web/src/settings/tabs/Webhooks.vue`, `web/src/shared/api/webhooks.ts` + tests.
**Modified (web):** `web/src/settings/App.vue` (add tab).
**Docs:** `AGENTS.md` "何时改哪里" row for webhook config; update `docs/spec/` only if a protocol doc needs it (it does not — no wire change).

**NOT touched:** `internal/webpush/` (parallel, untouched), the mobile platform stack (PR-A/B/C), `internal/proto/` (no frame change).

## Risks & Open Questions

- **Command-finish requires shell integration (OSC 133).** The webhook only fires for sessions where OSC 133 markers reach the relay (shell-integration enabled on the host). Sessions without it never emit `command_event`, so no webhook — same limitation the existing Web Push already has. Acceptable; documented.
- **Feishu message format may evolve** (text vs interactive card). PR ships `text`; richer cards are a follow-up via the `format` field.
- **Per-user webhook in a single-user deployment** is slight over-engineering vs admin-global, but matches the existing per-user web-push semantics and `DispatchCommandFinished(ownerUserID, …)` signature, so it's the lower-friction path and avoids a second config model.
- **`elapsed`/`label` formatting** for the human-readable Feishu text — reuse the desktop's `formatElapsed` semantics (ms → "2.3s"/"1m04s"); implement a small Go helper, unit-tested.
