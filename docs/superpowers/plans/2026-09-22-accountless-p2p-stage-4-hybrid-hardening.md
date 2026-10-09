# Accountless P2P Stage 4 - Hybrid Routing and Hardening

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 3](./2026-09-22-accountless-p2p-stage-3-rendezvous.md)
> Milestones: P8-P9
> Release: v0.9.0 and later capability releases
> Outcome: 统一 Relay direct、Rendezvous direct、Quick Tunnel 和 Relay fallback，并逐项开放高级远程能力。

## Entry Gate

- Relay P2P acceleration、Quick Tunnel accountless mode、Rendezvous each pass independent soak tests。
- All paths expose the same `BinaryFrameTransport` and authenticated principal abstraction。
- Route metrics and OUT seq cursors are reliable enough for automated handover。

## PR 4.1 - Unified route manager

Maintain candidates per `session_id`, never per `host_id`:

```text
Direct (Relay or Rendezvous signaling)
  > enabled Quick Tunnel WSS
  > enabled Relay WS
```

- Relay account sessions and Peer Space sessions remain separate trust principals even when they reference the same session id。
- Exactly one active subscriber/driver route lease。
- Candidate health does not create duplicate sidebar items。
- Route selection respects user policy; no account login prompt in Peer-only mode。

Implementation status: the frontend now has a pure candidate model keyed by
`(principal, session_id)` with deterministic `direct > Quick Tunnel WSS > Relay WS`
selection, route/principal compatibility checks, and an explicit Peer-only policy that cannot
select Relay-account candidates. The sidebar merge uses this model for the currently available
Relay catalog and Rendezvous Peer catalog, preserving one visible `session_id` with the established
Relay-entry precedence while keeping the two trust groups separate before UI collapse. Quick Tunnel
WSS is now exposed through that same native Peer transport contract as an explicit
`route=quick_tunnel` choice. Go retains the already verified first-join hint only in process memory,
keys it by the authenticated issuer Peer ID, rechecks bundle expiry and current deny-wins membership,
and resolves every endpoint and membership token outside the renderer. The WSS client reuses the
existing encrypted terminal/config record channel and reports `Quick Tunnel` rather than `Direct`.
The default request remains Rendezvous direct, and no Relay credential or `account_key` enters the Peer
path. Existing members can manually import a signed,
ticketless reconnect bundle. Import requires the current genesis and exact active issuer membership,
then atomically replaces (or removes) only the process-local Quick Tunnel hint. The encrypted Rendezvous
catalog also probes an optional v2 response that
carries the host's refreshed signed member bundle on its first page. Old hosts ignore the probe and are
cached as v1-only after a short timeout; new clients independently bind the bundle issuer to the catalog
Peer and current deny-wins membership before rotating only that process-local hint. This distribution
creates no terminal subscriber or Pion attempt and a v1 fallback leaves the current hint untouched.
Authenticated Peer live route leasing is now active on the shared Quick Tunnel/Rendezvous host path.
The lease key is `(remote_peer_id, session_id, client_instance_id)`: a replacement route revalidates
the exact active membership and session scope, catches up replay, then atomically inherits the existing
subscriber/driver identity. The superseded route loses terminal and config authority, a late old close
cannot remove the replacement, and stale concurrent candidates cannot overwrite the winner. Independent
client instances still coexist. Peer clients now automatically select a currently verified Quick Tunnel
hint after recoverable Direct reachability/transport failures. The transition keeps the client instance
and committed OUT cursor, drops stale generation callbacks, freezes writes until replay ready, and stays
on Quick Tunnel after fallback to prevent flapping. Authentication, protocol and backpressure failures do
not downgrade. Relay-account/Peer cross-principal routing and automatic failback remain deferred.

## PR 4.2 - Automated handover

- Direct establishment stops unnecessary Relay/Quick terminal byte flow。
- Direct loss resumes from last committed OUT seq。
- Late frames from the old route are dropped by route generation + seq。
- Input freezes during ambiguous ownership; it is never sent twice。
- Cooldown/hysteresis prevents flapping。
- Sync channel can choose any authenticated Peer route independently of terminal subscriber lifecycle。

Implementation status: the Peer-only `Rendezvous direct -> Quick Tunnel WSS` handover is implemented.
The availability decision is a bounded token-free Go capability query; endpoints and membership tokens
remain outside the renderer. Existing generation/cursor guards prove stale OUT is dropped, replay is
deduplicated, and queued input is released only on the replacement route's `DIRECT_READY`. Relay fallback,
cross-principal handover and cooldown-based Direct failback remain future slices. Deterministic client and
host soak tests now force 100 route replacements: the client retains one instance/cursor, drops duplicate
and stale OUT, and emits each queued write once; the host keeps one subscriber/driver lease and permanently
rejects every superseded route. Quick Tunnel now probes a Direct failback only after a 30-second stable
cooldown, with exponential retry capped at five minutes. The Direct candidate keeps the same identity and
cursor, freezes writes until replay ready, and either promotes atomically or reconnects Quick Tunnel from
the committed cursor without trusting an asynchronously revoked old writer.
Cross-principal Relay handover and cross-process NAT/network-switch soak remain future work.

## PR 4.3+ - Capability expansion

Each capability ships as a separate PR with explicit principal/frame allowlist, owner-host enforcement, size/cancellation tests and redacted diagnostics:

1. Paste image/file。
2. Remote file explorer。
3. Remote session create from profile。
4. Remote Web Preview with independent byte channel。

`full` permission does not automatically allow a new frame until its own enforcement PR lands.

Implementation status: items 1-4 are implemented. Peer image/file paste reuses the existing
`PASTE_IMAGE` / `PASTE_FILE` protocol payloads over both Rendezvous Direct and Quick Tunnel,
without Relay credentials or `account_key`. The renderer requires ready + driver + `full` and does
not queue large blobs across handover. The owner host independently revalidates the exact active
membership/session scope, current owner policy, driver and route lease, then bounds encoded and
decoded payload sizes before forwarding to the local session. Peer Remote File Explorer reuses
`FS_REQUEST` / `FS_RESPONSE` / `FS_EVENT` and the shared desktop/web panel over both Rendezvous
Direct and Quick Tunnel. Each authenticated host attempt owns its worker/watch lifecycle; requests
and outbound results revalidate membership, scope, owner `full` policy and route lease, while route
changes reject client pending RPCs and discard stale results. The Peer record supplies E2EE, so FS
payloads stay single-segment and do not depend on Relay `account_key`; read/write caps and permanently
denied credential directories remain unchanged. Remote profile launch reuses `SESSION_CREATE` /
`SESSION_CREATED` over a temporary authenticated Peer terminal route. The client selects a discoverable
control/full session on the requested host as an authorization anchor, prefers Direct over Quick Tunnel,
and sends only request/host/profile ids. The owner resolves the profile locally, rechecks membership,
session scope, owner policy and route lease before fork and response, limits each route to one in-flight
create, and drops late results after downgrade or route replacement. The request is never retried after it
starts. A target currently needs at least one discoverable session; zero-session host control remains a
future extension. Peer Remote Web Preview reuses `SERVICE_OPEN` / `SERVICE_OPENED` /
`SERVICE_CLOSE` for control but carries TCP bytes in a new authenticated `RecordService` logical
channel, independently of terminal frames and PTY subscriber lifecycle. Direct Pion and Quick Tunnel
WSS use the same bounded service codec; WSS schedules service below terminal and above config sync.
The owner requires the current driver plus effective `full`, revalidates exact membership/session
scope and route lease on every message, and closes services on downgrade, driver loss, detach or route
replacement. Loopback-only targets, 4 services per route, 16 connections per service, 512 MiB per-end
byte budgets and bounded backpressure are covered by transport and real TCP round-trip tests. The local
gateway receives only an opaque native attempt id; Relay tickets, Relay credentials and `account_key`
do not enter the Peer path.

## PR 4.x - Additional reachability

- [x] User-configured TURN: local-only URLs/username plus a keychain credential are injected as a
  separate Pion ICE server, and a selected `relay` candidate is visibly labelled `TURN relay`。
- [x] LAN/mDNS route hints: optional DNS-SD `_atterm-peer._tcp` advertisement and browsing use an
  epoch-scoped opaque tag; only active members can resolve it, results remain memory-only, Manual LAN
  routes take precedence, and the membership handshake remains authoritative。
- [x] Manual host/port + fingerprint: explicit IPv4/IPv6 listener, signed `manual_lan` bootstrap,
  locally persisted endpoint/fingerprint binding, authenticated catalog/config control route and native
  terminal route are implemented. Discovery runs in parallel with Rendezvous and deduplicates by
  `session_id`; the same membership handshake, encrypted records and route lease remain authoritative。
- [x] IPv6 direct candidates: explicit IPv6 advertised hosts select a `tcp6 [::]` listener, signed and
  discovered routes use bracketed URLs, mDNS publishes/accepts AAAA, and link-local candidates preserve
  the interface zone without changing membership authorization。
- [x] Completely no-public-infrastructure mode: a persistent local LAN-only policy preserves public
  route settings but transactionally stops Quick Tunnel, suppresses Rendezvous/STUN/TURN, rejects Direct
  and cached/imported Quick Tunnel candidates, and keeps Manual LAN plus mDNS operational. Relay account
  connectivity remains an explicit, separate user control。

Every route reuses Peer membership/handshake/record encryption; none creates a new trust model.
Manual LAN can perform invitation redemption, catalog and config exchange with Relay, Rendezvous, STUN
and Quick Tunnel disabled. A hermetic dual-identity integration test now exercises the production TCP
listener, signed invitation redemption, reserved control catalog/config exchange, terminal replay,
driver claim, input delivery and subscriber cleanup with every public route disabled. The stage exit
checkbox remains open until the same flow is exercised on two packaged desktop installations. The E2E
now enables the production LAN-only policy while leaving the stored Rendezvous/STUN configuration active,
so public-route suppression is exercised rather than simulated by clearing every setting.

## Hardening Program

- Fuzz ticket/handshake/record/fragment/sync/signaling parsers。
- NAT matrix: host、srflx、symmetric、UDP blocked、network switch。
- Chaos: Relay/Rendezvous restart、Quick URL rotation、route flap、late frames。
- Soak: large scrollback + config snapshot + mobile reconnect。
- Resource bounds: connections、reassembly、op log、tombstones。
- Grant/key expiry renewal and trust export/import without plaintext private keys。
- Battery/data measurement across desktop idle、iOS foreground and Web background。

## Stage Exit Gate

- [ ] Same session is shown once across available route candidates.
- [ ] 100 forced handovers have no OUT gaps/duplicates and no duplicate IN.
- [ ] Direct success removes high-volume bytes from Relay/Quick paths.
- [ ] Every expanded capability has transport and desktop enforcement tests.
- [ ] Peer-only mode never requires Relay login/account key.
- [ ] LAN/manual mode operates with all public services disabled.
- [ ] No path logs or exports `account_key`, Peer private key or epoch key.

Minimum full gate:

```bash
go test -race ./...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

Automatic fallback remains opt-in until WebKit/iOS and all packaged desktop platforms pass release soak.
