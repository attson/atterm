# Remote Viewer Count — Design (SP2)

Date: 2026-05-24
Status: Draft (pending implementation plan)

## Background

After SP1 (#76), a session shared via the central relay has a clean driver/viewer model. SP2 adds the **passive awareness** the user asked for: the desktop owner should see **how many remote clients are attached** to a session ("👁 N"), independent of who holds the driver role.

The challenge is structural: the owner's **local** session (on the desktop mini-relay) sees every remote client collapsed into a **single uplink subscriber** — it cannot count them. The count lives on the central relay's **mirror** session. So the mirror must propagate its subscriber count **down** the uplink to the desktop, which surfaces it in the UI.

## Goals

- The desktop owner sees a per-session count of attached remote clients, updating live as they attach/detach.
- Count = **all remote `/client` connections** on the mirror (web / mobile / other desktops). The driver is included (it's still a connected remote).
- Surfaced as a small **top-right badge** ("👁 N") on the owner's terminal, shown only when N > 0.
- Independent of SP1's driver/viewer overlay (orthogonal data + UI).

## Non-Goals

- No per-viewer identity/list (just a count). Names/avatars are a future extension.
- No web/mobile-side display of the count (this is owner-side awareness only).
- No change to SP1's driver/viewer overlay or take-control.
- No aggregate "total across all sessions" number — strictly per-session.

## Constraints

- AGENTS.md red-line 4 (wire compat): introduce a **new frame type on an unused `Type` byte** (`0x36`); do not change any existing frame's payload. Update `docs/spec/protocol.md`.
- AGENTS.md red-line 5: `internal/` must not import `desktop/`. The relay/session changes are self-contained; the desktop consumes the frame.
- `internal/session.Session` is shared local+mirror; the count hook is generic (any session can have one) but only the mirror (in `uplink_conn.go`) registers it to forward downstream.
- Lazy-uplink semantics (red-line 2) unchanged: the count frame is independent of STREAM_REQUEST/STOP; it rides the existing control connection.

## Architecture / Data Flow

```
remote /client attaches/detaches to the MIRROR session (central relay)
  → Session subscriber-count hook fires with len(subs)        [internal/session]
  → uplink_conn.go enqueues TypeViewers{session_id, count} on the downlink  [internal/relay]
  → desktop uplink.go: case TypeViewers → eventsEmit("relay:viewers", {session_id, count})  [desktop Go]
  → frontend listener updates a reactive Map<sessionId, count>
  → TerminalView renders a top-right "👁 N" badge when count > 0            [desktop frontend]
```

Note: the owner's own desktop attaches to its *local* mini-relay, not the central mirror, so it is **not** counted. The mirror's subscriber count is exactly the set of remote devices — which is the intended meaning of "watching."

## Components

### `internal/proto/frame.go`
- `TypeViewers Type = 0x36` — relay → uplink (downlink), reports the mirror's current remote subscriber count for a session.
- `type ViewersPayload struct { SessionID string `json:"session_id"`; Count int `json:"count"` }`.

### `internal/session/session.go`
- New field `onSubscriberCount func(int)` + setter `SetSubscriberCountHook(func(int))`.
- Fire it (async, lock released — mirrors the existing lifecycle-hook discipline) with the **new** `len(s.subs)`:
  - in `Subscribe` when a subscriber is actually added (`added == true`), and
  - in `removeSubscriber` when one was actually removed (`was == true`).
- Capture the count under `s.mu` and invoke the hook after `s.mu.Unlock()`.

### `internal/relay/uplink_conn.go`
- After creating a mirror via `newMirrorSession` (in the announce handler, where the `enqueue` closure is in scope), register:
  ```go
  sid := id
  sess.SetSubscriberCountHook(func(n int) {
      payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid.String(), Count: n})
      enqueue(proto.Frame{Type: proto.TypeViewers, SessionID: sid, Payload: payload})
  })
  ```
- `enqueue` is already non-blocking (select/default drop), so the hook never blocks a `/client` goroutine.

### `desktop/uplink.go`
- Add `case proto.TypeViewers:` in the inbound switch:
  ```go
  case proto.TypeViewers:
      var p proto.ViewersPayload
      if err := json.Unmarshal(f.Payload, &p); err != nil { continue }
      if u.eventsEmit != nil {
          u.eventsEmit(ctx, "relay:viewers", map[string]any{"session_id": p.SessionID, "count": p.Count})
      }
  ```

### `desktop/frontend`
- A central place that already listens to `relay:*` Wails events registers a `relay:viewers` listener and stores `Map<sessionId, count>` in shared reactive state.
- `TerminalView.vue` reads its session's count and renders a top-right badge (`👁 {{ count }}`) when `count > 0`. Positioned to not collide with SP1's viewer overlay (overlay is centered; badge is top-right corner).

## Error Handling

| Case | Behavior |
|---|---|
| Malformed `TypeViewers` payload | desktop `continue`s (ignored), like other inbound frames |
| `enqueue` downlink full | frame dropped (non-blocking); next change re-sends the current count — self-healing |
| Mirror torn down (uplink closes) | no more frames; frontend keeps last count until the session is removed by existing cleanup; on reconnect the mirror re-emits on next attach/detach |
| Count goes to 0 | `TypeViewers{count:0}` sent → badge hides |
| Frontend receives count for an unknown/closed session | stored in the map; harmless (no terminal renders it) |

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `internal/proto` | `ViewersPayload` JSON round-trip; `TypeViewers == 0x36` and unique | Go unit test |
| `internal/session` | count hook fires with correct `len(subs)` on subscribe and unsubscribe; not fired when add/remove is a no-op | Go unit test (fake hook capturing values) |
| `internal/relay` | a mirror session with the registered hook enqueues a `TypeViewers` frame carrying the new count on attach/detach | Go test wiring the hook to a capture channel |
| `desktop` | inbound `TypeViewers` → `eventsEmit("relay:viewers", {session_id, count})` | Go test with a fake `eventsEmit` (mirror the existing `relay:auth-info` test) |
| `desktop/frontend` | listener updates the map; `TerminalView` shows badge when count>0, hides at 0 | Vitest + @vue/test-utils |
| docs | `docs/spec/protocol.md` gains the `TypeViewers` (0x36) frame entry | doc edit |

## File Structure

**Modified (Go):** `internal/proto/frame.go` (+ test), `internal/session/session.go` (+ `session_test.go`), `internal/relay/uplink_conn.go` (+ test), `desktop/uplink.go` (+ `uplink_e2e_test.go` or a focused unit test).
**Modified (frontend):** the `relay:*` event-listener module + shared session state, `desktop/frontend/src/components/TerminalView.vue` (+ vitest).
**Docs:** `docs/spec/protocol.md` (new frame), `AGENTS.md` row if warranted.

**NOT touched:** SP1 driver/viewer code (orthogonal), web/mobile clients (owner-side feature), the resize/winsize path.

## Risks & Open Questions

- **Frontend event-listener location.** The exact module that registers `relay:auth-info`/`relay:auth-error` listeners is the integration point; the plan will pin it. Low risk (mechanism is established).
- **Badge vs overlay collision.** SP1's overlay is full-cover centered; the count badge is a small top-right chip. They can coexist; the plan positions the badge with a high z-index in the terminal corner.
- **Chattiness.** Viewer attach/detach is human-paced; no debounce needed (YAGNI). If a pathological reconnect storm ever emerges, coalescing can be added later.
- **Count includes the driver.** Per the decision, "all remote connections" — a remote that took control still counts as connected. Acceptable and simplest; matches "N devices attached."
