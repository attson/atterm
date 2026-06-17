# Feishu app-mode integration (relay) — design

Date: 2026-06-17
Status: Drafted — awaiting user review before plan.

## 0. Summary

Replace the current per-user "Feishu custom-bot webhook URL" path with
a proper Feishu self-built application: the relay holds each user's
`app_id` / `app_secret` / `encrypt_key` / `verify_token`, mints
`tenant_access_token` on demand, sends interactive cards into the
user's private chat with the bot, receives Feishu event callbacks
(message subscription + card action triggers), and ties an atterm
user to a Feishu `open_id` through a one-time short-code paired over
the message subscription.

This first cut is relay-backed only and covers two outbound events
(`command_finished`, `waiting_input`) plus the bind flow and card
"ack" callback. Desktop-direct long-connection mode (no relay) and
OAuth user-token flows are deferred milestones.

## 1. Goals

- Replace the existing `Format=feishu` outbound webhook path
  (`internal/webhook/render.go:renderFeishu`) with an app-mode IM
  message that uses richer interactive cards.
- Provide a binding flow that requires no public OAuth redirect and
  no `open_id` lookup in Feishu admin: user pastes app credentials
  into atterm, then sends `/bind <code>` to the bot in Feishu chat.
- Land a card "ack / jump back" round-trip so the half-duplex story
  (read on phone, dismiss without opening desktop) is complete.
- Keep the door open for desktop-direct (M3) and long-connection
  (M4) modes by isolating Feishu specifics in a new `internal/feishu/`
  package.

## 2. Non-goals (deferred milestones)

- Feishu OAuth / `user_access_token` flow (future M5).
- Desktop-direct outbound send without a relay (M3).
- Desktop-side Feishu WebSocket long-connection event subscription
  (M4) — and any "single Feishu app across multiple devices" story
  that mode would force on us.
- Sending commands back into a session from Feishu (touches the E2EE
  trust boundary; needs its own spec).
- Card actions beyond `ack` / `jump-back`: "mute N minutes", "show
  output snippet", "retry command" are explicitly out.
- Group / chat-id receiving (only `receive_id_type=open_id` private
  chat).
- Multiple Feishu accounts per atterm user.
- Desktop / mobile registration of the `atterm://` URL scheme that
  the jump-back button links to. Relay only emits the string; the
  follow-up PR teaches each client to catch it.

## 3. Scope: M0 + M1 + M2 in one spec

| ID | Slice |
|---|---|
| **M0** | `internal/feishu/` library: token cache, IM client, card schemas, event decrypt + verify. Pure Go, no DB. |
| **M1** | Outbound `command_finished` + `waiting_input` → Feishu interactive card, plus the bind data model. Drops `Format=feishu` from the legacy webhook path. |
| **M2** | Inbound `POST /v1/feishu/events/<app_id_hash>` handler: bind-message dispatch, card-action callback responding with an updated card. |

Why bundle: M0 has no user value standalone; M1 cannot bind a user
without M2's event subscription endpoint (binding consumes
`im.message.receive_v1` events). Splitting buys nothing.

Approximate footprint: ~13 new/edited files, ~1.8 k LOC including
tests. Plan breaks this into roughly 8 sequential tasks.

## 4. Architecture

```
                              ┌───────────────────────┐
                              │  Feishu open platform │
                              │  open.feishu.cn       │
                              └───┬─────────────▲─────┘
                       inbound    │             │ outbound
                  HTTPS event POST│             │ IM send API
                                  ▼             │
┌─────────────────────────────────────────────────────────┐
│  internal/relay/                                        │
│  ┌──────────────────┐    ┌─────────────────────────┐   │
│  │ feishu_http.go   │    │  uplink_conn.go         │   │
│  │ /v1/feishu/      │    │  dispatch on            │   │
│  │   events/<hash>  │    │   CMD-EVENT (existing)  │   │
│  │   bindings/...   │    │   + session notif       │   │
│  └────┬─────────────┘    └──────────┬──────────────┘   │
└───────┼─────────────────────────────┼──────────────────┘
        │                             │
        ▼                             ▼
┌─────────────────────────────────────────────────────────┐
│  internal/feishu/    (new package, A from brainstorm)   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │ client.go│ │ token.go │ │ card.go  │ │ event.go │   │
│  │ IM POST  │ │ tenant   │ │ schema + │ │ decrypt  │   │
│  │          │ │ token    │ │ render   │ │ + verify │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
│  ┌──────────────────────────────────────────────────┐  │
│  │ service.go (aggregate; reused by M3 desktop)     │  │
│  │   Send(binding, card)                            │  │
│  │   HandleEvent(rawCiphertext) → ParsedEvent       │  │
│  │   HandleCardCallback(raw) → CardAction + new JSON│  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                                ▲
                                │ Get / Upsert / Consume
                                ▼
┌─────────────────────────────────────────────────────────┐
│  internal/userstore/                                    │
│  ┌────────────────────────────┐ ┌────────────────────┐ │
│  │ feishu_bindings.go (new)   │ │ feishu_pending_    │ │
│  │ per-user ≤ 1 row, AEAD on  │ │ binds.go (new)     │ │
│  │ secret columns             │ │ short-code TTL     │ │
│  └────────────────────────────┘ └────────────────────┘ │
│  ┌────────────────────────────┐                        │
│  │ webhooks.go (edit)         │                        │
│  │  - delete Format=feishu    │                        │
│  └────────────────────────────┘                        │
└─────────────────────────────────────────────────────────┘
```

### Why a new package (not folded into `internal/webhook/`)

The webhook package's contract is "user pastes a URL, relay POSTs an
event payload there." Feishu app-mode is a different shape: app
credentials, server-managed token, structured cards, inbound
callback. Folding both into one package muddies the boundary; the
new package also gives M3 (desktop-direct) something to import
without dragging relay types along.

## 5. Data model

### 5.1 New tables

```sql
-- 5.1.a feishu_bindings: per-user ≤ 1 row.
CREATE TABLE feishu_bindings (
    user_id          TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id_hash      TEXT    NOT NULL UNIQUE,  -- SHA256(app_id) hex; routing key
    app_id_enc       BLOB    NOT NULL,         -- AEAD ciphertext
    app_secret_enc   BLOB    NOT NULL,
    encrypt_key_enc  BLOB    NOT NULL,
    verify_token_enc BLOB    NOT NULL,
    open_id          TEXT,                     -- plaintext; not a secret. NULL until bound.
    bound_at         INTEGER,                  -- unix seconds; set when open_id is filled
    disabled_at      INTEGER,                  -- non-null = 3 token failures, do not retry
    created_at       INTEGER NOT NULL
);

-- 5.1.b feishu_pending_binds: one short-code per user at a time.
CREATE TABLE feishu_pending_binds (
    user_id    TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT    NOT NULL UNIQUE,  -- 6 chars [A-HJ-NP-Z2-9]
    expires_at INTEGER NOT NULL          -- unix seconds, now + 15m
);
CREATE INDEX feishu_pending_binds_expires ON feishu_pending_binds(expires_at);
```

`UNIQUE(app_id_hash)` is load-bearing: two atterm users cannot share
the same Feishu app, because the inbound callback URL routes purely
on the hash. Second user trying to register gets a 409 with
"this Feishu app is already bound to another atterm user."

`UNIQUE(user_id)` on `feishu_pending_binds` means "start binding"
overwrites any previous in-flight code.

### 5.2 Field-level encryption

Four secret columns (`app_id`, `app_secret`, `encrypt_key`,
`verify_token`) are AEAD-encrypted at rest. Rationale: `app_secret`
+ `encrypt_key` together grant attacker full control of the Feishu
app — strictly more dangerous than the legacy webhook URL (which
was stored plaintext). `app_id` is encrypted too so that DB exfil
doesn't reveal which apps are wired up; the hash column carries
routing and matches what an attacker could already guess.

Implementation in `internal/userstore/secret_encrypt.go`:

- Key source: env `ATTERM_FEISHU_ENCRYPT_KEY`, base64-decoded to
  32 bytes. Missing or wrong-length → relay refuses to start with
  a descriptive error; no silent fallback to plaintext.
- Cipher: ChaCha20-Poly1305 (golang.org/x/crypto/chacha20poly1305)
  with a fresh 12-byte random nonce per encryption, stored as
  `nonce || ciphertext || tag` blob.
- No envelope encryption / per-row DEK; YAGNI for the single-relay
  threat model.
- No rotation path: rotating the env key invalidates existing
  bindings; UI says "re-bind required." Acceptable per CLAUDE.md
  preference (replace > deprecate).

### 5.3 Migration

`internal/userstore/migrations/NNNN_feishu.sql`:

1. `CREATE TABLE` for the two new tables (above).
2. `DELETE FROM webhooks WHERE format = 'feishu';` (per CLAUDE.md
   "no backward-compat for old releases"; the legacy custom-bot
   URL path is removed wholesale, not deprecated).
3. CHECK constraint or migration-time assert: surviving rows in
   `webhooks` all have `format = 'generic'`.

### 5.4 `webhook.Format` change

`internal/userstore/webhooks.go` `Webhook.Format` doc comment
shrinks to `"generic"`. `internal/webhook/render.go` loses the
`renderFeishu` function and the `format == "feishu"` branch in
`renderForFormat`. The legacy generic JSON shape stays untouched.

## 6. External API surface (Feishu endpoints we call)

| Verb | Endpoint | Caller | Used for |
|---|---|---|---|
| POST | `/open-apis/auth/v3/tenant_access_token/internal` | `token.go` | mint `tenant_access_token` |
| POST | `/open-apis/im/v1/messages?receive_id_type=open_id` | `client.go` | send interactive card |

Both files share a single `*http.Client` injected from `service.go`
(10 s timeout, no retries). Base URL is injectable for tests.

Feishu API returns `{code, msg, data}`. `code != 0` is mapped to
`fmt.Errorf("feishu: code=%d msg=%s", ...)` and surfaces to the
caller; `service.go` decides whether to write `disabled_at`.

## 7. Internal HTTP surface (relay routes we add)

All routes go in `internal/relay/feishu_http.go` and register from
`server.go` next to the existing webhook routes.

| Verb | Path | Auth | Purpose |
|---|---|---|---|
| GET    | `/v1/feishu/bindings/me`                  | user session | return binding state: `{configured, bound, open_id?, disabled_at?, callback_url}` |
| POST   | `/v1/feishu/bindings/me`                  | user session | upsert credentials; validates by minting one tenant token |
| DELETE | `/v1/feishu/bindings/me`                  | user session | wipe row + token cache |
| POST   | `/v1/feishu/bindings/me/begin-pair`       | user session | generate short-code, returns `{code, expires_at}` |
| POST   | `/v1/feishu/events/{app_id_hash}`         | unauth (verified by encrypt_key) | Feishu event callback |

Note `/v1/feishu/events/...` is **unauthenticated at the HTTP layer**
— Feishu has no concept of bearer tokens against the relay; the
verify step is "decrypt with the binding's `encrypt_key` and match
`verify_token`." The path component `{app_id_hash}` is the lookup
key into `feishu_bindings`.

The callback URL emitted by `GET /v1/feishu/bindings/me` is the
relay's public origin + `/v1/feishu/events/{hash}`; users paste
this string into Feishu admin's "event subscription" page.

## 8. Card schemas

Both cards are Feishu interactive cards (`msg_type=interactive`).
JSON shape lives in `internal/feishu/card.go` with two top-level
constructors and golden-file tests.

### 8.1 `command_finished`

```jsonc
{
  "msg_type": "interactive",
  "card": {
    "config": { "wide_screen_mode": true },
    "header": {
      "title": { "tag": "plain_text", "content": "命令完成" },
      "template": "green" | "red"            // by exit code
    },
    "elements": [
      { "tag": "div", "text": { "tag": "lark_md",
          "content": "**`<label>`** 退出码 `<n>` · 用时 <elapsed>" } },
      { "tag": "action", "actions": [
        { "tag": "button", "text": {"tag":"plain_text","content":"跳回打开 session"},
          "type": "primary",
          "url": "atterm://session/<session_id>" },
        { "tag": "button", "text": {"tag":"plain_text","content":"确认"},
          "type": "default",
          "value": { "kind":"ack", "session_id":"<id>", "event":"command_finished" } }
      ]}
    ]
  }
}
```

Sealed-body variant (E2EE): title becomes `命令完成（仅本机可见）`,
body becomes `命令详情仅本机可见 · 用本机端打开查看`, exit / label /
elapsed are dropped (the relay does not have them).

### 8.2 `waiting_input`

```jsonc
{
  "msg_type": "interactive",
  "card": {
    "config": { "wide_screen_mode": true },
    "header": {
      "title": { "tag": "plain_text", "content": "Session 等待输入" },
      "template": "orange"
    },
    "elements": [
      { "tag": "div", "text": { "tag": "lark_md",
          "content": "Agent 在等待你回复（已闲置 <n>s）" } },
      { "tag": "action", "actions": [
        { "tag": "button", "text": {"tag":"plain_text","content":"跳回打开 session"},
          "type": "primary",
          "url": "atterm://session/<session_id>" },
        { "tag": "button", "text": {"tag":"plain_text","content":"知道了"},
          "type": "default",
          "value": { "kind":"ack", "session_id":"<id>", "event":"waiting_input" } }
      ]}
    ]
  }
}
```

### 8.3 Ack response card

When the relay handles a `card.action.trigger` with `kind=ack`, it
responds inside the same HTTP request body with an updated card:

```jsonc
{
  "toast": { "type": "success", "content": "已确认" },
  "card": {
    "config": { "update_multi": true },
    "header": {
      "title": { "tag": "plain_text", "content": "已确认（<event> #<id-short>）" },
      "template": "grey"
    },
    "elements": [ /* div with original summary, no buttons */ ]
  }
}
```

`update_multi: true` makes Feishu apply the new card to all
recipients of the original message; for private chat this is just
the one user.

## 9. Data flows

### 9.1 Outbound (CMD-EVENT → Feishu card)

```
agent ──CMD-EVENT (uplink)──▶ relay/uplink_conn.go
  ├─ webpush.DispatchCommandFinished (existing)
  ├─ webhook.DispatchCommandFinished (existing, generic only now)
  └─ feishu.Service.SendCommandFinished(userID, ev)  [NEW]
        ├─ store.GetFeishuBinding(userID)
        │      no row, or open_id NULL, or disabled_at != NULL → return
        ├─ tokens.Get(binding) (singleflight'd)
        │      cache miss / expired → POST tenant_access_token/internal
        ├─ card.RenderCommandFinished(ev, binding)
        ├─ client.SendInteractive(token, binding.open_id, card)
        │      err → log; on 3 consecutive auth-class failures
        │            (code∈{99991663, 99991664, 99991665}) write disabled_at
        └─ goroutine, fire-and-forget
```

Identical shape for `waiting_input` via
`feishu.Service.SendSessionNotification` invoked next to
`webpush.DispatchSessionNotification`.

### 9.2 Inbound event callback

```
Feishu HTTPS POST ──▶ /v1/feishu/events/{hash}
  feishu_http.HandleEvent:
    ├─ parse outer JSON
    │    "type":"url_verification"  ──▶ reply {"challenge": <c>}
    ├─ binding := store.GetFeishuBindingByAppIDHash(hash)
    │    not found → 200 + log "unknown hash"
    ├─ plaintext := event.Decrypt(body.encrypt, binding.encrypt_key)
    │    decrypt fail → 200 + log (do not 5xx, Feishu hammers on 5xx)
    ├─ event.VerifySignature(header.token, binding.verify_token)
    │    mismatch → 200 + log
    ├─ dispatch by plaintext.header.event_type:
    │    "im.message.receive_v1"  → HandleBindMessage (async, 200 immediately)
    │    "card.action.trigger"    → HandleCardCallback (SYNC, response body = new card)
    │    other                    → 200 + log "ignored"
    └─ all paths bounded by 2.5 s context (Feishu's 3 s timeout)
```

The async vs sync split is the only subtlety: `im.message.receive_v1`
processing (binding completion, sending a confirmation message via
the IM API) runs in a goroutine because the IM call could take
hundreds of ms; the HTTP request returns 200 immediately. Card
callbacks must be synchronous because the response body **is** the
updated card; goroutining loses that channel.

### 9.3 Binding flow (user-visible steps)

```
Step 1 — In Feishu open platform:
        user creates a self-built app, notes app_id, app_secret,
        encrypt_key, verify_token. Subscribes events
        im.message.receive_v1 and card.action.trigger. Grants the
        bot im:message and im:message:send_as_bot scopes.

Step 2 — In atterm settings → Feishu tab:
        user pastes the four secrets.
        Frontend → POST /v1/feishu/bindings/me.
        Relay validates by minting one tenant_access_token (200 →
        creds OK). Writes encrypted row; returns app_id_hash and
        the callback URL string.

Step 3 — Back in Feishu open platform:
        user pastes the callback URL into "event subscription";
        Feishu fires url_verification, relay echoes the challenge.

Step 4 — In atterm settings:
        user clicks "Start pair."
        Frontend → POST /v1/feishu/bindings/me/begin-pair.
        Relay generates a 6-char short-code (alphabet A-HJ-NP-Z2-9,
        ~28 bits entropy), writes feishu_pending_binds, returns the
        code. UI shows: "Send /bind XXXXXX to your bot's private
        chat within 15 minutes."

Step 5 — In Feishu IM:
        user sends "/bind XXXXXX" to the bot.
        Event callback arrives → HandleBindMessage extracts the code
        and the sender's open_id, atomic DELETE … RETURNING on
        pending_binds, updates feishu_bindings.open_id + bound_at.
        Bot replies "✅ 已绑定到 atterm" via the freshly-cached
        tenant_access_token.

Step 6 — Frontend (poll or websocket):
        Sees open_id populated, dismisses the binding modal.
```

Re-pair (user switched Feishu accounts): just clear `open_id` via
DELETE then re-run Steps 4–5; credentials stay.

## 10. Error handling & edge cases

| Scenario | Behavior |
|---|---|
| Feishu callback exceeds 3 s | client always returns 200 within 2.5 s; long work goroutined; missed event is acceptable (binding code TTL absorbs) |
| `tenant_access_token` fetch fails | log + drop the outbound event; 3 consecutive auth-class failures → write `disabled_at`, surface in UI |
| Auth-class token error after re-config | DELETE `disabled_at` on successful `POST /v1/feishu/bindings/me` revalidation |
| Decryption fail at inbound | 200 + log warn with first 8 chars of hash |
| Signature mismatch | 200 + log error |
| Unknown app_id_hash | 200 + log info |
| Sealed payload (E2EE M6) on outbound | card.go switches to sealed-variant text; deep link still emitted (session_id is not sealed) |
| User deletes binding while events are in flight | new event lookups miss → 200 + log; in-flight goroutine still completes its outbound call against now-stale token (harmless) |
| User filled wrong `verify_token` | every inbound event fails signature check → 200 + log; binding flow gets stuck (no /bind goes through). UI exposes a "last 50 inbound events" log to help diagnose |
| Two atterm users paste the same `app_id` | second `POST /v1/feishu/bindings/me` fails on `UNIQUE(app_id_hash)` → 409 "already bound by another user" |
| Master key env missing at startup | fail-fast with descriptive error |
| Short-code race (user sends /bind twice fast) | atomic DELETE … RETURNING; second send sees pending row gone → bot replies "code already used or expired" |
| Card callback path errors | 2.5 s context, on timeout return a minimal `{toast: warning, content: "处理超时"}` and don't update the card |
| Frequent flapping `waiting_input` | M2 does not implement mute; flow is acceptable noise. A future milestone would add per-session mute if it becomes a real complaint. |

## 11. Testing strategy

### 11.1 Unit (`internal/feishu/*_test.go`)

- `token_test.go` — cache hit/miss/expiry; singleflight under
  concurrent fetch; auth-class code parsing; 3-failure disable
  using a fake clock.
- `card_test.go` — golden-file JSON for both cards in both normal
  and sealed variants; exit-code template selection matrix; deep
  link string formation.
- `event_test.go` — AES-GCM (Feishu's spec) decrypt happy path and
  tampered ciphertext; signature verify positive + negative; URL
  verification short-circuit; schema parsing for
  `im.message.receive_v1` and `card.action.trigger`.
- `client_test.go` — `httptest.Server` standing in for Feishu;
  request envelope check; 429/5xx handling; 10 s timeout enforced.
- `service_test.go` — end-to-end through the aggregate layer with
  fake transports and an in-memory store; covers both outbound and
  inbound dispatch arms.

### 11.2 Userstore (`internal/userstore/*_test.go`)

- `feishu_bindings_test.go` — CRUD + AEAD round-trip via the
  encrypt helper; `UNIQUE(app_id_hash)` rejection; `GetByAppIDHash`
  miss returns sentinel.
- `feishu_pending_binds_test.go` — write, query, atomic consume,
  expiry sweep, `UNIQUE(user_id)` overwrite semantics, alphabet
  sanity.
- `secret_encrypt_test.go` — round-trip, distinct nonces across
  calls, zero-length plaintext OK, wrong-key fails clean.

### 11.3 Relay integration (`internal/relay/feishu_http_test.go`)

Use existing `helpers_test.go` testserver scaffolding.

- `POST /v1/feishu/bindings/me` — validation calls a fake Feishu;
  bad secret → 400; good secret → row written, response has hash +
  callback URL.
- `POST /v1/feishu/bindings/me/begin-pair` — code in expected
  alphabet, pending row written, repeat overwrites.
- `POST /v1/feishu/events/{hash}` — happy path with a known
  binding's encrypt_key; url_verification challenge echo; unknown
  hash returns 200; signature mismatch returns 200.
- `DELETE /v1/feishu/bindings/me` — row gone, token cache
  invalidated (verify via metric or `Service.HasCached`).

### 11.4 Uplink integration

- `uplink_feishu_test.go` — CMD-EVENT arrives → fake
  `feishu.Service` records the call with expected fields; absent
  binding → no call; `disabled_at` set → no call.

### 11.5 Manual end-to-end checklist

`scripts/feishu-e2e-checklist.md` — not in CI. Run per PR with a
real Feishu app: paste creds → callback URL paste → url_verification
→ /bind → see real card on phone → press "确认" → card updates →
press "跳回" → deep link triggers (no-op until follow-up PR registers
the scheme handler, document that).

## 12. Risks

| Risk | Mitigation |
|---|---|
| `atterm://` deep link unimplemented at M2 ship time | Spec marks as follow-up; PR description and CHANGELOG call out "jump-back is a no-op until the desktop scheme handler ships" |
| Feishu API / event schema drift | Parsing isolated to `event.go` / `card.go` two small files; grep-replace footprint when fields move |
| Users conflate `verify_token` vs `encrypt_key` | UI placeholders quote the exact Feishu admin labels; bindings POST validates by minting a token (catches wrong `app_secret`), but only inbound events can catch wrong `verify_token` — so the UI surface "last 50 inbound events" log is load-bearing for diagnosis |
| Same Feishu app reused across atterm users | `UNIQUE(app_id_hash)`; explicit 409 with helpful message |
| Master key absent on relay restart | Fail-fast at startup, CHANGELOG mentions in the release notes |
| 3 s callback budget vs Feishu IM send for binding confirmation | The bot's "已绑定" reply is sent from the goroutine that handles `im.message.receive_v1` and does **not** block the 200; if the IM send fails, log it and the UI still flips to "bound" because the DB row is the source of truth |
| Spec size (~1.8 k LOC) | Plan slices into ~8 sequential tasks; subagent-driven-development for the bulk of `internal/feishu/` |

## 13. Open questions

None blocking the plan. Items deliberately deferred to a follow-up
PR or future milestone:

- Concrete deep link routing on desktop / mobile is M2 ship-time
  follow-up.
- A "test send" button in the Feishu settings tab to send a
  hand-crafted card (verify the bound channel works without
  waiting for a real `command_finished`). Could land in the same PR
  series cheaply; mark as optional task in the plan.
- A "rebind" button that just clears `open_id` and re-issues a
  short-code in one click instead of DELETE + begin-pair. UX
  polish; not load-bearing.

## 14. Out of scope from prior brainstorm (recorded for context)

These were considered and explicitly cut during brainstorming —
recorded so a future reader does not re-argue them.

- **OAuth / `user_access_token`**: only useful for accessing user-
  private resources (personal docs, IM history). We need only
  `open_id` to send and `tenant_access_token` to authenticate
  outbound; OAuth's extra dependency on a public HTTPS redirect
  buys nothing here.
- **Manual `open_id` entry**: deliverable zero-infra path, but
  `@bot + short-code` reuses the event subscription channel we
  already need to build and avoids users hunting in Feishu admin.
- **Desktop-local form-factor first**: rejected because Feishu's
  long-connection mode does not broadcast across same-app clients,
  so multi-device users would lose binding messages and card
  callbacks at random. Desktop-local stays a single-device tier
  (M3/M4).
- **Folding into `internal/webhook/`**: rejected for M3 reuse and
  clarity of package contract.
