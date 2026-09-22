# Accountless P2P Stage 4 - Hybrid Routing and Hardening

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 3](./2026-09-22-accountless-p2p-stage-3-rendezvous.md)
> Milestones: P8-P9
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

## PR 4.2 - Automated handover

- Direct establishment stops unnecessary Relay/Quick terminal byte flow。
- Direct loss resumes from last committed OUT seq。
- Late frames from the old route are dropped by route generation + seq。
- Input freezes during ambiguous ownership; it is never sent twice。
- Cooldown/hysteresis prevents flapping。
- Sync channel can choose any authenticated Peer route independently of terminal subscriber lifecycle。

## PR 4.3+ - Capability expansion

Each capability ships as a separate PR with explicit principal/frame allowlist, owner-host enforcement, size/cancellation tests and redacted diagnostics:

1. Paste image/file。
2. Remote file explorer。
3. Remote session create from profile。
4. Remote Web Preview with independent byte channel。

`full` permission does not automatically allow a new frame until its own enforcement PR lands.

## PR 4.x - Additional reachability

- User-configured TURN, visibly labelled relayed ICE path。
- LAN/mDNS route hints。
- Manual host/port + fingerprint。
- IPv6 direct candidates。
- Completely no-public-infrastructure mode。

Every route reuses Peer membership/handshake/record encryption; none creates a new trust model.

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
