# Feishu as Terminal — Design

**Date:** 2026-06-26
**Status:** Draft, pending review
**Related work:**
- 2026-06-17 feishu-app-integration
- 2026-06-17 feishu-hook-question
- 2026-06-25 feishu-interactive-cards
- 2026-06-26 feishu-ai-only-notifications
- 2026-05-14 driver-viewer-mode
- 2026-05-24 driver-viewer-mirror-reconciliation
- 2026-05-24 remote-viewer-count

## Summary

Extend the existing Feishu integration from "notification + AskQuestion card callback" into a lightweight **remote console** for atterm sessions. A Feishu user can attach to a running atterm session over DM, see its output streamed live into an **anchor card**, and inject input via two paths: replying to the anchor card with text, or submitting the anchor card's built-in input element. Both shell sessions and AI sessions (Claude Code) are supported, with type-specific output rendering. This is a **personal-use** feature (1:1 DM, no group support) intended as a **supplement** to the existing iOS / Web / desktop entrypoints — not a replacement.

The model is: the Feishu side becomes another `session.Subscriber` (alongside local terminal and Web viewers), with full driver/viewer semantics.

## Goals & non-goals

### Goals (this round, "F1–F10")
- See live PTY output of any active atterm session in Feishu DM, type-aware (shell vs AI).
- Send commands / control keys to an attached session from Feishu, via reply or in-card input.
- Sensible defaults: AI sessions auto-attach, shell sessions opt-in; user keeps control via admin panel toggle.
- Respect driver/viewer: default to viewer; explicit "preempt driver" toast when input requires it.
- Permission: every inbound action verified against `binding.open_id`.
- Failure isolation: any Feishu-side failure cannot affect the session itself.

### Non-goals (now)
- Full-screen TUI fidelity (vim, htop, less): out of scope, ChatOps abstraction cannot express these.
- Readline emulation (Tab completion, arrow-key history): cost > benefit.
- Group chat / multi-user shared sessions: scope is strictly personal 1:1 DM.
- Replacing the local terminal as the primary input.
- `claude -p` spawn-style integration (would change atterm's identity from terminal multiplexer to LLM host).

### Deferred (P2)
- "Full output archive card" — log viewing falls back to desktop / web for now.
- DM slash commands (`/list /attach /new /exit`) — `SessionAutoAttach` covers ~90% of cases.
- WebSocket long-connection mode — start with webhook to keep desktop runtime simple.
- Token-level AI streaming — current per-turn hook granularity is sufficient.
- Card 2.0 collapsible panels / rich forms inside the anchor — streaming mode constraints make this expensive; revisit if anchor-card UX feels limited.
- Dead-card cleanup after atterm restart — known cosmetic gap, not worth the scan logic for a single-user product.

## Architecture overview

Three new modules; everything else is reused.

```
            ┌─────────────────────────────────────────────┐
            │  Feishu DM (user ↔ atterm bot)              │
            │                                             │
            │   ┌────────────────┐                        │
            │   │ anchor #5      │ ◄── stream PATCH ──┐   │
            │   │  status / tail │                    │   │
            │   │  input / btns  │ ──── input ─────┐  │   │
            │   └────────────────┘                 │  │   │
            │   ┌────────────────┐                 │  │   │
            │   │ anchor #7      │                 │  │   │
            │   └────────────────┘                 │  │   │
            └────────────────────────────────────┼──┼───┘
                  inbound (text reply + card action)         │  │
                                                ▼            │  │
              ┌───────────── router.go (NEW) ──────────────┐ │  │
              │  · reply target msg_id → session_id        │ │  │
              │  · card_token         → session_id         │ │  │
              │  · open_id permission check                │ │  │
              │  · 500ms route budget; PATCH happens async │ │  │
              └────────────────┬───────────────────────────┘ │  │
                               ▼                              │  │
              ┌─────────────── session.Registry ────────────┐ │  │
              │  Session (PTY)                              │ │  │
              │   ├─ Subscriber: local terminal (driver)    │ │  │
              │   ├─ Subscriber: Web (viewer)               │ │  │
              │   └─ Subscriber: FeishuSubscriber (NEW) ────┼─┘  │
              │       · TypeIn  ← from router               │    │
              │       · TypeOut → outbound chunker          │    │
              └─────────────────┬───────────────────────────┘    │
                                ▼                                │
              ┌──────── outbound chunker (NEW) ─────────────────┐│
              │  Shell:  PTY bytes → ANSI strip → tail 5KB      ││
              │  AI:     hook events → per-turn → tail 5 turns  ││
              │  Both:   100ms throttle, diff-skip, PATCH       │┘
              └─────────────────────────────────────────────────┘
```

| Module | Where | Reuses |
|---|---|---|
| `FeishuSubscriber` | `internal/feishu/subscriber.go` (new) | `session.Subscriber` interface, `proto.Frame` |
| `router.go` | `internal/feishu/router.go` (new) | existing `event.go` decryption, `userstore` binding lookup |
| Outbound chunker | `internal/feishu/outbound.go` (new) | `internal/session/ansistrip.go` for ANSI strip |

Existing code (untouched in this round): `card.go`, `client.go`, `dispatcher.go`, `event.go`, `service.go`, `hook_server.go`, `relay/feishu_http.go`. Anchor-card rendering will add new functions in `card.go` but won't alter existing `RenderCommandFinishedCard` / `RenderAskQuestionCard`.

## Data flow

### Outbound: PTY → anchor card

```
Session PTY
    │
    ▼
FeishuSubscriber.OnFrame(TypeOut, data)
    │
    ▼
outbound buffer (per session)
    │  flush on any of:
    │   · 100ms window expired
    │   · buffer ≥ 4KB
    │   · saw \n and >50ms since last flush
    ▼
type-specific render:
   shell → ANSI strip → rolling tail (last 5KB OR last 30 lines, whichever yields a smaller body — guarantees we stay clear of the 30KB card cap)
   ai    → drop bytes; hook adapter produces structured turns instead
    ▼
diff vs last sent body — skip if identical (quota saver)
    ▼
CardKit streaming PATCH anchor body markdown
    │  hard floor: ≥100ms between PATCHes (10/s cap with margin)
    │  on PATCH error: 1 backoff retry, then drop + log
    ▼
Feishu app renders incremental update (platform handles typewriter diff)
```

### Outbound: AI session (hook-driven)

```
Claude Code hook events
   (UserPromptSubmit, PreToolUse, PostToolUse, Stop)
    │
    ▼
hook_server.go (existing) → ClaudeCodeAdapter (existing)
    │  new: pass non-AskQuestion events to outbound chunker too,
    │       not just AskQuestion (which still goes to RenderAskQuestionCard)
    ▼
per-turn aggregation:
    · UserPromptSubmit         → start new turn "👤 你:"
    · Stop (assistant final)   → finalize current turn "🤖 Claude:"
    · PreToolUse               → header subtitle "调用中…" + nest indicator "▸ <ToolName>"
    · PostToolUse              → tool output appended under nest
    ▼
rolling window: keep last N=5 turns in body
    ▼
CardKit PATCH anchor body markdown
```

### Inbound: Feishu → PTY

```
Feishu event arrives at /v1/feishu/events/{appIDHash}
    │
    ▼
event.go decrypt + verify token (existing)
    │
    ▼
ParseEnvelope routes to either:
    · im.message.receive_v1   (text reply)
    · card.action.trigger     (in-card input or button click)
    ▼
router.go (NEW):
    · text reply  → reply_in_thread_id → msgIDToSession → session_id
    · card action → card_token        → tokenToSession  → session_id
    │
    ▼
permission check: event.operator.open_id == session.owner.open_id
    │  fail → toast "无权限" + drop
    ▼
driver check:
    · no driver           → FeishuSubscriber.ClaimDriver(); inject TypeIn
    · driver = self       → inject TypeIn
    · driver = other      → push "preempt" toast card; do NOT inject
    ▼
session.SendInbound(proto.Frame{Type:TypeIn, Payload:text})
    ▼
ack within 500ms (rest is async); response can carry toast / update card
```

### Five-key shortcut buttons

Anchor card's button row sends preset byte sequences as `TypeIn`:

| Label | Bytes | Notes |
|---|---|---|
| `^C` | `0x03` | SIGINT to foreground |
| `^D` | `0x04` | EOF |
| `Esc` | `0x1B` | for menu cancel, vim escape (acknowledging vim is non-goal but Esc is cheap) |
| `Enter` | `0x0D` | newline submit for prompts that need explicit Enter |
| `结束 (End)` | (special) | detaches FeishuSubscriber; archives anchor; does NOT terminate the session |

## Anchor card

### Schema (JSON 2.0)

```
header:
  title:     "▸ session #{N} · {title}"
  subtitle:  "状态：{running|idle|stopped} · driver: {name} · {elapsed}"
  template:  blue (running) / green (idle) / grey (stopped) / red (error)

body (single markdown element — required by streaming mode):
  shell:   ``` ... last 5KB OR last 30 lines, whichever yields fewer bytes ... ```
  ai:      mixed markdown with emoji-prefixed sections
           👤 user prompt
           🤖 assistant response
             ▸ tool name
             ```tool output```

input:     single-line text element (Card 2.0 input component)
           submit triggers card.action.trigger with value.kind="input"

actions (button row): ^C  ^D  Esc  Enter  [结束]
  [结束]  → detach FeishuSubscriber; archive anchor

footer (only set on archive): "已结束 at {timestamp}" — set once during teardown.
```

### Lifecycle

```
Session created elsewhere (local / web)
        │
        │  attach decision per binding.SessionAutoAttach:
        │    "ai"   → only for AI sessions
        │    "all"  → all sessions
        │    "none" → never auto; explicit /attach only (P2)
        ▼
FeishuSubscriber.Attach
        │  POST anchor card; store (sessionID, msgID, cardToken, ownerOpenID)
        ▼
Running: outbound PATCHes body / header
         inbound reads cardIndex by msgID / cardToken
        ▼
Session exits OR user clicks [结束] OR RemoteTerminalEnabled flipped off
        │  one final PATCH:
        │    · header → grey
        │    · drop input element + action buttons
        │    · set footer "已结束"
        │    · remove from cardIndex
        ▼
Card sits in DM history (no further updates)
```

### cardIndex

```go
type CardAnchor struct {
    SessionID    string
    CardMsgID    string    // im_message id, for reply path
    CardToken    string    // cardkit token, for PATCH and in-card input
    OwnerOpenID  string    // for permission gate
    CreatedAt    time.Time
    LastPatchAt  time.Time // throttle reference
    LastBody     string    // diff-skip
}

sessionToAnchor map[string]*CardAnchor   // session_id → anchor
msgIDToSession  map[string]string        // reply target → session
tokenToSession  map[string]string        // card action → session
```

Held in memory only. atterm restart drops the map; old anchors become "dead cards" (known limitation, see §Failure modes).

## Binding & permission

### Binding model
- 1:1 between atterm user (single-machine) and `open_id` — already enforced by `userstore/feishu_bindings`.
- 1:N between `open_id` and atterm sessions — each session gets its own anchor card in the same DM.
- No new table; extend `feishu_bindings` with:
  - `RemoteTerminalEnabled bool` — master switch
  - `SessionAutoAttach string` — `"ai" | "all" | "none"`, default `"ai"`

### Permission rule
Every inbound action must satisfy `event.operator.open_id == binding.open_id`. On mismatch: toast "无权限", drop, no log spam.

This was a P0 security gap before remote terminal; with remote terminal it becomes a hard prerequisite — input is no longer occasional ("click button on AskQuestion") but continuous ("typing into PTY").

### Driver protocol

| Current driver | New input from Feishu | Action |
|---|---|---|
| none | inject | FeishuSubscriber.ClaimDriver, inject TypeIn |
| FeishuSubscriber | inject | inject TypeIn |
| local terminal / Web | inject | push preempt toast: `session #{N} 当前由 {name} 接管 [抢占] [我先看着]`. No silent steal. |

Viewer (output-only) does not require driver; FeishuSubscriber starts as viewer.

### Master switch off / binding removed

| Trigger | Effect |
|---|---|
| `RemoteTerminalEnabled = false` | Detach all FeishuSubscriber; PATCH each anchor to grey with "远程接管已关闭" footer; clear cardIndex; sessions unaffected; normal notification cards continue. |
| Binding deleted | Same as above plus stop receiving Feishu events. |

### Viewer count

FeishuSubscriber counts as a Relay viewer (PR #77 `TypeViewers`). Recommendation: count Feishu attaches separately so they don't squeeze the Web viewer cap. Concrete counting deferred to plan stage.

## Failure modes

**Rule:** Feishu-side failure never affects the session.

### Outbound errors

| Error | Action |
|---|---|
| PATCH 429 / 99991400 | Should not occur with 100ms throttle; if it does, log + skip this patch (next accumulates). |
| PATCH 404 / 230030 (card deleted / not found) | Remove from cardIndex; do not error to session. New anchor pushed on next significant event with 30s cool-down. |
| PATCH 5xx | 1s backoff retry once; persistent fail → skip + log. |
| `tenant_access_token` expired | Existing `TenantTokenCache` refreshes automatically. If refresh fails, FeishuSubscriber enters "degraded": stop patching but do not detach. Periodic probe every 60s. |

### Inbound errors

| Error | Action |
|---|---|
| Signature / token verify fail | Return 401, no body (existing `event.go` behavior). |
| `open_id` mismatch | Toast "无权限", drop. |
| Session no longer exists | Toast "会话已结束", and best-effort PATCH anchor to archived state (catches anchors missed by normal teardown). |
| Session exists but PTY refusing input | Toast "输入未被接收", PATCH header status. |
| Reply target not in msgIDToSession | Toast "找不到对应会话，请通过新锚卡操作". |
| 3-second response window pressure | Hard rule: 500ms budget for route+permission+inject; anchor PATCH is async, never on the ack path. |

### Process / network

| Scenario | Behavior |
|---|---|
| atterm restart | cardIndex empties. Old anchors become "dead cards" (buttons return "卡片已过期" toast; cosmetic header doesn't grey out — known gap). New sessions push fresh anchors per autoAttach. |
| Feishu webhook retry | Idempotency via existing `event_id` dedupe (30s window in dispatcher). |
| PTY output burst (compile / log flood) | Outbound chunker's 100ms+5KB caps absorb naturally. |

### Specific traps

| Trap | Defense |
|---|---|
| Reply target may not be an anchor (could be old AskQuestion card, plain toast, etc) | `msgIDToSession` lookup is strict; miss → "无法识别目标会话" toast. No guessing. |
| In-card input rapid double-submit | Clear input on submit; 5s same-token dedupe (reuse dispatcher's 30s window). |
| AI hook ordering: PreToolUse → tool runs → PostToolUse | Flush at PostToolUse granularity; PreToolUse only updates header subtitle to "调用中…", does not split a half-rendered turn. |
| Cross-process consistency (hook server vs relay) | Reuse existing `hook_server.go` 127.0.0.1 IPC; no new IPC channel. |

## In-scope feature set (F1–F10)

| # | Feature |
|---|---|
| F1 | `FeishuSubscriber` implementing `session.Subscriber`; attach pushes anchor, detach archives. |
| F2 | Anchor card schema v1 (JSON 2.0): header + body + input element + 5-button row (`^C` `^D` `Esc` `Enter` `结束`). |
| F3 | Outbound chunker for shell: PTY → 100ms buffer → ANSI strip → rolling tail (≤5KB AND ≤30 lines) → CardKit PATCH. |
| F4 | Outbound chunker for AI: hook events → per-turn → rolling 5 turns → CardKit PATCH. |
| F5 | Inbound router: reply target / card_token → session_id; 500ms route budget. |
| F6 | `open_id` permission check on every inbound event. |
| F7 | Driver protocol: default viewer, explicit preempt toast on driver conflict. |
| F8 | Master switch `RemoteTerminalEnabled` + `SessionAutoAttach` (`ai` / `all` / `none`) persisted on binding. |
| F9 | Failure containment: PATCH-fail removes anchor, token refresh, PTY isolation. |
| F10 | Admin UI: extend `web/src/admin/tabs/FeishuConfig.vue` with remote-terminal toggle + autoAttach dropdown. |

## Delivery plan

```
Phase 1 — MVP (~1 week)
  F1 + F2 + F3 + F5 + F6 + F8
  Goal: shell session reachable end-to-end; reply to inject; live tail in anchor.

Phase 2 — AI experience (~1 week)
  F4 + F7 + F9
  Goal: AI sessions stream per turn; driver preempt toast works; failure-isolation hardened.

Phase 3 — UI + polish (~3 days)
  F10 + docs + deploy checklist (the three Feishu console steps + open_id verification surfaced)
  Goal: user can flip it on from admin panel; first-run avoids the 200340-class traps.
```

## Testing strategy

- **Unit:** outbound chunker (throttle, diff-skip, rolling window); ANSI strip (leverage existing `internal/session/ansistrip_test.go` patterns); router permission logic.
- **Integration:** mock Feishu API for PATCH-failure → anchor removal, 429 → throttle, token expiry → degraded mode + recovery.
- **E2E:** run a real shell session and a real Claude Code session against a real Feishu DM; verify reply path and in-card input path; verify driver preempt UI; verify archive on session exit.
- **Regression:** existing `RenderCommandFinishedCard`, `RenderAskQuestionCard`, AI-only switch behavior must be unchanged.

## Open questions deferred to plan stage

- Exact concurrency limits for FeishuSubscriber count vs Web viewer cap (existing `TypeViewers` 0x36 frame).
- Whether `SessionAutoAttach` `"all"` is exposed in the first UI cut or only via config file initially.
- Storage format for `RemoteTerminalEnabled` / `SessionAutoAttach` — additive columns on `feishu_bindings` vs a new related table.
- Whether hook adapter changes warrant a new schema version on `claudeCodeAdapter` (current adapter is consumed only by AskQuestion path; AI streaming path extends consumption).
