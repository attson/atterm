# Web Push Command-Finished Notifications Design

**Date:** 2026-05-15
**Status:** Approved

## Goal

Deliver "command finished" notifications to browsers (desktop and mobile PWA) via Web Push, so users get pushed even when the page is not open. Self-hosted: the relay sends the Web Push requests itself; no third-party services.

This is the second stone of the Phase 3 roadmap item *"离开电脑后继续看任务"* in `docs/spec/architecture.md`, building on the OSC 133 shell integration shipped in v0.1.55.

## Motivation

OSC 133 already lets the desktop frontend fire a local system notification when a command finishes with the window unfocused. That covers the "I'm at the laptop but in another app" case; it does not cover *"I left the laptop, took my phone to lunch, want a ping when build is done."* Web Push fills that gap.

Web Push is the only browser API that lets a remote server deliver a notification while the browser tab is not open. iTerm2 / Warp / etc. cannot do this — they're terminal emulators with no remote component. atterm has a relay already; Web Push is the natural use of it.

## Non-Goals

- Pushing BEL or any non-OSC-133 event in this iteration. The detection layer for OSC 133 is established and small; BEL is a bigger semantics question (zsh autocomplete bell vs claude/codex bell) that should wait.
- Pushing session lifecycle events (start / exit / crash) in this iteration. Deferrable.
- Re-implementing the Web Push spec (RFC 8030 + 8291) from scratch. We use `github.com/SherClockHolmes/webpush-go`.
- Routing pushes via FCM / Pushover / OneSignal / any third-party. Self-hosted is a hard product principle.
- Push deduplication or retry queues. The push services already provide delivery semantics; we don't double-buffer.
- Notification customization (sound, icon per-event, action buttons). MVP shows title + body + tag.
- Push permissions / VAPID rotation UI. Manual file edits if ever needed.
- Persisting events to disk on relay for later replay. Web Push is fire-and-forget.

## Behavior

### Event lifecycle (per command finish)

1. Shell prints `OSC 133;D;<exit>` after user's command completes (shell integration from v0.1.55).
2. Desktop frontend's `CommandTracker` (in `TerminalView.vue`) parses it into a `CommandEvent`.
3. `shouldNotifyCommand({focused, thresholdSec, isLocal})` runs (existing — three-gate logic from v0.1.55).
4. If the gate passes:
    a. Local OS notification fires via existing `showNotification` Wails binding (v0.1.55 behavior — unchanged).
    b. **New:** A `BroadcastCommandFinished(sessionId, exitCode, elapsedMs, label)` Wails binding is invoked.
5. Desktop Go side (`uplink.go`) wraps the event in a `TypeCommandEvent` frame and writes it through its existing single-writer goroutine to the connected relay.
6. Relay (`uplink_conn.go`) verifies the session_id is in this uplink's current ANNOUNCE manifest, then calls `webpush.Service.DispatchCommandFinished(...)`.
7. The webpush service runs the fanout in a goroutine: resolve which token-hashes can view this session, look up each token's subscriptions, POST a Web Push request to each endpoint with VAPID auth.
8. Push services (FCM / Mozilla autopush / Apple push gateway) deliver the encrypted payload to the user's browser / PWA.
9. The browser's Service Worker (`web/sw.js`) receives the `push` event and calls `self.registration.showNotification(title, opts)`.

### Trigger gates (recap from v0.1.55)

All gates are evaluated on the desktop frontend; the relay does not re-gate.

- **Focus gate**: AT Term window is not focused.
- **Threshold gate**: command's start-to-finish duration ≥ `commandNotifyThresholdSeconds` (default 10, range 1–600).
- **Local-session gate**: pane is attached to a local session (not a cast-attached remote).

Web Push fires when local notification would have fired. Same threshold setting applies. Overlap (local OS notification + phone push) is acceptable.

### Subscription model

- A subscription is bound to a *token hash* (sha256 of the relay token, base64url) — not a token plaintext.
- A subscription record carries the browser-issued `endpoint`, `keys.p256dh`, `keys.auth`, and a server-set `created_at`.
- A token may hold up to 16 subscriptions (different browsers / PWAs). Additional subscribe calls beyond the cap are silently dropped server-side (still 200 to keep the client flow simple).
- Subscriptions persist to `<RELAY_CONFIG_DIR>/web-push.json` (write-temp-rename). Survives relay restart.
- A token's subscriptions receive pushes for any session that token can view, per the existing token-scope + owner `remote_permission` intersection (`internal/relay/permissions.go`).

### Notification payload (relay → browser)

```json
{
  "title": "AT Term · <label>",
  "body": "Command finished · exit <code> · <elapsed>",
  "tag": "<session_id>",
  "data": {
    "exitCode": 0,
    "elapsedMs": 12500,
    "sessionId": "<uuid>",
    "hostId": "<uuid>"
  }
}
```

- `<label>` is the `sessionLabel` prop passed in from `PaneGrid` (basename of cwd or command, falls back to `"session"`).
- `<elapsed>` matches the desktop-side `formatElapsed`: `12s` for sub-minute, `1m12s` otherwise.
- `tag = session_id` so consecutive pushes for the same session collapse on the lock screen (the latest replaces older).

### Configuration

Relay-side:

| Flag / env | Default | Notes |
|------------|---------|-------|
| `--vapid-subject` / `ATTERM_VAPID_SUBJECT` | `mailto:noreply@atterm.local` | RFC 8292 VAPID subject. Goes into the JWT, not into subscription state. Can be changed without invalidating subscriptions. |
| `--config-dir` / `ATTERM_RELAY_CONFIG_DIR` | `./data/atterm-relay` | Existing flag. Web Push file lives in here. |

No frontend opt-in flag — Web Push is an additive feature, always on when the relay supports it.

Browser-side:

| Setting | Default | Notes |
|---------|---------|-------|
| Push enabled per browser | off | User opts in via "🔔 Enable notifications" button. Persisted in `localStorage`. |

The existing `commandNotifyThresholdSeconds` from v0.1.55 is the threshold for both local desktop notifications and Web Push (since they share the same gate).

## Components

### Backend

- **`internal/webpush/` (new package)** — VAPID + subscription state + dispatch. Public surface:

  ```go
  type Service struct{ /* internal */ }

  func Open(dir, vapidSubject string) (*Service, error)

  func (s *Service) PublicKey() string
  func (s *Service) AddSubscription(tokenHash string, sub Subscription) error
  func (s *Service) RemoveSubscription(tokenHash, endpoint string) error
  func (s *Service) DispatchCommandFinished(ev CommandFinished)
  // SendTest returns the number of subscriptions a push was dispatched for.
  func (s *Service) SendTest(tokenHash string) int
  func (s *Service) SetSessionResolver(f func(uuid.UUID) []string)

  type CommandFinished struct {
      SessionID uuid.UUID
      HostID    uuid.UUID
      ExitCode  int
      ElapsedMS int
      Label     string
  }

  type Subscription struct {
      Endpoint  string `json:"endpoint"`
      Keys      struct {
          P256dh string `json:"p256dh"`
          Auth   string `json:"auth"`
      } `json:"keys"`
      CreatedAt int64  `json:"created_at"`
  }
  ```

  Files: `service.go`, `vapid.go`, `subscription.go`, `persist.go`, `dispatch.go`, `transport.go` (a thin wrapper around `webpush-go.SendNotificationWithContext` for testability via an injected HTTP client), plus tests as listed under Testing. Subscription cap is a `const maxSubsPerToken = 16` inside `subscription.go`.

  `Open` only returns a non-nil error for fatal initialization failures (e.g., the OS denies crypto entropy for VAPID keypair generation — practically impossible). All recoverable conditions (corrupt persistence, unwritable dir, missing subject) downgrade in place: log a one-time WARN and continue with sensible defaults, returning a non-nil `*Service`. `main.go` is still free to set `WebPush = nil` if it wants to fully disable the feature on a particular condition. All relay HTTP endpoints and the uplink frame handler must nil-check `WebPush`.

- **`internal/proto/`** — Add `TypeCommandEvent Type = 0x35`. Payload JSON:

  ```go
  type CommandEventPayload struct {
      ExitCode  int    `json:"exit_code"`
      ElapsedMS int    `json:"elapsed_ms"`
      Label     string `json:"label,omitempty"`
  }
  ```

  Direction: `uplink → relay` only. Not relayed to clients. The session_id rides the frame header (existing pattern). The host_id is *not* in the payload — relay reconstructs it from the uplink's manifest at handler time to prevent spoofing.

- **`internal/relay/uplink_conn.go`** — In the read loop, add a case for `TypeCommandEvent`:
  - Parse payload.
  - Verify `frame.SessionID` is currently in this uplink's manifest. If not, log debug and drop.
  - Look up `host_id` from this uplink's manifest.
  - Call `server.WebPush.DispatchCommandFinished(...)` if `server.WebPush != nil`.
  - Truncate `label` to 256 chars defensively.

- **`internal/relay/web_push_http.go` (new file)** — Four endpoints, all under the existing token-auth middleware:
  - `GET /api/push/key` → `{ "key": "<base64url vapid pub>" }`. Allowed for write and read tokens; 401 otherwise. Returns 503 if `server.WebPush == nil`.
  - `POST /api/push/subscribe` body `Subscription` → validates endpoint is https + keys have plausible length + non-empty → calls `WebPush.AddSubscription(tokenHash, sub)` → 200 `{ "ok": true }`. 400 on validation failure. 503 if disabled.
  - `POST /api/push/unsubscribe` body `{ "endpoint": "..." }` → idempotent. 200 always (even if endpoint not found).
  - `POST /api/push/test` → calls `WebPush.SendTest(tokenHash)` → 200 `{ "sent": <n> }`.

- **`internal/relay/server.go`** — `Config` gains `WebPush *webpush.Service`. `NewServer` keeps a reference. Provides `WebPushSessionResolver` that the wiring code in `main.go` passes into `service.SetSessionResolver`. The resolver iterates over the session's `remote_permission` and the registry's authorized tokens to compute "which token-hashes can view this session".

- **`cmd/atterm-relay/main.go`** — At startup, after parsing flags:
  ```go
  wpDir := cfg.ConfigDir // existing flag
  wpSvc, err := webpush.Open(wpDir, *vapidSubject)
  if err != nil {
      log.Printf("WARN: web-push disabled: %v", err)
      wpSvc = nil
  }
  relayCfg.WebPush = wpSvc
  ```
  Then after `NewServer`, wire the resolver:
  ```go
  if wpSvc != nil {
      wpSvc.SetSessionResolver(server.WebPushSessionResolver)
  }
  ```

### Desktop

- **`desktop/uplink.go`** — Add a public method:
  ```go
  func (u *Uplink) SendCommandEvent(sessionID uuid.UUID, exit, elapsedMS int, label string)
  ```
  Constructs the new frame and pushes to the existing out chan. If `u == nil` or the WS is not connected, silently drops. The drop is safe — local OS notification has already fired and Web Push misses are acceptable.

- **`desktop/app.go`** — New Wails binding:
  ```go
  func (a *App) BroadcastCommandFinished(sessionID string, exitCode, elapsedMS int, label string)
  ```
  Parses sessionID UUID, hands off to `a.uplink.SendCommandEvent`. Failures (invalid uuid, no uplink) are silent.

### Frontend (desktop)

- **`desktop/frontend/src/lib/api.ts`** — Add wrapper:
  ```ts
  export function broadcastCommandFinished(sessionId: string, exitCode: number, elapsedMs: number, label: string): Promise<void>
  ```
  Plus the matching `AppBindings` entry.

- **`desktop/frontend/src/components/TerminalView.vue`** — In the existing OSC 133 handler, after the `if (passed)` branch fires `showNotification`, also call:
  ```ts
  void broadcastCommandFinished(props.sessionId, ev.exitCode, ev.elapsedMs, props.sessionLabel || "session")
  ```
  This is the *only* desktop frontend change for Web Push. The gate logic is unchanged; we just emit an extra side effect.

### Frontend (web/)

- **`web/sw.js`** — Add `push` event listener:
  ```js
  self.addEventListener("push", (event) => {
    event.waitUntil((async () => {
      let payload = { title: "AT Term", body: "Command finished." };
      try {
        if (event.data) payload = { ...payload, ...event.data.json() };
      } catch (_) { /* keep fallback */ }
      const { title, body, tag, data } = payload;
      await self.registration.showNotification(title, { body, tag, data });
    })());
  });
  ```
  Also bump the `CACHE` constant (`"at-term-web-v3"` → `"at-term-web-v4"`) so caches refresh.

- **`web/app-core.js`** — Add pure helpers:
  - `pushSupported(navigator, window): boolean` — false unless ServiceWorker + PushManager + Notification all present, and not iOS Safari outside PWA mode.
  - `canEnablePush(notificationPermission: string): boolean` — true for `"default"` and `"granted"`, false for `"denied"`.
  - `base64UrlToUint8Array(s): Uint8Array` — for the VAPID public key → `applicationServerKey` conversion.

- **`web/app.js`** —
  - Add a "🔔 Enable notifications" button rendered in the status row once WS is connected and `pushSupported(navigator, window)` is true. Button text and state reflect `localStorage.getItem("push-enabled")` and `Notification.permission`.
  - Add `enablePushFlow()`:
    1. `Notification.requestPermission()` → if not granted, show inline hint, abort.
    2. `await fetch("/api/push/key", { headers: { Authorization: "Bearer " + token } })`.
    3. `await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64UrlToUint8Array(key) })`.
    4. `POST /api/push/subscribe` with the resulting `{ endpoint, keys: { p256dh, auth } }`.
    5. On 200: set localStorage flag, swap button to "🔔 ON".
    6. On error: console.warn, surface a small inline message ("server has push disabled" for 503; "permission denied" for `denied`).
  - Add `disablePushFlow()` mirroring the above, calling `sub.unsubscribe()` and `POST /api/push/unsubscribe`.
  - For iOS Safari (`!navigator.standalone && /iPad|iPhone|iPod/.test(navigator.userAgent)`): replace the button with a one-line hint "Add to Home Screen to enable notifications".

### Documentation

- **`docs/web-push.md` (new)** — User-facing: how to enable, iOS PWA caveat, where state lives, how to wipe + regenerate VAPID. Linked from README.
- **`docs/spec/protocol.md`** — Add `TypeCommandEvent (0x35)` entry to the frame type table and a short section describing direction (uplink → relay only), payload shape, and the host_id spoofing-prevention check.
- **`README.md`** — Add a row in the capability table; add a documentation bullet linking to `docs/web-push.md`.

## Data flow

### Relay startup

```
main.go
  → webpush.Open(configDir, vapidSubject)
     ├─ Read <dir>/web-push.json
     │   ├─ missing → generate P-256 + persist
     │   ├─ ok      → load keypair + subscriptions
     │   └─ corrupt → rename to .corrupt-<ts>.bak + regenerate
     ├─ writable check: if dir is unwritable, run in-memory + WARN
     └─ return *Service
  → relay.NewServer(Config{ WebPush: svc, ... })
  → svc.SetSessionResolver(server.WebPushSessionResolver)
```

If `Open` panics or returns an unhandled error, `main.go` logs ERROR and sets `WebPush = nil`. Relay still starts; the four `/api/push/*` endpoints return 503; `TypeCommandEvent` frames are silently dropped at the uplink handler.

### Subscribe

```
[user clicks 🔔 Enable on web page]
app.js Notification.requestPermission()
   ├─ denied  → inline hint, no API call
   └─ granted ▼
fetch GET /api/push/key with bearer token
   → 200 { "key": "<base64url>" }
sw.ready → reg.pushManager.subscribe({ userVisibleOnly:true, applicationServerKey:<...> })
   → PushSubscription { endpoint, keys: { p256dh, auth } }
POST /api/push/subscribe body=Subscription with bearer token
   ↓
relay web_push_http.handleSubscribe
   ├─ token-auth middleware → tokenHash
   ├─ validate body (endpoint is https; keys non-empty + plausible length)
   ├─ service.AddSubscription(tokenHash, sub)
   │   ├─ if same endpoint exists → overwrite (refresh CreatedAt)
   │   ├─ else if token already has 16 subs → drop silently
   │   └─ else append
   └─ persist <dir>/web-push.json (write-temp-rename)
   → 200 { "ok": true }
   ↓
app.js localStorage["push-enabled"] = "1"; UI → "🔔 ON"
```

### Command-finished push

```
shell prints OSC 133;D;<exit>
   → xterm parser invokes the OSC handler in TerminalView.vue
   → CommandTracker.onOsc133("D;<exit>", now)
       returns { kind: "finished", exitCode, elapsedMs }
shouldNotifyCommand({focused, thresholdSec, isLocal}) === true
   ├─ void showNotification("AT Term", "Command finished · ...")    (unchanged)
   └─ void broadcastCommandFinished(sessionId, exit, ms, label)     (new)
        ↓
desktop/app.go App.BroadcastCommandFinished
   → uplink.SendCommandEvent(sid, exit, ms, label)
        ↓
desktop/uplink.go writer goroutine
   → frame { Type: TypeCommandEvent, SessionID: sid, Payload: JSON({exit, ms, label}) }
   → WS write (single-writer goroutine, existing)
        ============== WSS ==============
relay/uplink_conn.go readLoop case TypeCommandEvent
   ├─ parse payload
   ├─ if sid not in this uplink's manifest → log debug, drop
   ├─ host_id = manifest's host_id
   ├─ truncate label to 256 chars
   └─ go WebPush.DispatchCommandFinished({sid, hostID, exit, ms, label})
        ↓ (async — handler returns immediately)
webpush.dispatch goroutine
   ├─ tokens = sessionResolver(sid)
   ├─ for each tokenHash:
   │   subs := byToken(tokenHash)
   │   for each sub:
   │       go transport.Send(sub, payload):
   │           ├─ 2xx                                   → keep
   │           ├─ 404 / 410                             → service.RemoveSubscription + persist
   │           ├─ 429 / 5xx / timeout / other           → log + keep
   │           └─ panic                                 → recover + log
   ↓
push service (FCM / Mozilla / Apple) → device
   ↓
sw.js "push" event listener
   → self.registration.showNotification(title, { body, tag, data })
   → OS notification (lockscreen / notification center)
```

### Unsubscribe (active)

```
[user clicks 🔔 OFF]
const sub = await reg.pushManager.getSubscription()
await sub.unsubscribe()
POST /api/push/unsubscribe { endpoint }
   ↓
relay → service.RemoveSubscription(tokenHash, endpoint) → persist
   → 200
localStorage["push-enabled"] = null; UI → "🔔 OFF"
```

### Unsubscribe (passive, push service returned 410)

Handled inside the dispatch goroutine — same as path 3's prune step.

### Test notification

```
[user clicks Test on settings]
POST /api/push/test
   → service.SendTest(tokenHash):
       for each sub of tokenHash:
           transport.Send(sub, { title:"AT Term test", body:"It works." })
   → 200 { "sent": <n> }
```

## Error handling

All failures degrade to either "no push delivered" or "subscription pruned". Web Push is never load-bearing for PTY, attach, or local notifications.

### Startup

| Failure | Handling | User effect |
|---------|----------|-------------|
| `<dir>/web-push.json` corrupt | Backup as `.corrupt-<ts>.bak`, regenerate keypair, drop subscriptions | Existing browsers' subscriptions are invalidated; users re-subscribe |
| `<dir>` unwritable | Run in-memory; WARN once | Subscriptions lost on restart |
| `--vapid-subject` invalid format | Fall back to `mailto:noreply@atterm.local`; WARN once | None visible |
| Unhandled `webpush.Open` error | `main.go` sets `WebPush = nil`; relay still starts | `/api/push/*` return 503; no pushes |

### HTTP endpoints

| Scenario | Handling |
|----------|----------|
| `WebPush == nil` | All four endpoints return 503 `{ "error": "web push disabled" }` |
| Missing / invalid token | 401 (existing auth middleware) |
| `POST /api/push/subscribe` body malformed JSON | 400 `{ "error": "invalid body" }` |
| `endpoint` is not https | 400 `{ "error": "endpoint must be https" }` |
| `p256dh` / `auth` missing or invalid length (must be base64url, decoded length within `[16, 128]`) | 400 `{ "error": "invalid keys" }` |
| Subscription cap (16/token) exceeded | Drop silently, return 200 (keeps client retries idempotent) |
| `POST /api/push/unsubscribe` endpoint not found | 200 (idempotent) |
| `POST /api/push/test` token has no subs | 200 `{ "sent": 0 }` |
| Persistence write fails | In-memory state already updated; log WARN; subsequent successful op will catch up |

**Rule**: in-memory state is authoritative at runtime. Persistence is best-effort.

### Frame path (uplink → relay)

| Scenario | Handling |
|----------|----------|
| Uplink not connected | `SendCommandEvent` returns silently; local notification already fired |
| Payload not valid JSON | uplink readLoop logs debug, does not disconnect |
| `frame.SessionID` not in this uplink's manifest | Log debug, drop (spoofing prevention or close-race) |
| `WebPush == nil` (startup degraded) | Log debug, drop |
| `label` > 256 chars | Truncate before passing to dispatch |
| `exit_code` negative or oversized | Pass through unchanged — semantics belong to the shell |

### Dispatch (relay → push service)

| Push service response | Handling | Subscription |
|-----------------------|----------|--------------|
| 200 / 201 | Success | Keep |
| 404 / 410 Gone | Browser-side subscription invalid | **Prune + persist** |
| 413 Payload Too Large | Should never happen (we send a small JSON); log WARN | Keep |
| 429 / 503 | Push service back-pressure | Log WARN, no retry, no backoff queue |
| Other 5xx / network error / timeout (10s) | Transient | Log WARN |
| `webpush.Send` panic | recover + log ERROR | Keep |

Dispatch never blocks the uplink reader. Each subscription's send runs in its own goroutine, but the *number* of goroutines is bounded by the subscription count (which is bounded by `tokens × 16`; in practice << 100).

### Session-resolver edges

| Scenario | Handling |
|----------|----------|
| Session's `remote_permission` was just narrowed | Resolver uses current snapshot; narrowed tokens drop from result; no push |
| Session closed between event emission and dispatch | Resolver may still find it in registry briefly; the late push is harmless |
| No token can view the session (e.g., owner published `remote_permission` that nobody satisfies) | Resolver returns empty; dispatch no-op |

### Browser / SW edges

| Scenario | Handling |
|----------|----------|
| `Notification.permission == "denied"` | UI shows "denied (change in browser settings)"; no API call |
| `Notification.permission == "default"` after dismiss | UI does not re-prompt |
| iOS Safari without PWA install | Show "Add to Home Screen" hint; no enable flow |
| iOS < 16.4 (no PushManager) | `pushSupported` returns false; button hidden |
| `pushManager.subscribe` throws | catch + console.warn + UI reset |
| `POST /api/push/subscribe` returns 503 | Inline hint "server has push disabled"; UI reset |
| SW push event with non-JSON payload | Show fallback `{ title: "AT Term", body: "Command finished." }` |
| SW process dormant when push arrives | Browser auto-wakes SW — out of our hands |
| OS-level Do Not Disturb | OS suppresses; we don't know and don't try to |

### Security

| Threat | Mitigation |
|--------|------------|
| Leaked token → attacker subscribes and sees command notifications | Accepted: same exposure as PTY output via attach. Command completion is a strict subset. |
| Token sub-flood (registering many endpoints) | 16 endpoints/token cap |
| Attacker-controlled endpoint URL | Not whitelisted by host (FCM/Mozilla/Apple hosts vary too much); push service authenticates the receiver, not us. Risk window is "attacker reads command summaries that arrive at their own endpoint" — same trust level as the token. |
| Same endpoint used across multiple tokens | Allowed (same browser, multiple tokens is legitimate) |
| VAPID private key leak | Attacker can forge pushes to endpoints they already know. Mitigation: rotate by deleting `<dir>/web-push.json`; all existing subs become invalid. |
| `TypeCommandEvent` frame spoofed by a malicious uplink | Manifest check: session_id must appear in the sender's ANNOUNCE. Cross-uplink spoofing impossible. |

### Platform notes

| Platform | Notes |
|----------|-------|
| Chrome / Edge / Brave | FCM endpoint; reference platform |
| Firefox | Mozilla autopush; works the same |
| Safari macOS | Apple push gateway; PWA install required |
| Safari iOS 16.4+ | PWA install required (must add to Home Screen); user grants permission inside the PWA |
| iOS < 16.4 | No PushManager — button hidden |
| Older browsers without SW | Button hidden |

## Testing

### Go unit tests

| File | Coverage |
|------|----------|
| `internal/webpush/vapid_test.go` | `generateVAPIDKeypair` returns valid P-256; private key derives public; `PublicKey()` round-trips through base64url |
| `internal/webpush/subscription_test.go` | Add new endpoint / overwrite same endpoint / Remove idempotent / cap at 16 / `byToken(tokenHash)` returns expected slice |
| `internal/webpush/persist_test.go` | Missing file → generate + persist / valid file → load / corrupt file → backup + regen / unwritable dir → in-memory + WARN |
| `internal/webpush/dispatch_test.go` | Calls sessionResolver; per-subscription goroutines; returns immediately; fake transport 410 → prune; fake transport 429 → keep + log; payload contains title/body/tag/data; tag == sessionID; label > 256 chars truncated |
| `internal/webpush/service_test.go` | `Open` → `AddSubscription` → simulated event → fake transport sees correctly-encoded push body; `SetSessionResolver` is used |
| `internal/webpush/transport_test.go` | Verifies endpoint URL, TTL header, Authorization JWT carries the configured subject |
| `internal/proto/codec_test.go` (extend) | `TypeCommandEvent` encode/decode round-trip; sessionID in header; payload fields; empty label allowed |
| `internal/relay/web_push_http_test.go` | `GET /api/push/key` read+write tokens both allowed, missing 401, `WebPush==nil` 503; `POST /api/push/subscribe` happy path, invalid JSON 400, http (not https) 400, missing keys 400, malformed key length 400, cap exceeded 200 with drop; `POST /api/push/unsubscribe` idempotent 200; `POST /api/push/test` happy path + no-subs 200/sent:0 |
| `internal/relay/uplink_conn_test.go` (extend) | Receives `TypeCommandEvent`: sid in manifest → calls Dispatch with right args; sid not in manifest → no Dispatch; invalid payload → connection stays |
| `internal/relay/server_test.go` (extend) | `WebPushSessionResolver(sid)` returns expected token-hashes matching token-scope ∩ owner-`remote_permission` |

### Web frontend tests

| File | Coverage |
|------|----------|
| `web/app-core.test.mjs` (extend) | `pushSupported` table: Chrome ✓ / Firefox ✓ / Safari iOS 16.4 PWA ✓ / Safari iOS non-PWA ✗ / no-PushManager ✗. `canEnablePush({permission})`: default ✓ / granted ✓ / denied ✗. `base64UrlToUint8Array` round-trip |
| `web/push-flow.test.mjs` (new) | `enablePushFlow` with injected fakes: all-success → one POST /api/push/subscribe + UI on; denied → no /api/push/key fetch; subscribe throws → UI reset + warn; POST returns 503 → "server has push disabled" |
| `web/sw.test.mjs` (new) | Load `sw.js` into vm with fake `self.registration.showNotification` and `event.data`: valid JSON → showNotification with title/body/tag/data; invalid JSON → fallback; missing data → fallback |
| `web/terminal-fit.test.mjs` (extend) | `sw.js` content changed → `CACHE` constant bumped |

### Desktop frontend tests

| File | Coverage |
|------|----------|
| `desktop/frontend/src/lib/api.test.ts` (extend or new) | `broadcastCommandFinished` wrapper signature matches `AppBindings.BroadcastCommandFinished` |
| `desktop/frontend/src/components/TerminalView.test.ts` (extend) | Source-level: imports `broadcastCommandFinished`; OSC handler calls both `showNotification` and `broadcastCommandFinished` when gate passes |

### Desktop Go tests

| File | Coverage |
|------|----------|
| `desktop/app_broadcast_test.go` (new) | `BroadcastCommandFinished` binding: nil uplink → silent; valid uplink → `SendCommandEvent` called; invalid uuid → silent |
| `desktop/uplink_test.go` (extend if exists, else new) | `SendCommandEvent` writes the correct frame to the out chan; pre-connect call → dropped |

### Manual smoke (pre-merge checklist)

1. New relay: `<dir>/web-push.json` auto-generated; `curl https://relay/api/push/key -H "Authorization: Bearer <token>"` returns a base64url string.
2. Chrome desktop: connect → click "🔔 Enable" → browser permission prompt → Allow → backend receives `/api/push/subscribe` once.
3. Chrome desktop: click "Test notification" → OS notification "AT Term test".
4. Chrome desktop: in atterm `sleep 12; ls`, blur AT Term + switch to another app → within seconds, Chrome OS notification fires with `Command finished · exit 0 · 12s`.
5. iOS Safari: visit relay → click "🔔 Enable" → hint "Add to Home Screen" appears → install PWA → reopen PWA → click Enable → iOS permission prompt → Allow.
6. iOS PWA: run the step-4 command on the linked desktop → notification appears on iPhone lock screen.
7. Firefox: repeat steps 2–4; Mozilla autopush path works.
8. Close the browser / PWA entirely: run the step-4 command → notification still arrives (the Web Push core value).
9. Click "🔔 OFF": `/api/push/unsubscribe` fires once; running the step-4 command → no notification.
10. Restart relay (preserving `<dir>/web-push.json`): existing subscriptions still receive pushes.
11. Two-token isolation: with a read token on one browser and a write token on another, both should receive pushes for sessions visible under their respective scopes.
12. Focus gate: keep AT Term focused, run the step-4 command → no local notification; relay receives no `TypeCommandEvent`; no Web Push.
13. Two browsers per token: Chrome + Firefox both subscribed under the same token → both notifications fire.

### Not tested

- Real FCM / Mozilla / Apple endpoints in CI (no outbound network) — fake transport only.
- VAPID JWT interop against each push service (upstream `webpush-go` covers this).
- iOS PWA installation flow (OS-level).
- Notification rendering style / vibration / sound.

## Limitations and known issues

- **Subscriptions are bound to a token.** If you rotate the relay's primary token, every browser that subscribed under the old token loses pushes until it re-subscribes. Acceptable trade-off — the alternative (per-device persistent identifiers independent of token) is much more state.
- **VAPID key wipe is irreversible.** Deleting `<dir>/web-push.json` regenerates the keypair; all existing browser subscriptions become un-usable (their `applicationServerKey` no longer matches). Acceptable for a self-hosted product where the user controls both endpoints.
- **No push for desktop-only users.** If you've never configured a remote relay (`relay_url` is empty), the uplink isn't connected and `BroadcastCommandFinished` drops on the floor. That's by design — there's no remote endpoint to push *to*.
- **Cast-attached panes don't broadcast.** A pane attached to another machine's session sets `isLocalSession=false` and `shouldNotifyCommand` short-circuits before either the local notification or the broadcast fires. Push for "remote sessions you're watching" would require the owner machine to broadcast — which it already does (locally) on its own.
- **Notification grouping is per-session.** `tag = sessionID` means consecutive completions for `make && make test` collapse into one lock-screen entry showing the latest. That's the right behavior for now; users who want per-command history can scroll the OS notification center.
- **No relay-side suppression for "user is actively watching".** If a phone has a tab open to the same session it subscribed for, both an in-page event (from the OUT stream) and a Web Push notification will fire on the device. The OS may de-duplicate visually via `tag`. A future iteration could query active subscribers per session, but YAGNI for MVP.

## Future work (extension points designed in but not implemented now)

- `CommandEventPayload` carries `exit_code` and `elapsed_ms` only. Adding `cwd`, `command`, or `host_label` is a payload-only change with no protocol break.
- `Subscription` JSON allows additional fields (the unmarshal is lenient) — e.g., browser-supplied label "John's iPhone" — without protocol changes.
- The same dispatch pipeline can later carry "BEL", "session crashed", "PTY orphaned" events by adding new `Type*` frames and `Dispatch*` methods. The fan-out and persistence machinery is shared.
- Per-subscription opt-out filters (e.g., "don't push commands matching `^cd `") can be added as a JSON field on `Subscription`. No protocol break.

## Implementation pointers

- `github.com/SherClockHolmes/webpush-go` provides `webpush.SendNotificationWithContext(ctx, payload, sub, opts)` with VAPID JWT signing and aes128gcm content encryption.
- `internal/relay/admin_config.go` is the precedent for write-temp-rename JSON persistence under `<RELAY_CONFIG_DIR>/`.
- `internal/relay/auth.go` exposes the token-hash helper used by the existing rate-limit + connection-limit logic. Reuse it for indexing subscriptions.
- `desktop/uplink.go` already serializes all outbound writes through a single goroutine + buffered chan — extending it is a one-method addition.
- `desktop/frontend/src/components/TerminalView.vue` line ~286 (the existing OSC 133 handler) is where the new `broadcastCommandFinished` call goes.
- `desktop/frontend/src/lib/commandFinish.ts` does not need to change — it already exposes `CommandEvent` with the right shape.
- `web/app.js` line ~159 (`registerServiceWorker`) is the natural place to also wire `enablePushFlow` once the SW is ready.
