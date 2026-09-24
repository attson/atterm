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

- Publish one protocol and contract suite for both deployments。
- Official/custom/disabled URL configuration。
- Self-host docs for TLS、origins、rate limits and reverse proxy。
- Browser secure-context validation before attempting WebCrypto/WebRTC。
- STUN settings and metadata-disclosure documentation。

## PR 3.3 - Stable member discovery and sync dialing

- Space members register rotating presence identifiers derived from Space material, not email/account ids。
- Small Spaces attempt anti-entropy with all reachable peers; larger Spaces use bounded fanout based on vector lag。
- A/B/C config propagation does not require one permanent hub。
- iOS/Web only promise foreground/system-allowed participation。
- Presence TTL expiration removes reachability only, never membership/grants/config state。

## PR 3.4 - Client route integration

- Add Rendezvous signaling adapter to the transport established in Stage 1。
- Existing membership authenticator from Stage 2 handles the connection。
- ConnectionBundle can carry Rendezvous and Quick Tunnel hints without changing CapabilityTicket。
- Retry/backoff distinguishes service unavailable、peer offline、ICE failed、auth failed。
- UI shows `Direct via Rendezvous signaling`; terminal route remains `Direct` after setup。

## PR 3.5 - Settings and operational UX

`Peer 连接` gains:

- Official/custom/disabled Rendezvous selection。
- Reachability latency/last registration/error。
- Self-host URL validation and privacy copy。
- Per-device last seen/sync state without implying server-side authority。
- Manual reconnect/sync actions and diagnostic export。

## Stage Exit Gate

- [ ] A trusted client reconnects after desktop restart without receiving a new Quick Tunnel URL.
- [ ] Official and self-hosted services pass the same contract suite.
- [ ] Rendezvous restart only drops ephemeral presence/signaling.
- [ ] Service logs/packet inspection contain no invite secret、SDP plaintext、config or terminal bytes.
- [ ] Rendezvous unavailable leaves local terminal、Relay and Quick Tunnel paths usable.
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
