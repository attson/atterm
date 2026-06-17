# Feishu hook + desktop-direct send (Q1) — design

Date: 2026-06-17
Status: Drafted — awaiting user review before plan.

## 0. Summary

Replace the v0.2.114 relay-side Feishu outbound path with desktop-direct
sending. The relay no longer composes or POSTs any Feishu IM message;
all Feishu notifications now leave the user's machine straight from
the desktop binary. This lets the desktop attach a precise "what is
the AI asking?" payload (captured via the agent's own hook system),
which the relay structurally could not see under E2EE.

Two storage / event-subscription modes are supported in parallel,
mirroring how the rest of atterm desktop handles relay vs. local-only
deployments:

- **Relay-backed mode** (user is logged in to a relay): Feishu app
  credentials live in the relay store (v0.2.114 onboarding unchanged),
  the relay's `/v1/feishu/events/...` HTTPS callback still receives
  inbound events, and the desktop fetches a fresh `tenant_access_token`
  from a new `/v1/feishu/relay-token/me` endpoint.
- **Local mode** (no relay account): credentials live in the OS
  keychain, the desktop subscribes to inbound events via Feishu's
  long-connection (WebSocket) protocol, and the desktop talks to the
  Feishu auth endpoint directly to mint tokens.

Outbound triggers come from three sources that converge on a single
desktop-side dispatcher:

1. **Precise**: `claude-code` Notification hook → new `atterm-hook` CLI
   → local HTTP endpoint on the desktop. Carries the actual prompt /
   question text from claude-code's `NotificationHookInput`.
2. **Heuristic — task state**: existing `internal/session.Session`'s
   OSC 133 parser + `looksLikeWaitingInput` heuristic, surfaced as a
   callback to the desktop dispatcher.
3. **Heuristic — command finished**: existing OSC 133 'D' detection,
   same callback path.

The v0.2.114 relay-side feishu send code (`SendCommandFinished`,
`SendSessionNotification`, the two `uplink_conn.go` dispatch sites,
the integration test) is **deleted**.

## 1. Goals

- Replace the relay-side outbound Feishu path with desktop-direct send.
- Surface the actual prompt text claude-code is waiting on (caught via
  its Notification hook), inside the Feishu card body.
- Keep the existing relay-backed onboarding flow intact for users
  logged in to a relay.
- Add a parallel local-only onboarding flow (keychain + Feishu
  long-connection) for users who do not run a relay.
- Lay a pluggable `HookAdapter` foundation so a future codex /
  aider / generic adapter can be added without restructuring.

## 2. Non-goals

- Mobile (Capacitor) and web clients participate as session viewers
  only. They never originate Feishu IM messages — the session host is
  always a desktop process.
- Codex hook integration. Codex CLI's external `notify` hook currently
  emits only on `agent-turn-complete` ([openai/codex#19921][codex19921]
  remains open as of 2026-04). The HookAdapter interface reserves a
  slot but the implementation is deferred.
- aider / generic shell AI tools. These fall back to the heuristic
  `looksLikeWaitingInput` + OSC 133 paths and do not receive precise
  question text.
- Group-chat Feishu sending (private chat only, as in v0.2.114).
- Higher-level card actions (mute N minutes, retry, full output paste).
  v0.2.114's `ack` button remains the only inbound action.
- Multiple Feishu accounts per atterm user.
- A migration path that automatically lifts relay-backed binding rows
  into the local keychain (or vice versa) when the user changes login
  state. The user manually re-enters credentials after switching.

[codex19921]: https://github.com/openai/codex/issues/19921

## 3. Scope: single spec, ~22 plan tasks

The scope is bigger than the v0.2.114 spec (~14 tasks) because it
spans both a new desktop subsystem and a CLI binary, plus the
v0.2.114 cleanup. The slicing is:

| Slice | Content |
|---|---|
| A — Relay cleanup | Delete v0.2.114 outbound feishu code (service.Send*, uplink dispatch sites, the integration test). Add `RelayToken` method + `POST /v1/feishu/relay-token/me`. |
| B — `atterm-hook` CLI | New `cmd/atterm-hook` binary, reads claude-code stdin, posts to desktop endpoint. Pure I/O. |
| C — Desktop dispatcher | `desktop/feishu/` new package: hook HTTP server, dispatcher with dedup, double-mode binding store / token source, card render adapter (imports `internal/feishu/card` from v0.2.114). |
| D — Feishu long-connection | non-relay-mode inbound. Wraps `github.com/larksuite/oapi-sdk-go/v3` long-connection client. |
| E — Wails wiring | Session lifecycle hooks, env-var injection for PTY children, hook-endpoint discovery file. Frontend settings tab adjustments. |

Each slice is a logical group in the plan, not a per-PR boundary —
the plan executor still runs one PR for the whole spec.

## 4. Architecture

```
                                                    ┌────────────────────┐
                                                    │  claude-code       │
                                                    │  Notification hook │
                                                    └─────┬──────────────┘
                                                          │ stdin: NotificationHookInput
                                                          ▼
                                              ┌─────────────────────────┐
                                              │  cmd/atterm-hook/  (new)│
                                              │  small standalone CLI   │
                                              └─────┬───────────────────┘
                                                    │ POST /atterm-hook/notify
                                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  desktop (wails host, the only origin of outbound Feishu IM)                │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐ │
│  │  desktop/feishu/  (new package)                                        │ │
│  │  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────┐│ │
│  │  │ dispatcher       │  │ hook_server      │  │ longconn (D)         ││ │
│  │  │ + dedup          │  │ HTTP receiver    │  │ Feishu WS subscriber ││ │
│  │  └──┬───────────────┘  └────┬─────────────┘  └────────┬─────────────┘│ │
│  │     │                       │                          │              │ │
│  │     └─────────┬─────────────┴──────────────────────────┘              │ │
│  │               ▼                                                       │ │
│  │  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────┐│ │
│  │  │ hook_adapter     │  │ binding_store    │  │ token                ││ │
│  │  │ HookAdapter      │  │ double-mode      │  │ double-mode          ││ │
│  │  │  - claude-code   │  │  - relay-backed  │  │  - relay-borrowed    ││ │
│  │  │  - codex (stub)  │  │  - local kc      │  │  - local kc-fed mint ││ │
│  │  └──────────────────┘  └──────────────────┘  └──────────────────────┘│ │
│  └───────────────────────────────────────────────────────────────────────┘ │
│                                  │                                          │
│                                  ▼ uses                                     │
│  ┌───────────────────────────────────────────────────────────────────────┐ │
│  │  Imports from internal/feishu/  (reused from v0.2.114, unmodified):    │ │
│  │   - card.go     (RenderCommandFinishedCard, RenderWaitingInputCard,    │ │
│  │                  RenderAckUpdateCard + new field WaitingInputInput     │ │
│  │                  .QuestionText)                                        │ │
│  │   - client.go   (SendInteractiveToOpenID, SendTextToOpenID)            │ │
│  │   - token.go    (TenantTokenCache — reused for local mode)             │ │
│  │   - event.go    (DecryptEnvelope, ParseEnvelope — long-conn uses)      │ │
│  └───────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                  │
                                  │ HTTPS POST /open-apis/im/v1/messages
                                  ▼
                          ┌──────────────────────┐
                          │  Feishu IM           │
                          └──────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  relay (slimmed down)                                                       │
│                                                                             │
│  internal/feishu/service.go:                                                │
│   - DELETE  SendCommandFinished, SendSessionNotification,                   │
│             sendCommandFinishedSync, sendSessionNotificationSync,           │
│             send (private), recordSendError, authFailMu, authFailures       │
│   - KEEP    HandleEvent, handleBindMessage, sendBindReply,                  │
│             MintTokenForCreds, InvalidateTokenForAppID                      │
│   - ADD     RelayToken(ctx, userID) (token, openID, expiresInSec, err)      │
│                                                                             │
│  internal/relay/feishu_http.go:                                             │
│   - KEEP    all /v1/feishu/bindings/me* routes                              │
│   - KEEP    /v1/feishu/events/{hash} route                                  │
│   - ADD     POST /v1/feishu/relay-token/me handler                          │
│                                                                             │
│  internal/relay/uplink_conn.go:                                             │
│   - DELETE  both s.cfg.Feishu.* dispatch calls                              │
│             (lines added by v0.2.114 commit 55ba9d4)                        │
│   - DELETE  the `s.cfg.Feishu != nil` clause from the no-op early return    │
│                                                                             │
│  internal/relay/uplink_feishu_test.go:                                      │
│   - DELETE  whole file (the dispatched paths no longer exist)               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.1 Why this split

- **Desktop is the only sender** because (a) the desktop already has
  plaintext access (account_key for E2EE; native PTY bytes when E2EE
  is off) and (b) the relay structurally cannot synthesize a useful
  question-text body without seeing the AI's output.
- **`internal/feishu/` modules are reused, not copied** — desktop is
  in the same Go module under `github.com/attson/atterm`, so importing
  `internal/feishu/{card,client,token,event}` from `desktop/feishu/`
  is direct and avoids duplication. The relay still imports the same
  modules for the surviving binding / event-callback paths.
- **A separate `desktop/feishu/` package** keeps wails-side wiring
  (session callbacks, env injection, settings UI) isolated from the
  pure protocol code, mirroring how `desktop/uplink.go` wraps
  `internal/proto`.

## 5. Data model

### 5.1 Local-mode binding storage (new)

When the user is not logged in to a relay, the desktop persists the
binding in the OS keychain (using the existing
`desktop/account_key_store.go` keyring helper pattern, but a different
service name to avoid collisions with the E2EE account key):

```go
// desktop/feishu/binding_store_local.go
type LocalBinding struct {
    AppID       string // base64'd as JSON in keychain blob
    AppSecret   string
    EncryptKey  string
    VerifyToken string
    OpenID      string  // empty until paired
    BoundAt     int64
    DisabledAt  int64
    CreatedAt   int64
}
```

Stored as a single JSON blob under keychain service
`atterm.feishu.binding`. The struct mirrors
`userstore.FeishuBindingCredentials + FeishuBinding` but lives wholly
on the desktop machine; no AEAD-at-rest encryption layer — the OS
keychain provides it.

Pending pair codes in local mode live in process memory only
(`map[string]int64` of `code → expires_at`, 15-minute TTL) — there is
exactly one pending code per user at a time. Lost on desktop restart;
the user simply clicks "Start pair" again.

### 5.2 Relay-mode binding storage (unchanged from v0.2.114)

`feishu_bindings` + `feishu_pending_binds` SQLite tables, ChaCha20-
Poly1305 field encryption, `ATTERM_FEISHU_ENCRYPT_KEY` env. Section 5
of the v0.2.114 spec applies verbatim.

### 5.3 The `BindingStore` abstraction (desktop)

```go
// desktop/feishu/binding_store.go
type BindingView struct {
    AppID, AppSecret, EncryptKey, VerifyToken string
    AppIDHash, OpenID                          string
    DisabledAt                                 int64
}

type BindingStore interface {
    Get(ctx) (*BindingView, error)
    SetCredentials(ctx, creds Credentials) error  // upsert path
    SetBound(ctx, openID string) error
    SetDisabled(ctx) error
    ClearDisabled(ctx) error
    Delete(ctx) error
}

// RelayBackedBindingStore   ← calls HTTP GET /v1/feishu/bindings/me
// LocalKeychainBindingStore ← reads keychain JSON blob
```

The dispatcher is constructed with one or the other based on
`Service.Mode == Relay | Local`. The mode is decided at startup by
inspecting whether `desktop` has a valid relay session token.

### 5.4 The `TokenSource` abstraction (desktop)

```go
type TokenSource interface {
    Get(ctx) (token, openID string, expiresInSec int, err error)
    Invalidate()
}

// RelayBorrowedTokenSource  ← POST /v1/feishu/relay-token/me
// LocalTenantTokenSource    ← imports internal/feishu.TenantTokenCache,
//                            keyed on the keychain-stored app_id/secret
```

### 5.5 `WaitingInputInput.QuestionText` (new field on existing struct)

`internal/feishu/card.go`'s `WaitingInputInput` gains an optional
field:

```go
type WaitingInputInput struct {
    SessionID      uuid.UUID
    IdleForSeconds int
    QuestionText   string // NEW. Empty for heuristic-only triggers;
                          //      populated for hook-driven triggers.
                          //      Truncated by the desktop before reaching here.
}
```

`RenderWaitingInputCard` emits a new `div` element above the action
row when `QuestionText != ""`, formatted as a code block (preserves
the AI's prompt formatting):

```jsonc
{ "tag": "div",
  "text": {
    "tag": "lark_md",
    "content": "```\n<question text up to 1200 chars / 6 lines>\n```\n_(已截断)_"   // ellipsis line shown only when truncated
  } }
```

Truncation rule lives in `desktop/feishu/dispatcher.go`:

- ANSI-stripped UTF-8 input
- Head-preserve (keep "I'm about to do X, continue? (y/n)" leading
  context; drop tail)
- Max 6 lines / 1200 chars; if cut, append `(已截断)` on its own line
  outside the code block

### 5.6 Hook input shape

```go
// desktop/feishu/hook_server.go
type HookNotifyRequest struct {
    SessionID string          `json:"session_id"`   // from ATTERM_SESSION_ID env
    AgentKind string          `json:"agent_kind"`   // "claude-code" only in P1
    HookInput json.RawMessage `json:"hook_input"`   // claude-code's NotificationHookInput verbatim
    HookVersion string        `json:"hook_version"` // claude-code version string, for adapter routing
}
```

The CLI fills `SessionID` from env, hard-codes
`AgentKind = "claude-code"` for P1 (since it knows it was invoked
from the claude-code hook), and forwards stdin verbatim as
`HookInput`.

## 6. External API surface

### 6.1 Feishu endpoints called by the desktop directly

| Verb | Endpoint | Caller |
|---|---|---|
| POST | `/open-apis/auth/v3/tenant_access_token/internal` | `LocalTenantTokenSource` (local mode only) |
| POST | `/open-apis/im/v1/messages?receive_id_type=open_id` | `desktop/feishu/dispatcher` — both modes |
| WS   | Feishu long-connection event stream (SDK-driven) | `desktop/feishu/longconn` (local mode only) |

### 6.2 Local HTTP endpoint exposed by desktop (for `atterm-hook` CLI)

A dedicated `net/http.Server` inside the desktop process, bound to
`127.0.0.1:0` so the OS picks a free port. This is **separate from
wails** — wails handles the webview / IPC for the desktop UI and is
not designed to host arbitrary HTTP receivers, so the hook server is
its own listener with its own lifecycle (started after the wails app
ready event, stopped on shutdown).

| Verb | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/atterm-hook/notify` | none (listener is bound to 127.0.0.1 only) | receive `HookNotifyRequest` |

The handler additionally double-checks `r.RemoteAddr` against
`127.0.0.1` / `::1` and rejects everything else with 403 — defense in
depth if the bind address is ever misconfigured.

### 6.3 Relay endpoints (the surviving + 1 new)

| Verb | Path | Status | Note |
|---|---|---|---|
| GET    | `/v1/feishu/bindings/me`          | UNCHANGED  | v0.2.114 |
| POST   | `/v1/feishu/bindings/me`          | UNCHANGED  | v0.2.114 |
| DELETE | `/v1/feishu/bindings/me`          | UNCHANGED  | v0.2.114 |
| POST   | `/v1/feishu/bindings/me/begin-pair` | UNCHANGED | v0.2.114 |
| POST   | `/v1/feishu/events/{app_id_hash}` | UNCHANGED  | v0.2.114 |
| **POST** | **`/v1/feishu/relay-token/me`** | **NEW**    | session-auth; returns `{tenant_access_token, expires_in, open_id, app_id_hash}` |

## 7. Data flows

### 7.1 Outbound — claude-code precise

```
1. desktop spawns session i.e. PTY for `claude` with extra env:
     ATTERM_SESSION_ID     = "<session uuid>"
     ATTERM_HOOK_ENDPOINT  = "http://127.0.0.1:<wails-port>/atterm-hook/notify"

2. user has configured ~/.claude/settings.json:
     "hooks": {
       "Notification": [
         { "matcher": { "type": "idle_prompt" },
           "command": "atterm-hook" },
         { "matcher": { "type": "permission_prompt" },
           "command": "atterm-hook" }
       ]
     }

3. claude-code fires Notification → forks `atterm-hook`,
   pipes NotificationHookInput JSON to stdin.

4. atterm-hook:
   - reads stdin (up to 64 KB; larger → drop with stderr warn, exit 0)
   - reads env (both required)
     - missing ATTERM_SESSION_ID → exit 0 silently (running outside atterm)
     - missing ATTERM_HOOK_ENDPOINT → try ~/.config/atterm/hook-endpoint file
                                       still missing → exit 0
   - builds HookNotifyRequest{SessionID, AgentKind:"claude-code", HookInput, HookVersion}
   - POST with 1s timeout
   - any error path → stderr + exit 0 (never bubble up to claude-code)

5. desktop/feishu/hook_server.HandleNotify:
   - decode HookNotifyRequest
   - lookup session by SessionID
     - not found → 404 (session ended) — atterm-hook discards
   - find HookAdapter by AgentKind ("claude-code" → claude-code adapter)
   - adapter.Parse(HookInput) → QuestionText (truncated) + dedup key
   - dispatcher.Dispatch(WaitingInputEvent{
       SessionID:     sessionID,
       QuestionText:  "...",
       Source:        "hook",  // for dedup with heuristic
     })

6. dispatcher.Dispatch (shared with heuristic path):
   - dedup: if lastDispatched[sessionID] within 30s and source == precise — emit; otherwise drop
   - binding := bindingStore.Get(ctx)
     - error → log + drop
     - OpenID == "" → drop (not paired)
     - DisabledAt > 0 → drop
   - token, openID, _ := tokenSource.Get(ctx)
     - error → recordError(binding, err); drop
   - card := RenderWaitingInputCard(WaitingInputInput{SessionID, IdleForSeconds, QuestionText})
   - cardBody := json.Marshal(card)
   - imClient.SendInteractiveToOpenID(ctx, token, openID, cardBody)
     - error → recordError; drop
   - mark lastDispatched[sessionID] = now
```

### 7.2 Outbound — heuristic (command_finished, generic waiting_input)

```
1. Session bytes flow through internal/session/Session.PushOut
2. Existing applyOSC133Locked / looksLikeWaitingInput / fireWaiting paths
   detect state transitions.
3. Add a small callback on Session, similar to s.onAIClassified:
     OnTaskStateChange func(sid uuid.UUID, prev, next string, meta TaskMeta)
4. desktop/relay_host.go registers this callback for each local session.
   Maps to:
     - "running"  → "completed" via OSC 133 'D'  → dispatcher.DispatchCommandFinished(...)
     - "running"  → "waiting_input"              → dispatcher.DispatchWaitingInput(..., Source:"heuristic")
5. Dispatcher does the same lookup / token / send dance as 7.1, but:
     - command_finished uses RenderCommandFinishedCard (existing v0.2.114)
     - waiting_input from heuristic uses RenderWaitingInputCard with
       QuestionText = "" (generic card, no question body)
```

### 7.3 Inbound — card ack action (double-mode)

**Relay-backed mode (unchanged from v0.2.114):**

```
飞书 → POST /v1/feishu/events/<hash> on relay → feishu.Service.HandleEvent →
  returns AckUpdateCard JSON → relay HTTP body → 飞书 updates card.
```

**Local mode (new):**

```
飞书 long-connection event arrives at desktop/feishu/longconn.
The longconn handler reuses internal/feishu/event.go (ParseEnvelope +
DecryptEnvelope + VerifyEnvelopeToken) since the long-connection
protocol carries the same envelope shape inside its WS frame.

CARD_ACTION_TRIGGER → renderer.RenderAckUpdateCard → reply via SDK
ack channel. Long-connection has a built-in synchronous-reply path
that mirrors the HTTP callback's "write the updated card JSON back".
```

### 7.4 Inbound — bind message `/bind XXXX` (double-mode)

**Relay-backed mode (unchanged from v0.2.114):**

```
relay binding upsert + begin-pair endpoints → user sends /bind XXXX in
Feishu → relay HTTPS callback → feishu.Service.handleBindMessage →
relay marks bound + sends "✅ 已绑定" reply.
```

**Local mode (new):**

```
1. User opens atterm settings → Feishu tab while NOT logged in to a relay.
   The UI presents the same four fields (app_id, app_secret, encrypt_key,
   verify_token) but writes them straight to LocalKeychainBindingStore.
2. On save, desktop/feishu/service immediately:
   - mints a tenant_access_token to validate creds (transparent to user)
   - opens the long-connection (if not already open)
3. UI shows "Start pair" → desktop generates a 6-char code,
   stashes it in process memory with 15min TTL, displays it.
4. User goes to Feishu and private-messages bot: /bind XXXX
5. Long-connection delivers im.message.receive_v1 → handler parses
   /bind, matches code, writes OpenID into keychain, replies "✅ 已绑定"
   via SDK send (which internally calls IM API the same as outbound
   sends would).
```

### 7.5 Hook endpoint discovery

```
1. Desktop start-up:
   - wails resolves an HTTP listener address (default 127.0.0.1:<random>)
   - writes the URL to ~/.config/atterm/hook-endpoint (POSIX)
                       %APPDATA%\atterm\hook-endpoint (Windows)
   - on shutdown, deletes the file (best-effort; stale lookups
     fail fast and exit 0, so non-deletion is acceptable)

2. PTY child env (per-session):
   - For every session spawned by desktop, ATTERM_HOOK_ENDPOINT is
     pushed into the child env alongside ATTERM_SESSION_ID.

3. atterm-hook CLI lookup order:
   a. env ATTERM_HOOK_ENDPOINT
   b. ~/.config/atterm/hook-endpoint or %APPDATA%\atterm\hook-endpoint
   c. nothing → exit 0 (silent; outside atterm)

The endpoint URL has the form `http://127.0.0.1:<port>/atterm-hook/notify`
where `<port>` is the listener's bound address (see §6.2).
```

## 8. The `HookAdapter` interface

```go
// desktop/feishu/hook_adapter.go
type HookAdapter interface {
    AgentKind() string  // matches HookNotifyRequest.AgentKind
    // Parse converts the agent-specific hook payload into a normalized
    // WaitingInputEvent. Returns (event, true) when the payload is a
    // "waiting for user" event we should notify on. Returns (_, false)
    // for events we ignore (turn-complete style, unknown matchers).
    Parse(hookInput json.RawMessage, hookVersion string) (WaitingInputEvent, bool)
}

// registry populated at startup:
var hookAdapters = map[string]HookAdapter{
    "claude-code": &claudeCodeAdapter{},
    // future: "codex": &codexAdapter{},  // currently a stub returning false
}
```

### 8.1 claude-code adapter

claude-code's `NotificationHookInput` (per
[anthropics/claude-code][cc-hooks] schema as of 2026-04):

```jsonc
{
  "matcher": {
    "type": "idle_prompt" | "permission_prompt" | "subagent_stop" | ...,
    "tool": "AskUserQuestion" | "..." | null
  },
  "prompt_id": "uuid",
  "context": {
    "last_message": "string",   // the model's latest assistant response
    "tool_name": "string",      // when a tool is asking
    "tool_input": object         // tool args, used for question text
  }
}
```

Parse logic:

- `matcher.type == "permission_prompt"` → always emit; question text is
  derived from `context.tool_name` + a short summary of `tool_input`
  (the dangerous tool's arguments)
- `matcher.type == "idle_prompt"` AND `matcher.tool == "AskUserQuestion"`
  → emit; question text is the tool's question body
- `matcher.type == "idle_prompt"` AND no AskUserQuestion → SKIP
  (this is the false-positive case [#12048][cc12048] flagged — the
  agent is idle but no question is pending)
- anything else → SKIP

Dedup key: `claude-code:<prompt_id>` if present, else a fallback hash
of `(session_id, truncated question text)`.

[cc-hooks]: https://docs.claude.com/en/docs/claude-code/hooks
[cc12048]: https://github.com/anthropics/claude-code/issues/12048

### 8.2 codex adapter (deferred stub)

`(codexAdapter) Parse(...)` returns `(_, false)` for everything. The
slot exists for spec / future plan symmetry; no test fixtures, no
adapter behavior changes if it stays absent.

## 9. Error handling & edge cases

| Scenario | Behavior |
|---|---|
| `atterm-hook` POST fails (desktop not running / wrong port) | CLI exits 0; user gets no Feishu card; no impact on claude-code |
| `atterm-hook` stdin > 64 KB | CLI prints stderr warning + exits 0 |
| Session ID in POST not found on desktop | hook_server replies 404; CLI discards |
| Session ID present but not paired in binding | dispatcher drops silently (matches v0.2.114 semantics) |
| Token fetch fails (auth-class) | dispatcher counts failure; 3-in-a-row → `SetDisabled` on store; UI shows "Feishu integration disabled — re-configure" |
| Token fetch fails (transient) | drop this event; no disable counter increment |
| Long-connection drops (local mode) | exponential backoff reconnect (1, 2, 4, 8, …, capped at 5min) |
| Long-connection rejected on reconnect with auth-class error | `SetDisabled`; UI surfaces it |
| Two desktops on the same relay account, each running its own session | each desktop dispatches its own card; no dedup needed (different sessions) |
| Two desktops on the same account, both attached to the same session | impossible by definition: PTY host is single |
| User has E2EE on and the session is encrypted | irrelevant — desktop sees plaintext directly from the local PTY; relay path no longer participates |
| User logs out of relay mid-session | dispatcher restart with `LocalKeychainBindingStore`; if no local creds, no cards sent; UI prompts re-configure |
| User logs in to relay mid-session | dispatcher restart with `RelayBackedBindingStore` + `RelayBorrowedTokenSource`; local creds (if any) become inert but not deleted |
| claude-code hook fires before atterm has imported the session into its registry | extremely brief race; hook_server returns 404; user re-triggers next time (claude-code hook firings are not exactly-once anyway) |
| User runs claude-code outside atterm (e.g. plain Terminal.app) | `ATTERM_SESSION_ID` env is absent; CLI exits 0; no Feishu spam |
| Dispatcher's dedup map grows unbounded | LRU cap at 256 sessions (typical user has < 20); evicted entries just allow a re-send, no correctness loss |
| 30s dedup window vs. user actually wants two cards | accepted noise floor; if it becomes a real complaint, surface as setting later |
| `internal/feishu/card.go` schema diverges between relay and desktop callers | the module is shared in-process; bumping the struct in one place updates both. Tests live in `internal/feishu/card_test.go` |
| Wails port changes between atterm restarts | endpoint file rewrite + env injection at session spawn → next session's hooks use the new port |
| Hook endpoint file on disk after a crash | stale endpoint file is harmless: lookup → connect-refused → CLI exits 0 |

## 10. Testing strategy

### 10.1 Unit — `desktop/feishu/`

| File | Coverage |
|---|---|
| `dispatcher_test.go` | dispatch happy path (both modes); dedup window across hook vs heuristic; auth-class disable counter; quiet drop on no binding |
| `hook_server_test.go` | route binding to localhost; JSON shape validation; 404 on unknown session; 413 on body > 64KB; concurrent requests |
| `hook_adapter_test.go` | claude-code adapter Parse: idle_prompt with AskUserQuestion (emit), idle_prompt without (skip), permission_prompt (emit), unknown matcher (skip), schema drift fallback |
| `binding_store_test.go` | interface contract (table-driven across both impls) |
| `binding_store_local_test.go` | keychain CRUD with an in-memory keyring fake; UNIQUE-by-app-id semantics (we only ever store one binding per process); short-code TTL eviction |
| `binding_store_relay_test.go` | relay client mock returning each error class |
| `token_test.go` | both `RelayBorrowedTokenSource` and `LocalTenantTokenSource`; cache reuse; explicit invalidate |
| `longconn_test.go` | Feishu SDK fake — handshake, event delivery, reconnect on close, auth-class reject path |
| `service_test.go` | mode selection on startup; live mode-switch on relay login state change |

### 10.2 Unit — `cmd/atterm-hook/main_test.go`

Run with `os/exec` against the compiled binary, fed via stdin and
mock localhost endpoints (httptest). Cases:

- Happy POST → exit 0
- Missing `ATTERM_SESSION_ID` → exit 0 silently
- Missing both env and endpoint file → exit 0 silently
- POST timeout (1s) → exit 0
- POST 5xx → exit 0
- 64 KB stdin → POST goes through; >64 KB → drop with stderr

### 10.3 Existing test changes — relay side and shared lib

- `internal/relay/uplink_feishu_test.go` — DELETE (the paths it tested
  are gone)
- `internal/feishu/service_test.go`:
  - DELETE `TestService_SendCommandFinished_NoBinding`,
    `TestService_SendCommandFinished_HappyPath`,
    `TestService_SendCommandFinished_SkipsDisabled` (the outbound
    methods themselves are deleted)
  - KEEP `TestService_HandleEvent_*` (inbound path stays)
  - ADD `TestService_RelayToken_*` — success (returns token + open_id),
    no-binding (sentinel), disabled (sentinel), upstream-fetch-fail
- `internal/relay/feishu_http_test.go` — add
  `TestFeishuHTTP_RelayToken_*` paralleling the bindings CRUD tests
  (200 / 401 / 404 / 410 / 502)
- `internal/feishu/card_test.go` — add `TestRenderWaitingInputCard_WithQuestion`
  asserting the new `div` containing the truncated code block, and
  `TestRenderWaitingInputCard_QuestionTruncation` for the head-preserve
  truncation rule
- `internal/relay/uplink_conn.go` callers — once the dispatch sites
  vanish, run `go test ./internal/relay/...` to confirm nothing else
  relies on the deleted methods

### 10.4 Integration — `desktop/feishu/integration_test.go`

Compose a stub `httptest.Server` that fakes Feishu's two HTTP routes
plus a tiny stub for the long-connection WS path. Then:

- Boot a `desktop/feishu.Service` in relay-backed mode pointed at a
  fake relay; trigger a synthetic `TaskStateChange(running → completed)`
  via the session callback; assert the stub Feishu IM endpoint
  received the right card body.
- Same in local mode (skipping the relay stub entirely); verify the
  long-connection handshake fires and a CARD_ACTION_TRIGGER event
  flowing in produces an ack reply.
- POST a `HookNotifyRequest` to the dispatcher's hook_server endpoint;
  assert the precise-text card body lands at the stub.

### 10.5 E2E manual — `scripts/feishu-hook-e2e-checklist.md`

Real claude-code, real Feishu app, both modes. Sample steps:

- [ ] install `atterm-hook` into PATH (homebrew or manual)
- [ ] add Notification hooks to `~/.claude/settings.json`
- [ ] log in to a relay account, configure binding, complete pair
- [ ] run `atterm` and start a `claude` session inside
- [ ] ask claude something that triggers AskUserQuestion → wait for
      Feishu card with the question text quoted
- [ ] tap "确认" — card updates to "已确认"
- [ ] tap "跳回打开 session" → atterm:// no-op (still a follow-up PR)
- [ ] log out of relay → repeat configure + pair in local mode →
      send `/bind` over Feishu → confirm long-connection completes
      binding
- [ ] kill atterm and confirm hook-endpoint file is removed; relaunch
      and confirm new env var values reach a freshly spawned claude

## 11. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| claude-code's `idle_prompt` matcher fires far more often than it should ([cc#12048][cc12048]) | medium | Filter rule in adapter: only emit when matcher is `permission_prompt` or `idle_prompt + tool == AskUserQuestion`. Document the caveat. |
| codex hook cannot expose `waiting_for_input` events ([codex#19921][codex19921]) | medium | Designed-in HookAdapter slot; user-visible behavior for codex is the heuristic fallback (which we still wire up). |
| `atterm-hook` binary distribution gets out of sync with desktop's wired endpoint contract | medium | Bake a `hook_version` field into `HookNotifyRequest`; server rejects unknown majors with a clear error; document SemVer policy |
| Feishu Go SDK (`github.com/larksuite/oapi-sdk-go/v3`) adds large dependency footprint | medium | Accepted. Alternative — hand-rolled WS subset — doubles effort; SDK has been used elsewhere in Chinese-market Go shops without notable issues |
| Local mode's long-connection competes with relay-backed registration of the same Feishu app | low | Both modes use the same app_id in user config; spec says they don't co-exist on the same desktop. UI enforces "switch to relay mode → uninstall local hooks first" |
| Hook endpoint file on disk allows local sniffing | low | The file contains `http://127.0.0.1:<port>`; any local user with shell access already has equivalent reach to atterm session state. Threat model unchanged. |
| Per-PR diff size — ~22 plan tasks | medium | Plan splits by slice A–E; subagent-driven-development executes each task in isolation; each task targets < 200 LOC delta |

## 12. Non-goals — repeated for emphasis

- Mobile / web sending paths
- Codex precise hook (deferred)
- aider precise hook (heuristic only)
- Group-chat Feishu sends
- Card actions beyond `ack`
- Multi-Feishu-account per user
- Auto-migration between relay-backed and local keychain storage

## 13. Open questions

None blocking the plan. Items deliberately deferred or recorded for
the next revision:

- A "test send" button in the Feishu settings tab — UX polish, can
  land in the same PR series as a bonus task if cycle time allows.
- Optional finer per-event toggles (e.g. "don't notify me of
  command_finished, only AI waiting_input") — accepted noise floor for
  P1.
- Mobile / web read-only viewing of card actions ("see what was acked
  on which device") — out of scope; the action results live in Feishu.

## 14. Out-of-scope, recorded for context

These were considered and explicitly cut during brainstorming —
recorded so a future reader does not re-argue them.

- **Plaintext bypass for E2EE** (send question text through relay in
  plaintext when user opts in) — rejected because (a) the relay
  becomes a meaningful trust boundary, and (b) the desktop-direct
  path obviates the question entirely once the architecture lands.
- **Mobile sending** — rejected because mobile clients are session
  viewers, never PTY hosts.
- **Auto-migration between modes** — rejected per CLAUDE.md preference
  for replace-over-deprecate; manual re-config is acceptable.
- **A single-binary "atterm + atterm-hook"** approach (subcommand,
  not separate CLI) — rejected because the wails-built `atterm`
  binary is large (~80 MB) and bundling it in a hot hook path slows
  every claude-code response.
