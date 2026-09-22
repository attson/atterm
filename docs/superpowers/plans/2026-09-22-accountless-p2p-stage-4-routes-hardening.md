# Accountless P2P Stage 4 - Optional Routes and Hardening

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 3](./2026-09-22-accountless-p2p-stage-3-client-settings-mvp.md)
> Milestones: P7-P9
> Outcome: 在不改变 Peer trust model 的前提下增加 Quick Tunnel、Peer-first/Relay-fallback，以及逐项开放高级远程能力。

## Entry Gate

- MVP 已完成 beta soak，直连、同步、撤销和设置页无高优先级缺陷。
- Stage 0 Quick Tunnel spike 给出可接受的 WSS 与供应链结论。
- Route metadata 与 active-route lease 已在 Stage 3 稳定。

## PR 4.1 - cloudflared lifecycle

Create `internal/quicktunnel/`:

- Resolve approved bundled/system binary according to Stage 0 decision。
- Start `cloudflared tunnel --url http://127.0.0.1:<random-port>`。
- Parse only validated `https://*.trycloudflare.com` URL。
- 30-second startup、5-second stop、process-tree cleanup、app shutdown cleanup。
- No silent download of unverified executable。

Settings shows unavailable/start/starting/online/error/stopped and third-party privacy copy.

## PR 4.2 - Tunnel signaling and encrypted WSS fallback

- Local loopback peer gateway supports encrypted signaling and binary WSS records。
- Share-time ConnectionBundle receives the temporary route and signature; CapabilityTicket remains unchanged。
- Try WebRTC first through tunnel signaling。
- Switch to WSS only after ICE timeout and only when the user allows tunnel fallback。
- UI labels the path `Quick Tunnel`, never `Direct`。
- WSS scheduler priority: input/control > terminal output > config sync。

Acceptance includes forced UDP block and cloudflared crash/restart.

## PR 4.3 - Hybrid route manager

Implement one route state machine per `session_id`:

```text
Direct Peer > enabled Quick Tunnel > enabled Relay
```

- Exactly one active subscriber/driver route lease。
- Candidate health does not create duplicate sidebar sessions。
- Route handover dedupes OUT sequence and never duplicates IN。
- Direct establishment stops unnecessary Relay stream subscription。
- Relay logout degrades to Peer-only without blocking UI。
- Automatic mid-session fallback ships only after deterministic handover tests pass。

## PR 4.4+ - Expand capabilities one at a time

Each item is a separate PR with explicit frame allowlist、owner-host enforcement、size limits、cancellation and audit-safe logs:

1. Paste image/file。
2. Remote file explorer operations。
3. Remote session create from profile。
4. Remote Web Preview control + independent byte channel。

Do not batch these permissions into a single “full means everything” switch. Unknown/new frame types remain denied until their own PR lands.

## PR 4.x - Additional reachability

Independent later slices:

- User-configured TURN; UI labels ICE path as relayed, not direct。
- LAN/mDNS route hints。
- Manual host/port + fingerprint。
- IPv6 direct candidates。
- Completely no-public-infrastructure mode。

Each route reuses Peer membership/handshake/record encryption and must not create a new trust system.

## Hardening Program

- Fuzz ticket/handshake/record/fragment/sync/signaling parsers。
- NAT matrix: host、srflx、symmetric、UDP blocked、network switch。
- Chaos: Rendezvous restart、Quick Tunnel URL rotation、route flap、late frames。
- Soak: large scrollback + config snapshot + mobile reconnect。
- Resource limits: connection count、reassembly bytes、op-log growth、tombstone retention。
- Key/grant expiry renewal and explicit trust export/import without plaintext private keys。
- Battery/data measurements for desktop idle、iOS foreground and Web tab background。

## Stage Exit Gate

- [ ] Quick Tunnel alone can join and attach without Relay/Rendezvous account.
- [ ] Direct success removes terminal traffic from Quick Tunnel.
- [ ] Forced direct failure uses WSS only with consent and correct route label.
- [ ] Hybrid shows one session and never duplicates input/output during handover.
- [ ] Every expanded capability has transport + desktop enforcement tests.
- [ ] LAN/manual mode operates with public services disabled.
- [ ] No route stores or derives Relay `account_key`.

Verification grows with each slice; minimum full gate:

```bash
go test -race ./...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

Release soak must include WebKit/iOS and all packaged desktop platforms before enabling automatic fallback by default.
