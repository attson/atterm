# Feishu Remote Terminal — Deployment Checklist

The remote-terminal feature turns a Feishu DM into a lightweight remote console:
every atterm session gets a per-session anchor card that streams output live, and
the user can inject input by replying to the card or using its in-card input
element. Before users enable the master switch in the per-account settings, the
Feishu app must be configured correctly. Skipping any step below results in the
classic `200340` error ("出错了, 请稍后重试") when users press card buttons, or
silently dropped events.

## Required Feishu app configuration

These steps happen in the [Feishu Developer Console](https://open.feishu.cn/app)
on the page for your atterm app.

1. **Subscribe to `card.action.trigger` event**
   Event Subscription → add `card.action.trigger`. Without this, card button
   presses never reach atterm.

2. **Enable interactive card capability**
   App Capabilities → Interactive Cards → toggle on.

3. **Configure the encrypted event request URL** (relay/webhook mode only)
   Event Subscription → Request URL →
   `<your atterm relay base URL>/v1/feishu/events/{appIDHash}`. The exact URL is
   shown in the per-account Feishu settings panel after the binding is created
   (Settings → Feishu → "事件订阅地址").

   Long-connection (WebSocket) mode does not need this URL — the desktop's
   `LongConn` registers with Feishu directly. Most desktop installs use this
   mode and can skip step 3.

## Encryption + signing prerequisites

Both must be set on the binding and they must match what is configured in the
Feishu Developer Console:

- **`encrypt_key`** — used to AES-256-CBC decrypt incoming events. The atterm
  relay rejects non-encrypted requests
  (`internal/feishu/event.go:ErrNotEncryptedBody`), so this is non-optional.
- **`verification_token`** — verified with constant-time compare on every event
  (`internal/feishu/event.go:VerifyEnvelopeToken`).

Both fields are surfaced in the per-account settings UI.

## Permission model

- Every inbound event (text reply or card action) is gated by
  `event.operator.open_id == binding.open_id`. Foreign open_ids see a "无权限"
  toast and the input is dropped.
- Group chats are out of scope: the bot must be in a 1:1 DM with the user who
  owns the binding. Don't add the bot to shared rooms.
- The `RemoteTerminalEnabled` master switch (Settings → Feishu → "远程接管")
  must be on. While off, anchor cards are not created and no input is accepted
  — only the existing notification cards
  (`CommandFinished`, `AskQuestion`) continue to work.

## AutoAttach modes (per binding)

| Mode | Behavior |
|---|---|
| `ai` (default) | An anchor card is created automatically when a session is classified as AI (e.g. `claude`, `codex`). Shell sessions stay out. |
| `all` | Every new session gets an anchor card. |
| `none` | No auto-attach. (Manual `/attach` command is P2 — not yet available.) |

Switch via Settings → Feishu → "自动接管会话" dropdown.

## Operational caveats

- **atterm restart drops all in-memory anchor cards.** Existing cards in the
  DM become inert ("dead cards") — buttons return "卡片已过期" toast and the
  body stops updating. New sessions push fresh anchors as normal.
- **Tail rendering is bounded.** Each anchor's body keeps only the last
  ~30 lines / 5 KB of shell output, or the last 5 AI conversation turns. For
  full output, use the local atterm desktop / web entry.
- **Single-card limits.**
  - 10 PATCH operations per second per card. The chunker throttles to ≤10/s.
  - 30 KB card body limit. The rolling tail keeps each PATCH body ≤5 KB
    (well under the cap, with margin for header/footer overhead).
  - 3-second callback response window. The inbound router enforces a 500 ms
    route budget; anchor PATCHes always run asynchronously to avoid blocking
    the ack path.
- **Driver preempt.** If the local terminal or another viewer is currently the
  session's driver, Feishu input returns an `ActionPreempt` decision and the
  user sees a preempt toast — Feishu does NOT silently steal driver.
- **Master switch flip → off.** All FeishuSubscribers are detached
  immediately; their anchors are archived (grey footer); sessions themselves
  keep running. New attachments require a flip back on AND a new significant
  event (new session, or first AI-classification on an existing session).

## Failure containment

- All Feishu API failures stay isolated from the PTY. The hard rule is "Feishu
  errors never affect session behaviour."
- PATCH 5xx → 1-second backoff retry, then drop the update and log.
- PATCH 230030 / 404 (card deleted by user) → anchor is removed from the
  registry; the FeishuSubscriber is detached for that session.
- `*AuthClassError` (token expired) → the per-process `TenantTokenCache`
  refresh path is left to recover on the next call; the failing PATCH is
  logged and dropped without retry.

## Verification after configuration

1. In Settings → Feishu, bind the app and confirm "已绑定" appears.
2. Toggle "启用远程接管" on. Set autoAttach to "AI sessions only" (the
   default).
3. Open a Claude Code session in atterm.
4. Within ~1 second, the bound Feishu DM should receive a fresh anchor card
   for the session. Click any button or send a reply text — the action should
   reach the session within ~1 second.

If buttons return "出错了, 请稍后重试": revisit the required configuration
steps at the top of this file.
