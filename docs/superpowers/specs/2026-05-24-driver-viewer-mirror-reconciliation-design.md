# Driver/Viewer Mirror Reconciliation — Design (SP1)

Date: 2026-05-24
Status: Draft (pending implementation plan)

## Background

atterm shares one PTY across multiple clients via a driver/viewer model: exactly one subscriber is the **driver** (its IN/RESIZE reach the PTY); the rest are **viewers** locked to the PTY's reported size. A viewer sees a "remote has taken control / press space to take back" overlay and can reclaim by sending `CLAIM_DRIVER`. This was added in commit #2 (`driver/viewer mode for multi-client PTY sizing`).

When a session is shared **remotely**, the topology is two-layer:

```
desktop owner A ── local mini-relay (LOCAL session) ── uplink ──▶ central relay (MIRROR session) ── web / desktop B / mobile
```

`internal/session.Session` is the same type for both the LOCAL session (on the desktop's mini-relay) and the MIRROR session (on the central relay).

### The bug

A remote client attaching via the central relay never renders the viewer overlay, and the owner is never shown that control was taken. Confirmed for desktop→desktop, browser→desktop, and mobile→desktop.

Root cause: the MIRROR session has an **independent** driver concept decoupled from the real PTY driver.
- `Session.Subscribe` (session.go:~297) auto-promotes the **first subscriber** to driver. On the mirror, the first *remote* attacher becomes the mirror's driver, so its META carries `driver_client_id == its own clientID` → `isDriver = true` → no overlay.
- Commit #4 (`fix: preserve driver_client_id when broadcasting cwd/title META`) removed the mirror's raw `ms.sess.Broadcast(f)` passthrough and replaced it with `ms.sess.UpdateMeta(m)`, which rebroadcasts the **mirror's own** `driver_client_id` and **discards the upstream `m.DriverClientID`** (see uplink_conn.go:~314-320 comment). #4 correctly fixed a *solo local user* seeing a spurious overlay on every `cd` (that fix lives in `relay_host.go` / local `UpdateMeta` and is preserved), but it over-applied to the mirror broadcast path, hiding the upstream driver from remote clients.

So the upstream driver (`A`) is known at `uplink_conn.go` (`m.DriverClientID`) but thrown away. The fix re-introduces upstream-driver propagation **without** reintroducing the cwd-clobber bug.

Additionally, the **web client lacks the entire driver/viewer feature**: `web/src/shared/ws/client-conn.ts` sends ATTACH with only `session_id`+`since_seq` (no `client_id`), has no `claimDriver`, and `web/src/main/components/TerminalView.vue` has no `isDriver`/viewer overlay (only a replay overlay). The mobile client (`desktop/frontend/src/mobile/MobileTerminal.vue`) reuses the desktop `lib/connection.ts` (which *has* `client_id`+`claimDriver`+`onDriverChange`) but does not render a viewer overlay or wire a take-control affordance.

## Goals

- The MIRROR session reflects the **upstream PTY driver**: remote attachers render as viewers (overlay shown) when the desktop owner is driving.
- Remote take-control works end-to-end from all clients: a viewer's `CLAIM_DRIVER` → central relay mirror `Inbound()` → uplink → desktop `ClaimLocalDriver` → local driver changes → upstream META reflects it → propagates back to the mirror → all clients reconcile (the new driver hides its overlay; the previous driver, incl. owner A, shows it).
- **Unified take-control affordance**: a tappable "Take control" button in the viewer overlay on all three clients; desktop additionally keeps the bare-Space shortcut.
- Web client gains the full driver/viewer feature (client_id, claimDriver, onDriverChange, viewer overlay).
- Mobile client renders the viewer overlay + take-control button.
- Preserve commit #4's solo-user fix (no spurious overlay on `cd` when alone).

## Non-Goals

- **"N viewers attached" passive indicator** on the owner — that is SP2 (separate spec). It needs new viewer-count plumbing (mirror→uplink) and is independent of driver state.
- No protocol/wire changes: `META.driver_client_id`/`driver_client_name` and `CLAIM_DRIVER` (0x34) already exist. `client_id`/`client_name` are existing ATTACH/CLAIM fields. (AGENTS.md red-line 4.)
- No change to the resize/winsize coupling (AGENTS.md red-line 6); driver changes already drive `applyViewerSize` on the existing path.
- No multi-hop driver topology (single desktop↔central hop only). YAGNI.

## Constraints

- AGENTS.md red-line 4: no new frame types, no payload-shape changes. `client_id` is an existing additive field; web simply starts sending it.
- AGENTS.md red-line 5: `internal/` must not import `desktop/`. The relay change is self-contained in `internal/session` + `internal/relay`.
- AGENTS.md red-line 6: don't touch the predict-fork/skip-no-op-RESIZE coupling. Driver flips already call `applyViewerSize`; we add no new resize path.
- AGENTS.md red-line 10: web stays same-origin, no CDN.
- `internal/session.Session` is shared by local + mirror; the mirror behavior is gated behind an explicit flag so local sessions are unchanged.

## Architecture

The desktop's LOCAL session is the single source of truth for who drives the PTY. The MIRROR session becomes a **slave** of the upstream driver instead of having its own.

```
LOCAL session (desktop): first subscriber = driver (unchanged; #4 solo fix intact)
   │ broadcastDriverMeta(driver_client_id = A)  [on driver change / cwd / title]
   ▼ uplink forwards META upward (driver fields included; already happens)
MIRROR session (central relay, driverFromUpstream = true):
   • Subscribe does NOT auto-promote first subscriber
   • UpdateMeta adopts m.DriverClientID / m.DriverClientName from upstream, then rebroadcasts
   │ broadcast META(driver_client_id = A) to remote subscribers
   ▼
remote client (desktop B / web / mobile): driver_client_id ≠ self → isDriver = false → viewer overlay
   │ user taps "Take control" (or desktop Space) → CLAIM_DRIVER (client_id = self)
   ▼ mirror.Inbound() → uplink → desktop ClaimLocalDriver(self) → LOCAL driver = remote
   ▼ LOCAL broadcastDriverMeta(driver_client_id = remote) → up → mirror adopts → all clients reconcile
```

### Timing / snapshot correctness

When the uplink subscribes to the LOCAL session it receives a snapshot META carrying the current `driver_client_id` (A), which it forwards up; the mirror adopts it. A remote client attaching to the mirror then receives a snapshot META with A's id. There is a brief pre-first-META window where the mirror's `driverClientID` is empty; the client starts optimistic (`isDriver = true`) and corrects on the first META — identical to the existing same-relay behavior, so no spurious overlay flash beyond what already exists.

## Components

### `internal/session/session.go`
- Add field `driverFromUpstream bool` and setter `SetDriverFromUpstream(bool)`.
- `Subscribe`: when `driverFromUpstream` is true, **skip** the `if s.driverSubscriber == nil { promote }` block — a mirror never self-assigns a driver from its own subscribers. (Local sessions unchanged.)
- `UpdateMeta`: when `driverFromUpstream` is true, adopt `m.DriverClientID`/`m.DriverClientName` into `s.driverClientID`/`s.driverClientName` (treat a change in driver id as `changed = true`), then `broadcastDriverMeta` with the adopted values. When false, behavior is exactly as today (broadcast own driver fields; #4 preserved).
- `ClaimDriver` (already exists): unchanged — used on both layers; on the mirror a remote claim still sets the mirror's driver locally for immediate feedback, and the authoritative reconciliation arrives via the upstream META after `ClaimLocalDriver`.

### `internal/relay/uplink_conn.go`
- After `sess := session.New(id, info)` (mirror creation, ~line 178), call `sess.SetDriverFromUpstream(true)`.
- The existing `case proto.TypeMeta: ms.sess.UpdateMeta(m)` now propagates the upstream driver (because `UpdateMeta` adopts `m.DriverClientID` under the flag). Update the misleading comment block (~314-320) to describe the new behavior.

### `web/src/shared/ws/client-conn.ts`
- Generate `clientID = crypto.randomUUID()` and a `clientName` (hostname-ish / "web"); include `client_id`+`client_name` in the ATTACH payload.
- Add `claimDriver()` sending a `CLAIM_DRIVER` (0x34) frame with `{client_id, client_name}`.
- On META: parse `driver_client_id`/`driver_client_name`; track `currentDriverClientID`; fire a new `onDriverChange(driverID, isMe, driverName)` handler (fire only on change; `isMe = driverID !== "" && driverID === clientID`). Mirror the desktop `lib/connection.ts` logic.

### `web/src/main/components/TerminalView.vue`
- Add `isDriver` state (optimistic `true`, corrected by `onDriverChange`); set xterm `disableStdin = !isDriver` so viewer keystrokes don't reach the PTY.
- Render a viewer overlay ("remote has taken control" + a **Take control** button calling `conn.claimDriver()`) when `!isDriver`. Reuse the desktop overlay's copy/structure.
- Keep the existing replay overlay independent.

### `desktop/frontend/src/components/TerminalView.vue`
- Add a tappable **Take control** button to the existing `viewer-overlay` (calls `conn.claimDriver()`), alongside the existing bare-Space shortcut. No logic change to `isDriver`.

### `desktop/frontend/src/mobile/MobileTerminal.vue`
- Wire `onDriverChange` (from the reused `lib/connection.ts`) into an `isDriver` ref; set `term.options.disableStdin = !isDriver`.
- Render a viewer overlay over the terminal with a **Take control** button calling `conn.claimDriver()` when `!isDriver`.

## Data Flow (take-control round trip)

```
viewer taps "Take control"
  → conn.claimDriver()  [CLAIM_DRIVER, client_id = viewer]
  → central relay mirror session Inbound()  → uplinkOut
  → desktop uplink.go: ClaimLocalDriver(sessionID, client_id, client_name)
  → LOCAL session: driverSubscriber = uplink sub; driverClientID = viewer
  → LOCAL broadcastDriverMeta(driver_client_id = viewer)
       • owner A's TerminalView: onDriverChange(viewer, isMe=false) → isDriver=false → overlay shown
  → uplink forwards META up → mirror UpdateMeta adopts driver_client_id = viewer → broadcast
       • the claiming viewer: onDriverChange(viewer, isMe=true) → isDriver=true → overlay hidden
       • other viewers: still isDriver=false → overlay stays
```

## Error Handling

| Case | Behavior |
|---|---|
| Mirror receives upstream META before any client attaches | adopts driver; later attachers get correct snapshot |
| Client attaches before first upstream META | optimistic `isDriver=true`, corrected on first META (same as today) |
| Web `CLAIM_DRIVER` while WS not OPEN | no-op (guarded like desktop `claimDriver`) |
| Owner disconnects while a remote drives | unchanged from current relay cleanup; mirror driver reflects whatever upstream last reported |
| Empty `driver_client_id` from upstream (older desktop) | clients read "no driver" → all show overlay-as-viewer; acceptable degraded mode, matches pre-existing "empty allowed" semantics |

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `internal/session` | mirror (`driverFromUpstream=true`): Subscribe does NOT self-assign driver; UpdateMeta adopts upstream driver id and rebroadcasts; local session still self-assigns + #4 solo non-clobber still holds | Go unit tests (extend `session_test.go`) |
| `internal/relay` | end-to-end mirror: upstream META driver propagates to a mirror subscriber; a mirror subscriber's CLAIM_DRIVER reaches `Inbound()` for uplink forwarding | Go test with mirror session + fake subscribers |
| `web` connection | ATTACH includes `client_id`; `claimDriver()` emits CLAIM_DRIVER; META drives `onDriverChange` with correct `isMe` | Vitest (source/structure asserts mirroring desktop `connection.test.ts`) |
| `web` TerminalView | viewer overlay renders when `!isDriver`; Take-control button calls `claimDriver`; `disableStdin` toggles | Vitest + @vue/test-utils |
| `mobile` MobileTerminal | viewer overlay + Take-control button render and call `claimDriver`; `disableStdin` toggles on driver change | Vitest |
| desktop TerminalView | Take-control button present and calls `claimDriver` (Space path already tested) | Vitest |
| Manual (user env) | desktop→desktop + browser→desktop: viewer overlay appears on attach; take-control round trip flips overlays on both ends | two-client smoke |

## File Structure

**Modified (Go):** `internal/session/session.go` (+ `session_test.go`), `internal/relay/uplink_conn.go` (+ a relay-level test).
**Modified (web):** `web/src/shared/ws/client-conn.ts`, `web/src/main/components/TerminalView.vue` (+ vitest specs).
**Modified (desktop):** `desktop/frontend/src/components/TerminalView.vue` (Take-control button) and `desktop/frontend/src/mobile/MobileTerminal.vue` (viewer overlay + button) (+ vitest specs).
**Docs:** this spec; update `AGENTS.md` "何时改哪里" driver/viewer row if present, and note the mirror's upstream-driver behavior.

**NOT touched:** `internal/proto` (no frame change), the resize/winsize path, SP2 viewer-count plumbing.

## Risks & Open Questions

- **Reintroducing #4's bug.** Mitigated: the local session keeps `driverFromUpstream=false` (unchanged); only the mirror adopts upstream driver. #4's solo case is a local-session/`relay_host.go` concern and is untouched. The existing #4 regression test stays green.
- **Mirror's own `ClaimDriver` vs upstream authority.** A remote claim sets the mirror driver locally for instant feedback, but the authoritative value comes back via upstream META after `ClaimLocalDriver`. If the desktop rejects the claim (permission), the upstream META will re-assert the real driver, correcting the optimistic local flip. Acceptable.
- **Web client name.** Web has no OS hostname; use a stable label (e.g., `"web"` or a short random suffix). Cosmetic only (shown in "by &lt;name&gt;").
- **Permission interaction (red-line 11).** View-only remote permission must still block CLAIM_DRIVER — the relay already drops CLAIM_DRIVER for `view_only`/read-only scope (client_conn.go:~190-194). No change needed; the viewer overlay's Take-control button will simply have no effect for view-only users (acceptable; could hide it later).
