# Accountless P2P Stage 2 - Peer Space and Quick Tunnel

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 1](./2026-09-22-accountless-p2p-stage-1-relay-acceleration.md)
> Milestones: P3-P5
> Release: v0.7.0 beta, v0.7.x stable after soak
> Outcome: 用户不登录 Relay，也能通过预签邀请和 Quick Tunnel 接管终端并同步配置。

Implementation status (2026-09-28): PR 2.1 foundation is in progress on
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
template records by stable id, and maintains durable per-realm Relay
import/export hashes so echoes do not create another mutation. Desktop now
persists this compatibility state and has un-wired helpers for durable Relay
imports and retryable exports; replacing direct `prefssync` network writes is
still pending. The record materializer now scopes whole-value deletions
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
dedicated Peer secret-sync opt-in. Local edits can now be reduced to minimal
canonical mutation batches without advancing HLCs for unchanged records, but
setters deliberately do not use that path until the old writer can be replaced
atomically. Relay imports filter SSH secrets and device-inaccessible vault
records; Relay exports currently skip the whole SSH bundle so metadata cannot
overwrite Relay-only secrets. Canonical winners can also be projected directly
into a detached local `appConfig` without a Relay account key; scalar
tombstones、Profile env capability rules and SSH metadata-only application are
covered before one final config-store commit. Pre-join local customizations now
have a versioned, encrypted, write-once pending payload in `peerstore`; user
acceptance creates merge-only upserts and clears it only after durable append.
An un-wired Desktop anti-entropy receiver now binds verified batches to the
membership authenticated by the transport, rejects snapshot creators and tail
operation actors outside the current deny-wins member view, captures local
customizations before first snapshot adoption, persists snapshot/tail and
governance candidates before one terminal projection, refreshes accepted epoch
keys, and returns a separate transfer cursor plus durable vector. Exact snapshot
and completed-batch retries are idempotent. The receiver has no session/uplink
dependency, so config exchange cannot create a PTY subscriber. The remaining
transport work must supply the exact membership token already authenticated by
the Peer handshake; it must not accept a caller-selected token. The encrypted
Peer store now also retains a bounded, signature-verified grow-only membership
directory with serial-fork protection and v1-v4 derivation. Anti-entropy sends
missing membership tokens before snapshots and tail operations, so a device can
authorize sibling actors and exact multi-member rotation recipient sets without
a central directory. Current views choose one deterministic active grant per
peer and apply member/grant revocation deny-wins; expired and revoked grants do
not enter rotation recipients. The authenticated Pion record layer now carries
a separate config logical channel without renumbering the six Stage 1 record
kinds. Inventory、batch and durable ack use record kinds 7–9; kind 10 provides
a type-preserving 16 MiB bounded fragment format with reassembly state isolated
from terminal fragments. Pion routes config messages through an error-returning
config callback, so fragment、JSON or authorization failures close the attempt
instead of reaching the terminal callback. The Desktop adapter obtains the exact
remote membership token from the completed Peer handshake, drives inventory →
immutable plan → batch → durable ack, and retains exact batch bytes for lost-ack
retry. A multi-page loopback test over independent encrypted stores confirms
eventual projection and zero terminal subscriber lifecycle callbacks. This is
transport plumbing only: Relay-assisted direct still uses account authentication,
and no Quick Tunnel gateway or accountless route lifecycle is wired yet. Relay
compatibility hash/timestamp/record-ref state is persisted per Space and realm
with cross-process transactional updates. Canonical winners can now be
decrypted by exact epoch、grouped per legacy key and materialized back into
Relay scalar/ordered/sealed values, ready for adapter wiring.
Identity, route metadata and handshake authentication alone do not provide a
remote connection. PR 2.3 is now implemented locally: `internal/quicktunnel`
binds an ephemeral IPv4 loopback gateway、discovers only an explicitly supplied
or PATH-installed `cloudflared`、launches it with auto-update disabled、accepts
only a single-label `https://<label>.trycloudflare.com` endpoint, and owns the
30-second start / 5-second process-tree shutdown lifecycle. Desktop shutdown
owns the lifecycle through a narrow interface. Fake-process tests cover stdout
and stderr discovery、invalid endpoints、timeouts、forced termination and Unix
descendant cleanup; no public tunnel is opened by tests. The first independently
shippable PR 2.4 slice is now implemented locally: `GET /peer/v1/connect`
requires the `atterm-peer-v1` subprotocol、uses a bounded routing-only open
envelope、revalidates both signed memberships/session scope/effective
permission、and reuses the existing Peer membership handshake to derive
direction-separated encrypted records. SDP/ICE signals support an isolated
64 KiB fragment format and strict per-direction count/size limits; wrong
identity、scope、subprotocol、capacity and tampered ciphertext fail closed.
The second PR 2.4 slice is now implemented locally too: an authenticated signal
channel can drive the existing Pion host/client attempts、exchange encrypted
offer/answer、complete a separate membership handshake on the DataChannel、and
carry bidirectional terminal/control and fragmented config records. Signal and
DataChannel record kinds are mutually rejected, and closing either transport
reclaims the other attempt. The third PR 2.4 slice now mounts that handler in an
explicitly started Desktop Quick Tunnel host, resolves canonical active members
through the deny-wins Peer Space view, intersects both grants with the live
owner policy, and attaches the authenticated Pion channel to the requested
local Session. Initial replay、`DIRECT_READY`、driver claim、input/resize and
subscriber cleanup use the existing Session semantics without requiring a
Relay `account_key`. The same authenticated channel starts config anti-entropy
without creating another PTY subscriber; revoked membership or a live owner
permission downgrade closes the attempt. Desktop can sign first-join and member
reconnect ConnectionBundles for the current URL; route rotation changes only
the bundle/route, and revoked/consumed/expired invitations cannot be published.
Start remains explicit and restartable, while app shutdown owns cleanup. Real
loopback WebSocket + two-stage Peer handshake + Pion + Session tests cover the
path under the race detector. The fourth PR 2.4 slice now adds the
application-encrypted WSS data path on the same authenticated Quick Tunnel
socket. An explicit client `wss_fallback`
request and ordered host `wss_ready` acknowledgement prevent replay from racing
the mode switch. The route reuses the signaling handshake's exact membership
and directional record keys, then accepts only existing terminal/config record
kinds with the same isolated reassemblers. A bounded record scheduler enforces
control/input over terminal output over config sync while keeping fragments of
the same logical message contiguous within a priority. Desktop routes WSS and
Pion through the same Session、permission、driver and anti-entropy adapter, and a
real loopback test confirms replay/input/config plus exactly one terminal
subscriber. The first PR 2.5 slice is now implemented locally as well: Quick
Tunnel exposes an application-encrypted first-join endpoint, and Desktop can
preview a pasted token or fragment deep link, require an echoed genesis
fingerprint, redeem the invitation, verify the complete governance/rotation
bootstrap, persist opened epoch keys plus recipient-bound recovery envelopes,
and initialize the local Peer Space only after every required check succeeds.
The shared Settings join UI now accepts pasted tokens and fragment deep links,
shows the authenticated fingerprint/capabilities/route, and requires explicit
confirmation before redemption. It exposes QR scanning only when both a Peer
bridge and Capacitor camera capability are present. Web/iOS Peer bridge wiring
and the end-user fallback consent flow remain pending. Desktop Settings now
also exposes the explicit Quick Tunnel start/stop lifecycle and copies a
ticketless member reconnect bundle for the current route. Regenerating that
bundle after URL rotation does not mint or replace durable membership. The
Desktop host flow can now create a new Space, pre-sign constrained invitation
batches, list their lifecycle without rendering invitation tokens, publish an
open invitation through the current Quick Tunnel route, and revoke it. Host UI
tests cover creation, stable session-scope deduplication, first-join bundle
copying, token non-disclosure and revocation.

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
