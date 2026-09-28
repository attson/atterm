# Accountless P2P Stage 2 - Peer Space and Quick Tunnel

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 1](./2026-09-22-accountless-p2p-stage-1-relay-acceleration.md)
> Milestones: P3-P5
> Release: v0.7.0 beta, v0.7.x stable after soak
> Outcome: 用户不登录 Relay，也能通过预签邀请和 Quick Tunnel 接管终端并同步配置。

Implementation status (2026-09-24): PR 2.1 foundation is in progress on
`feature/peer-space-foundation`. P-256 desktop identity storage、immutable
genesis、signed membership、route-independent `atp1` invitation batches、
encrypted invite ledger and cross-process single-use/revocation writes are
implemented. Signed `apj1` join requests、idempotent membership issuance、Web
non-exportable IndexedDB identity and iOS Keychain identity adapters are also
implemented locally. Signed `atc1` ConnectionBundle and the Stage 1-compatible
Peer membership handshake authenticator are implemented locally as well. The
transport-independent config replica core is now implemented locally too:
exact-byte signed immutable ops、bounded HLC、contiguous version vectors、
counter-fork detection、causal/LWW scalar merge and record-level remove-wins
tombstones. The durable cross-process op log、post-fsync ack frontier and signed
snapshot/compaction path are also implemented locally. Separate signing/ECDH
identities、sync/vault epoch keys、capability-gated per-member envelopes and
context-bound encrypted config payloads are implemented at the Go core layer.
Memberships and join requests now bind the separate wrapping public key;
Desktop persists its wrapping private key in a dedicated keyring slot、Web uses
a non-exportable IndexedDB ECDH key with v1 migration、and iOS stores the v2
ECDH material in Keychain. Signed `akr1` epoch rotations now bind the actor
membership、exact capability-filtered recipient set、parent rotation hash and
key commitment. The resolver retains concurrent branches but deterministically
selects the lower operation hash and requires loser branches to rebase. Signed
`arv1` member/grant/issuer-scoped batch revocations now form a grow-only
deny-wins set; their exact tokens and derived denial maps are atomically stored
in the encrypted cross-process Peer Space ledger. Transport-independent
anti-entropy inventory、bounded cursor batches、large-token chunking、retry
deduplication and verified reassembly now cover config snapshots/tails plus
revocation/rotation candidates. The canonical schema now covers all 18 legacy
Relay preference keys、separates portable/vault collections、splits ordered
template records by stable id, and maintains durable-ready per-realm Relay
import/export hashes so echoes do not create another mutation. Desktop still
needs to persist and wire this compatibility state in place of direct
`prefssync` writes. The record materializer now scopes whole-value deletions
to records previously known by that Relay key, keeps ordered entity positions,
and can atomically append mixed sync/vault encrypted mutation batches. The
Desktop compatibility codec now round-trips the legacy encrypted profile/SSH
blobs while splitting portable metadata、ordered positions、default profile and
vault secrets into independent records. Desktop now creates signed sync/vault
root rotations、persists their tokens in encrypted Peer Space state, stores
opened keys in Keychain, and can
recover missing Keychain entries from the device-bound rotation envelope.
Desktop now also restores a per-Space durable config replica with its signing
identity and current capability-gated epoch keys without making app startup
depend on Peer state. Space creation atomically seeds customized portable
preferences、templates、profile/SSH metadata and explicitly opted-in profile
environment values; SSH credentials/private keys remain local pending a
dedicated Peer secret-sync opt-in. Local-writer migration、new-device join
transport/UI and the actual Peer logical-channel adapter still remain. Relay
compatibility hash/timestamp/record-ref state is persisted per Space and realm
with cross-process transactional updates. Canonical winners can now be
decrypted by exact epoch、grouped per legacy key and materialized back into
Relay scalar/ordered/sealed values, ready for adapter wiring.
Identity, route metadata and handshake authentication alone do not provide a
remote connection.

## Why Quick Tunnel Before Rendezvous

Quick Tunnel 把 signaling/WSS endpoint 直接暴露到当前 desktop，不要求先部署新的公共服务。它先验证完整的账户无关信任、邀请、配置同步和 fallback 数据路径；Rendezvous 之后只解决稳定发现/信令，不再承担身份系统首发风险。

代价需要明确：Quick Tunnel URL 临时且每次启动可能变化。没有 Rendezvous 时，主机重启后客户端需要获得新的 route bundle；trust/membership 不失效，只是旧地址失效。

## PR 2.1 - Peer identity, Space, and invitations

Implement Stage 0 accountless decisions:

- P-256 device identity on Desktop keyring、iOS Keychain、Web non-exportable IndexedDB key。
- Immutable Space genesis and signed membership chain。
- `can_invite` / `can_sync_secrets` capability narrowing and depth=1 delegation。
- Route-independent CapabilityTicket + share-time ConnectionBundle。
- Issuer-bound `redemption_peer_id` atomic single-use ledger。
- Deny-wins member/grant/batch revocation and deterministic epoch rotation。

The direct transport from Stage 1 gets a second `HandshakeAuthenticator` for Peer membership; it does not fork a new WebRTC/frame stack.

## PR 2.2 - Decentralized config replica

Create `internal/configsync/`:

- Signed immutable ops、monotonic device counters、HLC、version vector。
- Scalar causal/LWW、record-level remove-wins map、stable ordering positions。
- Materialized view、durable ack、signed snapshot/compaction。
- Separate sync/vault epoch keys and per-member envelopes。
- Existing `prefssync` becomes the optional Relay compatibility adapter, not a second writer。

Migration and bootstrap:

- Current 18 synced keys migrate without losing Relay-only compatibility。
- templates/profiles/SSH hosts merge per record, not whole-array LWW。
- Vault values are opt-in and only sent to `can_sync_secrets` members。
- Joining device adopts Space snapshot; existing local customizations become explicit pending imports。

Property tests use randomized A↔B↔C partitions and at least 10,000 seeds.

## PR 2.3 - cloudflared lifecycle and local gateway

Create `internal/quicktunnel/` using the Stage 0 packaging decision:

- Loopback peer gateway on a random port。
- `cloudflared tunnel --url http://127.0.0.1:<port>` process lifecycle。
- Strict `https://*.trycloudflare.com` URL parsing。
- 30-second start、5-second stop、process-tree/app-shutdown cleanup。
- No silent download of an unverified executable。

Gateway supports encrypted signaling and binary WSS. It authenticates only through invitation/membership proof, never Relay credentials.

## PR 2.4 - Quick Tunnel WebRTC and WSS paths

- ConnectionBundle carries the current temporary route and signature。
- Use tunnel signaling to attempt the existing WebRTC direct path first。
- If ICE fails and user allows it, continue over the same application-encrypted WSS record layer。
- UI labels paths `Direct` and `Quick Tunnel` accurately。
- WSS scheduler priority: input/control > terminal output > config sync。
- Cloudflare sees IP/timing/size but not terminal/config plaintext。

## PR 2.5 - Join, bootstrap, and reconnect UX

- QR scan、paste、deep link and fingerprint/permission confirmation。
- Redeem only against `redemption_peer_id`；issuer must be online, user interaction is not required。
- Persist membership/epoch envelopes before reporting join success。
- Run config anti-entropy on a separate logical channel that does not count as a terminal subscriber。
- Within one tunnel lifetime, reconnect using membership without another invitation。
- After host restart/URL rotation, request/share a new signed route bundle; do not mint a new membership。

## PR 2.6 - Settings and account separation

- Resize Settings adaptively and fix content scrolling。
- Merge account config and signed-in devices into `Relay 账户`。
- Add `Peer 连接`: identity、Space members、invitations、sync state and Quick Tunnel lifecycle。
- Relay devices and Peer members remain separate lists and concepts。
- Desktop shows host controls; Web/Capacitor show client/join/sync controls。

## Stage Exit Gate

- [ ] Fresh Desktop/Web/iOS client joins and controls a desktop without Relay login.
- [ ] Same invitation/trust survives Quick Tunnel route URL changes; only route bundle refreshes.
- [ ] WebRTC is preferred; forced ICE failure uses encrypted WSS only with consent.
- [ ] Three devices eventually converge config through available Peer connections.
- [ ] No online peer means visible pending sync, not data loss or hidden cloud storage.
- [ ] Vault data is off by default and capability-gated.
- [ ] Settings is scrollable at desktop/mobile target sizes.
- [ ] Existing Relay acceleration still works and remains independently disableable.

Verification:

```bash
go test -race ./internal/peer... ./internal/configsync/... ./internal/quicktunnel/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

## Not In This Stage

- Official/self-hosted Rendezvous or stable member discovery。
- TURN、LAN/mDNS、manual address。
- Advanced file/preview/session-create capabilities。
