# Accountless P2P Stage 0 - Spec and Compatibility Spikes

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Milestone: P0
> Outcome: 冻结可实现的 wire/security/sync 规范；不交付用户功能。

## Entry Gate

- 当前 Relay-only 与本地终端测试基线为绿。
- 总计划中的 Peer Space、邀请核销 authority、配置同步和实际 route 命名已经确认。
- Spike 代码允许放在 test/internal tooling；不得提前接入生产启动路径。

## PR 0.1 - Protocol and threat-model spec

Create:

- `docs/superpowers/specs/2026-09-22-accountless-p2p-design.md`
- `docs/testdata/peer/` golden-vector schema/fixtures

Freeze:

- Genesis、membership、DeviceGrant、CapabilityTicket、ConnectionBundle typed schemas。
- Handshake transcript、record header、fragment header、nonce/AAD、channel labels。
- SyncOp、version vector、snapshot、governance/epoch rotation schemas。
- Rendezvous encrypted signaling messages、limits、error codes。
- Threat model：资产、攻击者、信任边界、metadata leakage、fail-closed matrix。

Exit:

- 所有字段有 byte-level signing/AAD 规则；没有“实现时再决定”的 crypto 字段。
- 明确协议版本协商和未知版本行为。
- `internal/proto.Version` 保持 1；Peer envelope 自有版本，不占新 frame Type。

## PR 0.2 - Cross-runtime crypto vectors

Prototype:

- Go P-256 ECDSA key encoding/sign/verify。
- Go/WebCrypto P-256 ephemeral ECDH。
- HKDF-SHA256 + XChaCha20-Poly1305 record derivation。
- P1363 signature encoding、base64url、exact JSON bytes verification。

Tests:

- Go 生成、Browser 验证；Browser 生成、Go 验证。
- Chromium、Firefox、WebKit/WKWebView target 至少各跑一次。
- Transcript 任意 bit 改动、错误 Space genesis、错误 peer id 都验证失败。

Exit:

- Golden vectors 入库并由 Go/Vitest 同时消费。
- 若 WebKit 不支持选定 key persistence/operation，必须在本 PR 改算法或确定 native bridge，不能推迟到客户端阶段。

## PR 0.3 - WebRTC and session-router spike

Prototype:

- Pion host ↔ browser ordered/reliable DataChannel。
- 16 KiB fragmentation、16 MiB hard cap、`bufferedAmount` backpressure。
- candidate trickle、ICE restart、connection teardown。
- 把 `internal/relay/client_conn.go` 的 frame dispatch 与 WebSocket I/O 分离的最小尝试。

Measure:

- WebKit 可接受 message size。
- 10 MiB scrollback replay 的内存峰值与交互延迟。
- terminal 与 sync 使用独立 channel 后的 head-of-line behavior。

Decision output:

- 选择 transport-neutral relay handler，或记录为何只能由 `desktop/uplink.go` 做窄 adapter。
- 禁止复制 LIST/ATTACH/replay/driver state machine。

## PR 0.4 - Decentralized merge simulation

Build a test-only model for three devices A/B/C:

- HLC + device counter + version vector。
- scalar causal/LWW、record-level remove-wins map、ordered position id。
- duplicate/reordered/delayed delivery。
- snapshot + compaction + stale device bootstrap。
- concurrent revocation and epoch rotation winner selection。

Exit:

- 10,000 randomized seeds converge byte-for-byte。
- Security governance does not call ordinary LWW code.
- Document Relay compatibility mapping for server `updated_at` and echo deduplication。

## PR 0.5 - Quick Tunnel feasibility spike

Non-blocking for MVP. Verify only:

- `cloudflared tunnel --url` output on macOS/Windows/Linux。
- WebSocket upgrade, idle lifetime, process stop semantics。
- Apache-2.0 NOTICE, binary size, signing/notarization impact, checksum source。
- System-binary discovery versus bundled binary recommendation。

Do not add production auto-download or Settings controls in this stage.

## Stage Exit Gate

- [ ] Design spec reviewed and status changed from Draft to Accepted.
- [ ] Crypto vectors pass Go + browser runtimes.
- [ ] Router reuse decision recorded with a bounded implementation path.
- [ ] CRDT simulation converges and defines migration semantics.
- [ ] Quick Tunnel spike records go/no-go without blocking Stage 1.

Verification:

```bash
go test ./internal/peerproto/... ./internal/peercrypto/...
cd desktop/frontend && npm test -- peer
```

If packages are still spike-only, equivalent scoped test commands must be recorded in the PR description.
