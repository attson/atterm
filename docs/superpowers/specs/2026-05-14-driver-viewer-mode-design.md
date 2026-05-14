# Driver/Viewer Mode Design

**Date:** 2026-05-14
**Status:** Draft

## Goal

Eliminate cross-client PTY size conflicts that corrupt the local xterm when a remote subscriber attaches to a session. Introduce a single-driver model where exactly one subscriber drives the PTY (sends IN/RESIZE) at any moment; all others are viewers whose xterm dimensions are locked to the PTY's current size. The driver role transfers on demand via a spacebar press from a viewer, without owner confirmation.

## Motivation

Today every subscriber freely sends `RESIZE` frames matching its own xterm dimensions, and the relay forwards each one to the PTY. When two subscribers with different fitted sizes are simultaneously attached, the PTY oscillates or settles on one size while the other xterm still believes a different size — claude code (and any TUI) renders cursor-positioned bytes for the PTY's size, and the off-size xterm renders that stream into a viewport that doesn't match, producing the overlapping-text artifacts the user reports.

Captured smoking gun in 2026-05-14 diag session: local xterm at 133×39 → remote attaches at 137×41 → remote's xterm sends `RESIZE 137×41` → PTY resized → claude redraws for 137×41 → local xterm (still 133×39) receives those bytes → visible corruption. Remote view is fine because remote xterm and PTY now agree.

Within the existing red-line constraint (AGENTS.md #6 — `predict-fork + queue-not-drop + skip-on-match` are interlocked and must not be touched in isolation), the cleanest fix is to gate which subscriber's RESIZE reaches the PTY, rather than touch the three local-spawn primitives. This spec adds a transient runtime role (driver/viewer) layered on top of the existing `remote_permission` policy.

## Non-Goals

- Multi-remote arbitration at the public relay. All public-relay subscribers behind one uplink are treated as one collective "remote" for driver purposes. Multiple simultaneous mobile attachers sharing the remote-driver role is acceptable v1 chaos (rare in practice; user usually drives from one device at a time).
- Per-frame origin tagging on IN/RESIZE. We trust cooperative clients to stop sending IN/RESIZE in viewer mode.
- Reclaim driver on reconnect. If the current driver's subscriber disconnects, the role is dropped; whoever re-claims first becomes driver.
- Configurable takeover gesture. Space is hardcoded for MVP.
- Visual animation/gradient for transitions. A static badge + 2-second banner is enough.
- Server-side terminal virtualization (mosh/xpra approach). PTY remains a single physical size.

## Behavior

### Driver assignment

1. Each subscriber generates a `client_id` (UUID v4) when it opens a `SessionConnection`. The ID lives for the lifetime of that connection (it persists across the connection's internal reconnect cycles, but a new `SessionConnection` instance gets a fresh ID).
2. The client sends `client_id` in the `ATTACH` payload.
3. The session's first-ever subscriber becomes driver automatically. The session records both `driverSubscriber` (`*Subscriber` pointer, for runtime gating) and `driverClientID` (string, for broadcast).
4. When a viewer sends `CLAIM_DRIVER`, the relay sets `driverSubscriber = senderSub`, `driverClientID = payload.client_id`, then broadcasts a `META` frame with the new `driver_client_id`.
5. When the driver subscriber unsubscribes (disconnect, lag-drop, session close), the relay clears both fields and broadcasts `META` with empty `driver_client_id`.

### Driver semantics

A subscriber is "driver" if its `*Subscriber` pointer equals `session.driverSubscriber`. The client is "driver" if its locally-stored `client_id` equals the latest `meta.driver_client_id` it has received.

**Server enforcement (relay):**
- `IN`, `RESIZE`, `PASTE_IMAGE` frames from a non-driver subscriber are dropped (logged at debug level).
- `CLAIM_DRIVER` is always accepted, regardless of current driver, subject only to existing `remote_permission` (see below).
- All read-only flow (`OUT`, `META`, `CLOSE`, `REPLAY_PROGRESS`) continues to fan out to every subscriber.

**Client UI (driver):**
- `FitAddon` runs normally; on container resize, xterm refits and sends `RESIZE`.
- Keystrokes go to xterm and forward to PTY as `IN` frames (unchanged).
- No visual indication of "driver mode" — that's the default expected state.

**Client UI (viewer):**
- `FitAddon` is **not** used to size the terminal. Instead, `term.resize(meta.cols, meta.rows)` is called whenever `META` arrives with new dims. The xterm element renders at its natural cell size; if the container is larger it shows surrounding padding (background color); if the container is smaller the overflow is clipped (`overflow: hidden`). No horizontal/vertical scroll for MVP.
- `term.options.disableStdin = true` so xterm itself ignores keystrokes (defense in depth — the client also stops sending `IN`).
- A small badge in the bottom-left corner reads `viewer · press space to take over` in the same muted style as the existing `connecting…` overlay.
- Pressing `Space` (without modifiers) sends `CLAIM_DRIVER` instead of forwarding to xterm. Captured at the same keydown handler used today for the copy shortcut (`capture: true`).
- Other keys are swallowed in viewer mode (don't reach xterm, don't get sent as `IN`).

**Transition feedback:**
- When `meta.driver_client_id` changes such that this client's role flips, a 2-second banner appears: `you are now the driver` or `you are now a viewer`. Reuses the same toast slot the terminal already has.

### Permission interaction

`remote_permission` (view / control / full) layers on top:

| Permission | Can attach? | Can CLAIM_DRIVER? | Notes |
|---|---|---|---|
| `view` | yes | **no** | Always viewer. Badge hides the "press space" hint and shows just `viewer`. Server drops `CLAIM_DRIVER` from view-permission subs. |
| `control` | yes | yes | Can take over. Same as `full` for driver purposes. |
| `full` | yes | yes | Default. |

The owner (local loopback subscriber) is unaffected by `remote_permission` since it's not subject to remote policy.

### Edge cases

1. **Session created, no subscribers yet** — `driverSubscriber = nil`, `driverClientID = ""`. The PTY exists with its initial fork dims. First subscriber to attach becomes driver and may then change PTY size via RESIZE.

2. **Driver disconnects** — driver fields cleared, META broadcast. Remaining viewers see `driver_client_id = ""` → all in viewer mode with no driver. PTY size frozen at last driver's size. Any remaining viewer can press space to claim.

3. **Race on CLAIM_DRIVER** — relay serializes claims under the session lock. First received wins; later claims overwrite (last-write-wins among contemporaneous claims).

4. **Owner reconnects after disconnect** — `SessionConnection` instance is destroyed and re-created (Vue component re-mount or app restart), so the new connection gets a fresh `client_id`. It is treated like any other attacher: if no current driver, it auto-claims; otherwise it joins as viewer.

5. **Local container smaller than PTY in viewer mode** — content clips at the right/bottom. User can resize the host window to see the rest, or press space to take over (which fits PTY to the new container).

## Architecture

### Protocol additions

`internal/proto/frame.go`:

```go
// CLAIM_DRIVER: a viewer requests to become the session driver. Payload
// is the requester's end-to-end client_id so the relay can echo it in
// the resulting META.driver_client_id.
TypeClaimDriver Type = 0x34

type ClaimDriverPayload struct {
    ClientID string `json:"client_id"`
}

// AttachPayload gains client_id (optional for backwards compat; if
// missing the relay assigns an internal UUID and the client can never
// recognize itself as driver — equivalent to always-viewer mode for
// pre-driver-mode clients).
type AttachPayload struct {
    SessionID string `json:"session_id"`
    SinceSeq  uint64 `json:"since_seq,omitempty"`
    ClientID  string `json:"client_id,omitempty"`
}

// MetaPayload gains driver_client_id. Empty = no driver currently.
type MetaPayload struct {
    Cwd            string `json:"cwd,omitempty"`
    Title          string `json:"title,omitempty"`
    DriverClientID string `json:"driver_client_id,omitempty"`
}
```

### Components touched

**Backend (Go):**

- `internal/proto/frame.go` — new frame type, payload struct, extend `AttachPayload` and `MetaPayload`.
- `internal/proto/codec.go` — no changes (existing `Marshal`/`Unmarshal` is type-agnostic).
- `internal/session/session.go`:
  - Add `driverSubscriber *Subscriber` and `driverClientID string` fields to `Session`.
  - Extend `Subscriber` struct with `clientID string` (end-to-end ID).
  - In `Subscribe`, auto-promote when `len(s.subs) == 0` before adding (the new sub becomes driver). Accept `clientID` parameter.
  - New method `ClaimDriver(sub *Subscriber, clientID string)`: sets fields, broadcasts META.
  - On `removeSubscriber`, if the removed sub was driver, clear fields and broadcast META.
  - Add `IsDriver(sub *Subscriber) bool` helper.
- `internal/relay/client_conn.go`:
  - Parse `client_id` from ATTACH payload; pass to `sess.Subscribe(sinceSeq, clientID)`.
  - On `TypeClaimDriver`: validate permission, then call `sess.ClaimDriver(sub, payload.ClientID)`.
  - In the `IN/RESIZE/PASTE_IMAGE` switch arm, add an `if !sess.IsDriver(sub)` check before `SendInbound`. Drop with `s.debugf` log if non-driver.
- `desktop/uplink.go`:
  - Forward `TypeClaimDriver` in both directions (uplink → public-relay-via-CLAIM, and from public relay back through inbound → SendLocalInbound).
  - `desktop/relay_host.go::SubscribeLocal` — pass the uplink's own `clientID` (a freshly-generated UUID for this uplink subscription).
- `docs/spec/protocol.md` — document the new frame type, ATTACH and META payload additions, the driver state machine.

**Frontend (TS/Vue):**

- `desktop/frontend/src/lib/proto.ts` — add `CLAIM_DRIVER: 0x34` to `TYPE`.
- `desktop/frontend/src/lib/connection.ts`:
  - `SessionConnection` constructor generates and stores `clientID = crypto.randomUUID()`.
  - ATTACH payload includes `client_id`.
  - On `META`, parse `driver_client_id`; if it changed, fire a new `onDriverChange?: (driverClientID: string, isMe: boolean) => void` handler.
  - New method `claimDriver()`: sends `CLAIM_DRIVER` frame with `{ client_id }` payload.
- `desktop/frontend/src/components/TerminalView.vue`:
  - Track `isDriver` reactive ref, updated from `onDriverChange`.
  - When `isDriver` flips to `false`: stop the `ResizeObserver`-driven `safeFit`. On every `onMeta` received, call `term.resize(meta.cols, meta.rows)` if dims differ.
  - When `isDriver` flips to `true`: restart `safeFit` (which then sends a RESIZE matching the container).
  - Set `term.options.disableStdin = !isDriver` reactively.
  - New keydown handler at capture phase: in viewer mode, intercept `Space` (no modifiers), call `conn.claimDriver()`, preventDefault. Other keys in viewer mode: preventDefault + stopPropagation (don't reach xterm; don't send IN — xterm.onData already won't fire because disableStdin is true, but defense in depth).
  - Badge UI: a small `<div class="viewer-badge">viewer · press space to take over</div>` when `!isDriver`. Reuse the overlay styling.
  - Toast on role change.

### Error handling

- Malformed `CLAIM_DRIVER` payload (bad JSON): drop frame, log at debug level.
- `CLAIM_DRIVER` from a `view`-permission subscriber: drop, log.
- Empty `client_id` in `CLAIM_DRIVER`: relay records as empty string. Other clients see `driver_client_id = ""` and behave as if there's no driver (all viewers, no badge match). UI-wise this looks like "no one is driving" which is mostly harmless. We don't reject — clients with old code that don't send client_id can still try to claim; they'll appear as "driver = anonymous" to themselves (no match) but at least PTY enforcement on the relay side works (driverSubscriber points to them, gates IN/RESIZE correctly).

## Testing

- Unit tests on `internal/session/session_test.go`:
  - First subscriber auto-becomes driver.
  - `ClaimDriver` flips driverSubscriber and triggers META broadcast.
  - Non-driver IN/RESIZE dropped (via `IsDriver` check called by caller).
  - Driver unsubscribe clears state and broadcasts META.
- Unit tests on `internal/relay/client_conn_test.go` (or a new file):
  - ATTACH with client_id round-trips.
  - CLAIM_DRIVER from view-permission subscriber is dropped.
  - Non-driver subscriber's IN/RESIZE doesn't reach inbound queue.
- Frontend tests in `connection.test.ts` (new) and extending `TerminalView.test.ts`:
  - SessionConnection generates a client_id and sends it in ATTACH.
  - On META with driver_client_id == self.clientID, isDriver flips true.
  - On viewer mode + space keydown, `claimDriver()` is called and event is preventDefault'd.
- Manual test plan (added to plan doc): owner local + one remote attacher, verify size correctness on both, takeover via space both directions, owner reconnect.

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Misidentifying driver on protocol upgrade — old clients without `client_id` can't recognize themselves and behave as permanent viewers, but they can still send IN/RESIZE which the relay now drops because old clients aren't tagged. Result: cross-version clients become completely unresponsive. | High | Spec change: if `AttachPayload.ClientID == ""`, relay assigns a fresh internal UUID and stores it. The client's view of `driver_client_id` will never match its (missing) local ID so it always renders as viewer. But because the relay's `driverSubscriber` points to it correctly, its IN/RESIZE are still forwarded. Net effect: old clients work but always feel like "viewer" in UI (no UI changes anyway since they don't render the badge). Tested by integration test forcing empty client_id. |
| FitAddon disable/enable race with `term.resize` — toggling between driver and viewer might leave xterm in an inconsistent state if a fit and an external resize land in the same animation frame. | Medium | Always cancel pending `safeFit` rAF before calling `term.resize(meta.cols, meta.rows)`. The existing `safeFit` is already guarded by `getBoundingClientRect()` checks. Add a small flush helper. |
| Container smaller than PTY size in viewer mode looks broken (content clipped). | Low | Acceptable for MVP — it's clearly "you're peeking at a bigger session". Future polish: CSS scale to fit, or scrollable wrapper. |
| Space-key collision when user is mid-selection (intending to extend selection, not claim) — xterm's selection mode might intercept space differently. | Low | Capture-phase keydown handler runs before xterm. Selection is a different gesture (mouse). Tested manually. |
| The diag log added on `diag/altscreen-repaint-trace` is now stashed; future sessions investigating similar issues won't have it. | Low | Optionally land that 10-line log as a separate small change on main first. Independent of this spec. |

## Open questions

None — proceed to plan.
