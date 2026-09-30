# Accountless P2P Stage 3 - Stable Rendezvous

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 2](./2026-09-22-accountless-p2p-stage-2-quick-tunnel-peer-space.md)
> Milestones: P6-P7
> Release: v0.8.0
> Outcome: Peer Space 成员通过官方或自建 Rendezvous 稳定发现并建立直连，不依赖临时 Quick Tunnel URL。

## Product Boundary

- Rendezvous 只做 presence/discovery/signaling。
- 不创建账户、不保存配置、不转发 terminal frames、不充当 TURN。
- Membership/governance 仍由 Peer Space 决定；Rendezvous presence 不是成员真相源。
- WebRTC 成功后所有数据绕过 Rendezvous。
- Direct 失败时可使用已启用的 Quick Tunnel；没有 Quick Tunnel/TURN 时明确失败。

## PR 3.1 - Minimal Rendezvous service

Implementation status: the standalone stateless service, v1 challenge/register/publish protocol,
120-second bounded mailbox, retry dedupe, exact Origin enforcement, TLS/loopback-proxy startup gates,
and payload-free health/metrics are implemented. Client route integration remains in PR 3.3/3.4.

Create:

- `internal/rendezvous/`
- `cmd/atterm-rendezvous/`

Implement:

- TLS/WSS-only production posture and strict Origin policy。
- Identity challenge for host/member presence。
- Opaque-topic routing for end-to-end encrypted SDP/ICE messages。
- 120-second mailbox TTL、64 KiB message cap、per-IP/topic/global bounds。
- Health/metrics without payload logs。
- Stateless restart behavior; no user/config database。

## PR 3.2 - Official/self-hosted contract

Implementation status: canonical official/custom/disabled endpoint and STUN configuration,
browser capability preflight, self-host container/docs, and the shared black-box contract runner
are implemented. Settings presentation and connection lifecycle remain in PR 3.4/3.5.

- Publish one protocol and contract suite for both deployments。
- Official/custom/disabled URL configuration。
- Self-host docs for TLS、origins、rate limits and reverse proxy。
- Browser secure-context validation before attempting WebCrypto/WebRTC。
- STUN settings and metadata-disclosure documentation。

## PR 3.3 - Stable member discovery and sync dialing

Implementation status: sync-epoch-derived opaque topics, 15-minute rotating member presence,
adjacent-slot resolution, ephemeral registration challenge identities, an in-memory reachability
directory, and deterministic all-peer/bounded-fanout config-sync planning are implemented. The
Desktop planner revalidates active membership and joins reachability only to durable peer
acknowledgement vectors. Pion signaling, automatic reconnect/backoff, and actual route dialing remain
in PR 3.4, so the Stage A/B/C network convergence exit gate is not claimed yet.

- Space members register rotating presence identifiers derived from Space material, not email/account ids。
- Small Spaces attempt anti-entropy with all reachable peers; larger Spaces use bounded fanout based on vector lag。
- A/B/C config propagation does not require one permanent hub。
- iOS/Web only promise foreground/system-allowed participation。
- Presence TTL expiration removes reachability only, never membership/grants/config state。

## PR 3.4 - Client route integration

Implementation status: the pairwise-encrypted signaling adapter, multiplexed Pion attempts,
stable route failure categories, Desktop host registration/reconnect/rotation lifecycle, shared
Quick Tunnel/Rendezvous terminal attachment runtime, and mixed or Rendezvous-only member reconnect
bundles are implemented. First invitation redemption remains Quick Tunnel-only. The transport API
is covered with real in-memory Rendezvous + Pion tests; end-user route selection/status presentation
remains PR 3.5, so the Stage exit gate is not claimed here.

- Add Rendezvous signaling adapter to the transport established in Stage 1。
- Existing membership authenticator from Stage 2 handles the connection。
- ConnectionBundle can carry Rendezvous and Quick Tunnel hints without changing CapabilityTicket。
- Retry/backoff distinguishes service unavailable、peer offline、ICE failed、auth failed。
- UI shows `Direct via Rendezvous signaling`; terminal route remains `Direct` after setup。

## PR 3.5 - Settings and operational UX

Implementation status: Desktop Settings now exposes official/custom/disabled Rendezvous and
default/custom/disabled STUN selection, validates and persists local-only endpoints, reports
registration state/latency/time/reachable presence count with stable error codes, and provides
manual reconnect plus authenticated-channel config sync. The Peer directory reports each member's
last direct config exchange, member reconnect bundles can be copied with Rendezvous alone online,
first-join bundles remain Quick Tunnel-only, and diagnostics include only a redaction-safe aggregate
Rendezvous summary. End-user accountless session discovery/attach still needs a separate protocol,
so the Stage exit gate is not claimed by this PR.

`Peer 连接` gains:

- Official/custom/disabled Rendezvous selection。
- Reachability latency/last registration/error。
- Self-host URL validation and privacy copy。
- Per-device last seen/sync state without implying server-side authority。
- Manual reconnect/sync actions and diagnostic export。

## PR 3.6 - Desktop session discovery and attach

Implementation status: Desktop now requests a bounded, paginated session catalog over the existing
pairwise-encrypted Rendezvous route, while the host revalidates active membership, revocation,
session scope and permission ceilings for every request. Catalog reads create no terminal subscriber
or Pion attempt. The Desktop sidebar merges discovered Peer sessions by authoritative session id,
prefers a Relay entry when both exist, and opens Peer-only sessions through the native Go/Pion client
without a Relay `/client` WebSocket. Attach re-reads durable Peer state and reuses the existing
membership handshake, host permission enforcement, periodic revocation check and config channel.
Automated in-memory catalog/Pion tests and frontend direct-only connection tests are implemented;
the real two-Desktop flow has also passed local acceptance: discovery, control, restart without a
new Quick Tunnel URL, view-only enforcement, member revocation and Rendezvous failure isolation.

- Discover only sessions authorized by both members' scopes and effective permission ceilings。
- Keep session metadata, SDP/ICE, membership material and terminal bytes opaque to Rendezvous。
- Cache route identity only in Go; renderer receives no membership token or Peer route secret。
- Reconnect a selected session through native Pion with replay de-duplication and no Relay fallback。
- Keep local terminal, Relay and Quick Tunnel lifecycle independent from Rendezvous discovery failure。

## Stage Exit Gate

- [x] A trusted client reconnects after desktop restart without receiving a new Quick Tunnel URL.
- [ ] Official and self-hosted services pass the same contract suite.
- [x] Rendezvous restart only drops ephemeral presence/signaling.
- [ ] Service logs/packet inspection contain no invite secret、SDP plaintext、config or terminal bytes.
- [x] Rendezvous unavailable leaves local terminal、Relay and Quick Tunnel paths usable.
- [ ] A/B/C sync converges through rotating online peers without a designated hub.
- [ ] Restrictive NAT is reported honestly; Rendezvous alone is not called a data relay.

Verification:

```bash
go test -race ./internal/rendezvous/... ./internal/peer... ./internal/configsync/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

## Not In This Stage

- TURN/SFU or persistent encrypted mailbox。
- Automatic route handover across all three route families。
- File/preview/session-create expansion。
