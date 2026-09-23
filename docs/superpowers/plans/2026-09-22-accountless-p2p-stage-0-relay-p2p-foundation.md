# Accountless P2P Stage 0 - Relay-assisted P2P Foundation

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Milestone: P0
> Release: v0.6.0 pre-release gate (not shipped independently)
> Outcome: 冻结 Relay 体系内的 WebRTC 加速协议和切路语义；不交付用户功能。

## Why This Comes First

第一版不改变账户、设备授权或配置同步模型。Relay 继续负责登录、会话发现、权限、driver arbitration 和 fallback；只把高流量 terminal data path 从 Relay WS 升级到 WebRTC DataChannel。

这一步先验证最难且最通用的部分：Pion/WebRTC、frame transport、seq handover、lazy subscription 和 NAT 成功率。后续 Quick Tunnel/Rendezvous 复用同一 transport，不重新实现终端通道。

## Entry Gate

- 当前 Relay-only、本地终端、E2EE、driver/viewer、lazy stream 测试基线为绿。
- 现有 `account_key` 只用于 proof/key derivation，仍遵守不出主线程/Keychain/Keyring的红线。
- Spike 不接入生产启动路径。

## PR 0.1 - Relay-assisted direct-path spec

Create `docs/superpowers/specs/2026-09-22-relay-p2p-acceleration-design.md` and freeze:

- Dedicated Relay signaling endpoint/WS contract，不占用 terminal `proto.Type`。
- Relay-issued one-time direct ticket，绑定 user/session/host/client/permission/expiry。
- Pluggable direct handshake auth：Stage 1 使用 `account_key` possession proof；后续使用 Peer membership proof。
- Encrypted record、fragment、nonce/AAD 和 channel labels。
- Relay→Direct、Direct→Relay 的 seq handover state machine。
- Driver lease、subscriber count、STREAM_REQUEST/STOP ownership。
- Failure/timeout/error codes and privacy metadata。

The Relay may authenticate signaling but cannot impersonate either endpoint because it does not know `account_key`.

## PR 0.2 - Cross-runtime handshake vectors

Prototype Go/browser vectors for:

- Relay ticket binding and expiry。
- P-256 ephemeral ECDH。
- `account_key`-bound transcript proof without transmitting the key。
- Directional record keys via HKDF-SHA256。
- XChaCha20-Poly1305 records and monotonic counters。

Tests cover Go↔Browser interoperability、wrong account key、wrong session id、tampered ticket/transcript and replay.

## PR 0.3 - Pion/DataChannel spike

- Pion host ↔ Chromium/Firefox/WebKit ordered reliable DataChannel。
- 16 KiB fragmentation、16 MiB cap、bounded reassembly/backpressure。
- ICE trickle/restart/teardown。
- 10 MiB scrollback replay and interactive input under output load。
- Metrics: setup latency、memory、message-size compatibility。

## PR 0.4 - Router and route-handover spike

Prototype extraction of WebSocket-independent frame dispatch from `internal/relay/client_conn.go` or a narrow adapter owned by `desktop/uplink.go`.

Prove this sequence:

1. Client attaches through Relay and records last OUT seq。
2. Relay signaling connects it to the owning uplink。
3. Direct channel authenticates and requests `since_seq`。
4. Host replays the gap; client deduplicates by OUT seq。
5. Only after DIRECT_READY does Relay remove its terminal subscriber。
6. Direct failure reattaches Relay from the last committed seq。

No frame can be delivered twice to xterm or written twice to PTY. A route transition never creates two driver leases.

## PR 0.5 - Later-path feasibility notes

Non-blocking spikes only:

- Quick Tunnel WebSocket upgrade/lifetime/process management and packaging。
- Accountless Peer identity storage on desktop/Web/iOS。
- Minimal Rendezvous WSS signaling limits。
- Decentralized config merge model for the later no-Relay phase。

These produce decisions, not production code, and cannot delay Relay P2P acceleration.

## Stage Exit Gate

- [x] Relay direct-path spec accepted.
- [x] Go/browser handshake and record vectors pass.
- [ ] WebKit/Pion channel and fragmentation behavior measured.
- [x] Route handover state model proves no output duplicate and no duplicate input across 100 transitions; real session integration remains Stage 1.
- [x] Router reuse boundary recorded; LIST/ATTACH/replay state machine will not be copied.
- [x] Quick Tunnel/Rendezvous/config-sync unknowns have written go/no-go notes.

Verification commands are spike-specific and must be recorded in each PR. No Settings or shipping feature flag is added in Stage 0.
