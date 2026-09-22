# Accountless P2P Stage 2 - Authenticated Transport and Rendezvous

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 1](./2026-09-22-accountless-p2p-stage-1-space-sync.md)
> Milestones: P3-P4
> Outcome: 两个已有 Space 成员可经官方/自建 Rendezvous 建立加密 WebRTC 连接，复用现有 terminal protocol，并进行配置 anti-entropy。

## Entry Gate

- Peer identity、membership verification、encrypted record、config replica APIs 稳定。
- Stage 0 已决定 session router reuse boundary。
- 现有 Relay lazy subscriber、permission、replay tests 有可复用基线。

## PR 2.1 - Binary transport abstraction and Pion host

Create `internal/peertransport/`:

- Pion PeerConnection lifecycle。
- ordered/reliable `atterm-terminal-v1` DataChannel。
- ordered/reliable `atterm-sync-v1` DataChannel。
- 16 KiB fragment/reassembly with 16 MiB hard cap。
- bounded queues、buffered-amount watermarks、cancellation and close reasons。

The package owns transport primitives only and must not import `desktop/`.

## PR 2.2 - Authenticated handshake and record layer

Implement Stage 0 transcript over DataChannel:

- Invite redemption hello or membership/grant reconnect hello。
- Signed ephemeral P-256 ECDH transcript。
- independent C→H/H→C keys and monotonic record counters。
- channel/connection/space/peer binding in AAD。
- replay、counter rollback、tamper、wrong genesis and revoked membership fail closed。

WebRTC DTLS is never treated as Peer authorization; terminal/sync payloads remain encrypted at the application layer.

## PR 2.3 - Session router integration

Refactor/adapter selected in Stage 0:

- Existing WebSocket clients continue unchanged。
- Peer principal carries device/grant/session scope。
- `LIST_RESP` exposes local-owned sessions only and filters allowlist。
- Effective permission is the minimum of session owner、grant and invitation scope。
- Peer router rejects unknown/non-MVP `proto.Type` by default。
- `desktop/uplink.go` performs the second permission check before PTY/local capability access。

Tests must prove Peer never republishes Relay-mirrored sessions.

## PR 2.4 - Terminal lifecycle and sync channel

Terminal channel acceptance:

- LIST/ATTACH/REPLAY_PROGRESS/OUT/CLAIM_DRIVER/IN/RESIZE/PING work。
- 0→1/N→0 subscriber lifecycle drives stream start/stop exactly once。
- Detach/ICE failure removes subscriber and driver lease。
- Sync channel does not increment terminal subscriber count。

Sync scheduling:

- Exchange schema/version vectors/digests。
- Durable op batches and acks。
- Snapshot fallback after compaction。
- Separate DataChannel prevents sync head-of-line blocking terminal traffic。

## PR 2.5 - Minimal Rendezvous service

Create:

- `internal/rendezvous/`
- `cmd/atterm-rendezvous/`

Implement:

- TLS/WSS-only production mode and strict Origin policy。
- Host/member presence with identity challenge signature。
- Opaque topic routing for encrypted offer/answer/trickle ICE payloads。
- 120-second TTL、64 KiB message cap、per-IP/topic/global limits。
- Health/metrics without payload or secret logs。
- No account DB、config storage、terminal frames、TURN or store-and-forward。

Official and self-hosted deployments run the same contract suite.

## PR 2.6 - Dialing, retry, and route diagnostics

- Official/custom/disabled Rendezvous configuration。
- STUN configuration and metadata disclosure copy。
- Host registration backoff and client dialing timeout。
- Small Space dials all reachable members for sync; presence is never membership truth。
- Distinguish signaling failure、ICE failure、auth failure、permission failure。
- `new RTCPeerConnection`/`new WebSocket` synchronous exceptions enter retry/error state instead of escaping boot.

## Stage Exit Gate

- [ ] Two desktop test processes list/attach/control over Pion without Relay login.
- [ ] Browser client performs view/control with permission enforcement at both boundaries.
- [ ] Large replay reports progress and stays responsive.
- [ ] Config ops converge over the sync channel under terminal output load.
- [ ] Rendezvous packet/log inspection contains no invite secret、SDP plaintext or terminal/config plaintext.
- [ ] Rendezvous outage does not affect local PTY or existing Relay uplink.
- [ ] Official and self-hosted endpoints pass identical contract tests.

Verification:

```bash
go test -race ./internal/peertransport/... ./internal/rendezvous/... ./internal/relay/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
```

## Not In This Stage

- Polished QR/join/Settings UX。
- Quick Tunnel/TURN/Relay route fallback。
- File、preview、remote session create frames。
