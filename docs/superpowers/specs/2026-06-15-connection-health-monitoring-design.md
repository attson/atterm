# Connection Health Monitoring

Date: 2026-06-15
Scope: desktop (Wails) uplink + web/PWA + mobile (Capacitor) — all three clients
       monitor their own WS connection to the relay. Relay-side change is
       limited to echoing PING payloads back as PONG.

## Goal

Continuously monitor each client's WebSocket link to the relay and surface its
quality (RTT, reconnect history, byte throughput, sequence gaps) in a small
status pill plus an on-demand drawer. The user should be able to glance at the
pill and know whether the link is healthy; if not, the drawer should answer
"why" without forcing them into terminal-level debugging.

Out of scope (this release):

- Active end-to-end probes between two clients (driver ↔ viewer RTT).
- HTTP-side probing of `/healthz` from clients (WS-only this round).
- System notifications / OS-level alerts — color and pill text only.
- Persisting metric history across app restarts.

## What changes

### Protocol (binary frames)

`TypePing (0x20)` and `TypePong (0x21)` already exist in `internal/proto/frame.go`
but neither is currently sent or interpreted (WS control-frame pings handle
keepalive). This spec gives them application-level semantics:

| Frame | Sender | Payload | Receiver behavior |
|-------|--------|---------|-------------------|
| `TypePing (0x20)` | any client (uplink, viewer, list-conn) | `[8B BE u64 monotonic ms]` (optional; 0-length still valid) | Relay echoes the exact payload back as `TypePong`. |
| `TypePong (0x21)` | relay | echo of received PING payload | Sender computes `RTT = now() - decoded(ts)` if payload length == 8, otherwise treats the pong as connection-liveness only. |

Backward compatibility:

- Old client + new relay: client sends empty PING, relay echoes empty PONG, no
  RTT sample is recorded — the client's `ConnHealth` falls back to
  state/reconnect-count indicators only.
- New client + old relay: client sends 8-byte PING, old relay either echoes
  empty (current handlers send empty) or ignores. Client detects PONG payload
  length != 8 and skips the sample. No errors, no fatal logs.

No new frame types. No version negotiation. `internal/proto/codec.go` already
encodes arbitrary payloads on all frame types; codec tests just need a case for
"PING with 8-byte payload survives a round trip".

### Relay echo path

Two relay conns need a one-line change:

- `internal/relay/uplink_conn.go` — uplink reader: on `TypePing`, write back a
  `TypePong` with the received payload (`f.Payload`) instead of dropping it.
- `internal/relay/client_conn.go` — viewer/control reader: same.

`internal/relay/client_sessions_conn.go` is **not** modified — it is
unidirectional (relay → client only; no reader loop). Mobile / desktop views
that show the session list before opening a specific session will see the pill
in `connection-state-only` mode (no RTT band). RTT becomes available once the
user enters a session and the `/client` WebSocket is open.

`internal/relay/agent_conn.go` is also **not** modified — `agent_conn`
represents the legacy "agent" role that no current client uses.

Writes go through the same write channel each conn already owns; no new mutex,
no new goroutine. The write is at most ~32 bytes (header + 16 byte UUID +
8 byte payload) and happens at most once per ping interval per conn, so
back-pressure on the existing send channel is not a concern.

### Client ConnHealth library

Two parallel implementations sharing the same shape:

- Go side, used by desktop uplink: `internal/connhealth/connhealth.go`
- TypeScript side, used by web/PWA + mobile + desktop frontend status pill:
  - `web/src/shared/connhealth/connhealth.ts` (canonical)
  - re-exported by `desktop/frontend/src/lib/connHealth.ts` via shared imports

Each instance owns one logical WS link and exposes:

```ts
interface ConnHealth {
  state: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  rtt: { last_ms: number | null; p50_ms: number | null; p95_ms: number | null };
  rtt_samples: Array<{ at_ms: number; rtt_ms: number }>;  // ring, max 60
  reconnect: {
    count_last_hour: number;
    last_at_ms: number | null;
    last_reason: string;
    history: Array<{ at_ms: number; reason: string; duration_ms: number }>;  // last 5
  };
  bytes: { in_per_sec: number; out_per_sec: number };  // 1-min EMA
  seq_gaps: number;  // monotonic count of observed OUT-frame seq jumps
  emit: 'health-changed' event each time state or band changes;
}
```

Sampling and aggregation:

- **RTT**: timer fires every 5 s while `state === 'connected'`. On send, encode
  `now_ms = performance.now()` (TS) or `time.Since(startMono).Milliseconds()`
  (Go) into 8 BE bytes and write a `TypePing`. On matching `TypePong`,
  `rtt_ms = decode_now() - decoded_ts`. Ring buffer of last 60 samples
  (5 minutes). p50/p95 recomputed lazily on read.
- **State**: driven by WS lifecycle hooks (open/close/error). When the existing
  reconnect logic in `uplink.go` / `useRemoteSession.ts` enters its backoff
  loop, ConnHealth bumps `reconnect.count_last_hour` and records reason
  (mapped from WS close code or `Error.message`).
- **Bytes**: WS message handler increments a per-second bucket (current second
  index = floor(now/1000)). A 1 Hz timer rolls buckets into a 60-bucket ring
  and recomputes EMA (α = 0.2).
- **Seq gaps**: `TypeOut` frames already carry an 8-byte BE seq prefix. The
  client side already tracks `last_seq` for snapshot/replay logic; ConnHealth
  reads from the same source and counts increments > 1.

Bounds:

- Memory: 60 × (number + number) + 5 × small struct ≈ 2 KB per instance.
- Wire overhead per session: 1 PING + 1 PONG every 5 s ≈ 32 B/s round trip per
  client. With three viewers + one uplink per relay, that's ~128 B/s, well
  under any throughput threshold.

### Where the library wires in

Desktop uplink (Go):

- `desktop/uplink.go` `runOnce`: create a `connhealth.Tracker`, hand it to the
  reader (records RTT on `TypePong`, byte-in on each frame, seq gap on
  `TypeOut`) and to the writer pump (byte-out, periodic PING). Expose
  `(u *uplink) Health() connhealth.Snapshot` so the frontend can poll via Wails
  RPC.
- Add Wails method `(a *App) GetUplinkHealth() ConnHealthDTO` returning the
  snapshot serialized to the same shape the TS interface uses.

Desktop frontend:

- New composable `useUplinkHealth()` calls `GetUplinkHealth` on a 1 s rAF-aligned
  poll while the drawer is open, and on a 5 s poll while only the pill is
  visible (cheap, just to drive band color).
- `ConnHealthPill.vue` lives in `desktop/frontend/src/components/`, rendered
  inside `TitleBar.vue` to the right of the existing identity status.
- `ConnHealthDrawer.vue` opens as a slide-in panel from the right; sparkline is
  a 60-point inline SVG (no chart lib — matches existing
  `TitleBar.vue` philosophy).

Web/PWA + mobile:

- `web/src/shared/ws/client-conn.ts` (the existing `SessionConnection`)
  gains a `ConnHealth` instance, exposed via a `getHealth()` accessor.
  PING send + PONG handle live inside the existing message loop.
- Mobile (Capacitor) reuses the same `client-conn.ts` because the mobile
  build is just `web/` bundled into a `WKWebView`.
- Pill and drawer components live in `web/src/shared/components/` and are
  imported by `web/src/main/App.vue` (browser/PWA) and the mobile shell.
- Desktop frontend imports the same shared components via the existing
  `@shared/` alias.

### UI: pill + drawer

Pill states (left-to-right text):

| State | Text | Color |
|-------|------|-------|
| `state=connected`, RTT 0–150 ms | `●  120 ms` | green (`--good`) |
| `state=connected`, RTT 150–500 ms | `●  340 ms` | yellow (`--warn`) |
| `state=connected`, RTT > 500 ms or null after a sample window | `●  820 ms` | red (`--bad`) |
| `state=connecting` (first connect) | `…  connecting` | dim |
| `state=reconnecting` | `↻  reconnecting` | yellow, gentle 1 Hz pulse |
| `state=closed` (manual pause / no relay configured) | `●  off` | dim |

Click pill → drawer opens. Drawer contents (top to bottom):

1. Current RTT (large), p50 / p95 over last 5 min, and a 60-point sparkline.
2. Up / down byte rate, current (KB/s) and 1-min EMA.
3. Connection state row: state, time since last state change, reconnect
   count over last hour.
4. Recent reconnects table (up to 5): `time · reason · downtime`. Reason is
   pulled from the WS close-code map already used by `handleCloseError`.
5. Seq-gap counter (one line; informational; usually 0).
6. Footer link "Open Diagnostics" → existing Settings → Diagnostics tab.

The pill is hidden when no relay is configured (`url == ''`) — there is no link
to monitor.

### Settings switch

Add a checkbox under Settings → General: **"Connection health monitoring"**,
default **on**. Off disables PING emission and freezes the pill at the
connection-state band (no RTT shown). The byte / seq counters keep running
(they are passive observers).

## Architecture

```
[ConnHealthPill] ──click──> [ConnHealthDrawer]
       │                            │
       └── reads ──> useConnHealth()
                      │
                      ├── (desktop) Wails RPC  GetUplinkHealth()
                      │              │
                      │              ▼
                      │       internal/connhealth.Tracker (Go)
                      │              │
                      │              ▼
                      │       desktop/uplink.go reader/writer
                      │              │
                      │              ▼
                      │       WebSocket ─── PING/PONG ──> relay
                      │
                      └── (web/mobile) Pinia ref ──> WS client (TS)
                                                       │
                                                       ▼
                                              WebSocket ─── PING/PONG ──> relay
```

Three relay conns:

```
TypePing (8B ts) ────in────> [uplink_conn|client_conn|client_sessions_conn|agent_conn]
                              │
                              └── echo payload as TypePong ──out──> same WS
```

## Error handling

- **PONG payload != 8 B**: drop the sample silently, keep the conn alive. This
  is the old-relay compat path.
- **PING fails to enqueue (send channel full)**: skip this round, do not crash;
  next 5 s timer fires normally. The pill will eventually drop into the
  reconnecting state if writes are persistently blocked because the existing
  WS write timeout will fire first.
- **RTT samples missing** (e.g. monitoring disabled, or pong never received):
  pill shows the connection-state band only; drawer shows "no samples yet".
- **Clock skew**: RTT is computed by the sender using its own clock for both
  ends of the round trip; relay clock is never read. Immune to skew.

## Testing

Go:

- `internal/proto/codec_test.go`: add `TestPingPongPayloadRoundTrip` covering
  8-byte ts payload.
- `internal/relay/uplink_conn_test.go`, `client_conn_test.go`,
  `client_sessions_conn_test.go`, `agent_conn_test.go`: send a `TypePing`
  with a known 8-byte payload, expect the matching `TypePong` to come back
  with the same payload.
- `internal/connhealth/connhealth_test.go`: ring buffer eviction, EMA
  convergence, state transitions, p95 computation.
- `desktop/app_conn_health_test.go`: `GetUplinkHealth` returns a snapshot
  shaped exactly like the TS DTO.

TypeScript:

- `web/src/shared/connhealth/connhealth.test.ts`: same semantics as the Go
  test — ring buffer, EMA, state machine.
- `desktop/frontend/src/components/__tests__/ConnHealthPill.test.ts`: band
  classification (green / yellow / red / reconnecting).
- `web/src/shared/components/__tests__/ConnHealthDrawer.test.ts`: renders
  current RTT, sparkline path, reconnect table.

Manual smoke:

- `wails dev` + `go run ./cmd/atterm-relay --dev-insecure` on localhost, verify
  pill shows ~5–20 ms green.
- `tc qdisc add dev lo root netem delay 300ms` (Linux) or Network Link
  Conditioner (macOS) → pill should turn yellow within ~10 s.
- Kill relay, watch pill switch to `reconnecting`; restart relay, count climbs
  by one.

## Risks and mitigations

- **Risk**: bumping PING semantics in a published protocol could break
  third-party relays. **Mitigation**: there are none — atterm relay is the only
  implementation. Doc the change in `docs/spec/protocol.md`.
- **Risk**: 5 s timer for three clients per session adds 0.6 Hz of frame churn
  on relay. **Mitigation**: at 32 B per round trip this is ~20 B/s per
  connection, far below the limiter's per-IP rate.
- **Risk**: RTT could pollute the diagnostics tab if the user toggles
  monitoring off mid-session. **Mitigation**: snapshot is read at drawer-open
  time; switching off only freezes future samples, the visible numbers stay
  truthful.

## Files touched (preview)

```
internal/proto/codec_test.go                    (+ ping/pong payload test)
internal/connhealth/connhealth.go               (new)
internal/connhealth/connhealth_test.go          (new)
internal/relay/uplink_conn.go                   (echo)
internal/relay/client_conn.go                   (echo)
desktop/app.go                                  (GetUplinkHealth)
desktop/app_conn_health_test.go                 (new)
desktop/uplink.go                               (wire Tracker, send PING, echo seq)
desktop/frontend/src/lib/api.ts                 (GetUplinkHealth binding)
desktop/frontend/src/lib/connHealth.ts          (re-export shared lib)
desktop/frontend/src/components/ConnHealthPill.vue        (new)
desktop/frontend/src/components/ConnHealthDrawer.vue      (new)
desktop/frontend/src/components/TitleBar.vue              (mount pill)
desktop/frontend/src/components/SettingsGeneral.vue       (toggle)
desktop/frontend/src/composables/useUplinkHealth.ts       (new)
desktop/frontend/src/i18n/messages/{en,zh-CN}.ts          (strings)
web/src/shared/connhealth/connhealth.ts                   (new)
web/src/shared/connhealth/connhealth.test.ts              (new)
web/src/shared/ws/client-conn.ts                          (wire Tracker, send PING)
web/src/shared/components/ConnHealthPill.vue              (new)
web/src/shared/components/ConnHealthDrawer.vue            (new)
web/src/main/App.vue                                      (mount pill in header)
docs/spec/protocol.md                                     (document PING payload)
```
