# Accountless P2P Stage 1 - Relay P2P Acceleration

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 0](./2026-09-22-accountless-p2p-stage-0-relay-p2p-foundation.md)
> Milestones: P1-P2
> Release: v0.6.0 beta, v0.6.x stable after soak
> Outcome: 已登录 Relay 的设备优先直连传 terminal bytes，失败时无感回到现有 Relay path。

## Product Boundary

- 用户仍需登录同一 Relay account。
- Session list、presence、permission、driver arbitration 和配置同步仍由 Relay 提供。
- WebRTC 只优化数据路径；不创建 Peer Space、不签发账户无关 invitation。
- UI 文案是“直连加速”，不是“无 Relay 模式”。

## PR 1.1 - Relay signaling control plane

Add a dedicated authenticated signaling surface:

- Existing Bearer/subprotocol auth and connection/rate limits apply。
- Client requests a direct attempt for one `session_id`。
- Relay verifies same account、session ownership、permission and active uplink。
- Relay issues short-lived single-use direct ticket and routes opaque SDP/ICE messages。
- Signaling messages have strict size/TTL/in-flight bounds and no payload logging。

Relay never receives `account_key` or derived direct record keys.

## PR 1.2 - Desktop Pion host and auth-provider abstraction

Create reusable transport primitives under `internal/peertransport/` and keep orchestration in `desktop/uplink.go`:

- Pion PeerConnection and terminal DataChannel。
- Pluggable `HandshakeAuthenticator` interface。
- First implementation verifies Relay ticket + account-key transcript proof。
- Fragment/reassembly、record encryption、counter replay protection、backpressure。
- Connection/candidate cleanup and bounded concurrent direct attempts。

The future Peer membership authenticator must plug in without changing frame transport.

## PR 1.3 - Shared browser client transport

Refactor `desktop/frontend/src/lib/connection.ts` around `BinaryFrameTransport`:

- Existing Relay WebSocket adapter remains default and behavior-compatible。
- RTCDataChannel adapter carries existing `proto.Marshal(Frame)` bytes inside direct records。
- `SessionConnection` keeps one OUT seq cursor across route changes。
- `new RTCPeerConnection` and signaling WS synchronous failures enter normal retry/fallback state。

Wails/Web/Capacitor use native browser WebRTC; no second terminal UI is introduced.

## PR 1.4 - Direct upgrade and Relay fallback state machine

Implement one route lease per attached session:

```text
relay_attached -> direct_connecting -> direct_replay -> direct_active
       ^                                      |
       +------------- fallback ---------------+
```

Rules:

- Attach through Relay first; direct upgrade is opportunistic。
- Direct replay begins from client last committed OUT seq。
- Relay subscriber is released only after direct catch-up and DIRECT_READY。
- Direct loss reattaches Relay from the same seq before accepting new input。
- Exactly one route may send IN/RESIZE/CLAIM_DRIVER at a time。
- Route flapping uses cooldown; no endless connect loop。

## PR 1.5 - Lazy stream, permission, and E2EE hardening

- Direct subscriber participates in the same 0→1/N→0 lifecycle。
- Switching routes does not momentarily drop the logical subscriber to zero or create two uploads。
- Relay checks permission before issuing ticket; desktop checks again before PTY/local access。
- Existing sealed TypeOut bytes may pass unchanged, while the direct record layer also protects IN/control frames。
- Unknown/new frame types default deny on the direct path。
- Account-key unlock failure skips direct acceleration and keeps Relay behavior; it never falls back to an unauthenticated direct channel。

## PR 1.6 - UX, metrics, and staged rollout

- Relay settings gains `Prefer direct connection` toggle, default off during beta。
- Session route indicator: `Direct` / `Relay` / `Connecting direct`。
- Diagnostics: ICE state、candidate type、setup time、fallback reason, with addresses/secrets redacted。
- Relay metrics: direct attempts/success/fallback and estimated relay bytes avoided。
- Rollout can be enabled per build/config without changing account or E2EE defaults。

Implementation status (2026-09-23): PR 1.1-1.6 are implemented on
`design/accountless-p2p`. The per-device toggle lives in the shared General
settings surface because the Relay tab is desktop-only; on desktop it gates
both client upgrades and the direct host listener. Beta remains default-off,
and the Relay-side `--direct-signal` / `ATTERM_DIRECT_SIGNAL_ENABLED` kill
switch remains independent. Automated package, frontend, build, and protocol
contract checks pass. The real-network exit gates below remain intentionally
unchecked until Desktop/Web/iOS and constrained-NAT test runs are recorded.

## Stage Exit Gate

- [ ] Same-account Desktop↔Desktop/Web/iOS sessions upgrade to direct when ICE succeeds.
- [ ] UDP-blocked/symmetric-NAT cases remain usable through existing Relay.
- [ ] No OUT gap/duplicate across 100 repeated direct/fallback transitions.
- [ ] No duplicated IN/resize/driver claim during forced route flaps.
- [ ] Direct active means Relay no longer carries PTY bytes for that subscriber.
- [ ] Lazy stream and REPLAY_PROGRESS semantics remain intact.
- [ ] Relay-only clients and old clients continue to work unchanged.
- [ ] No Peer identity/config migration occurs in this stage.

Verification:

```bash
go test -race ./internal/peertransport/... ./internal/relay/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

## Not In This Stage

- Accountless invitations、Peer Space、decentralized config sync。
- Quick Tunnel、Rendezvous/TURN。
- File、preview、remote session create over direct path。
