# Relay-assisted P2P Acceleration Design

> Status: Accepted for v0.6 implementation
> Date: 2026-09-22
> Parent plan: [Accountless P2P](../plans/2026-09-22-accountless-p2p.md)
> Release boundary: v0.6.x only; this document does not define Peer Space or accountless access

## 1. Outcome

已登录同一 Relay 账户的客户端仍先通过现有 `/client` attach。随后它可以经新的 Relay signaling 通道与 owning desktop 建立 WebRTC DataChannel，把 terminal data path 升级为直连。登录、session list、权限、driver arbitration、presence 和 fallback 仍由 Relay 管理。

直连失败、超时或中途断开时，客户端从最后 committed OUT sequence 重新 attach Relay。关闭 feature flag 后，运行行为必须与 v0.5 Relay-only 完全一致。

本阶段不创建 Peer identity，不签发 accountless invitation，不引入 TURN，也不改变 `internal/proto.Version = 1`。

## 2. Trust Boundary

一次 direct attempt 同时需要两类授权：

1. Relay authorization：Relay 已验证 user、session、permission、active uplink 和 client connection，并通过已鉴权 signaling WS 向双方下发同一份短期 pending authorization。
2. End-to-end authentication：双方用 `account_key` 派生的 proof key 对包含 ticket、claims 和临时 P-256 公钥的 transcript 做角色分离 HMAC。

Relay 知道 ticket、claims、SDP/ICE timing 和连接元数据，但不知道 `account_key`、ECDH private key、proof key 或 record key。因此 Relay 可以拒绝/延迟/重放 signaling，但无法伪造能通过另一端校验的 direct handshake。

`account_key` 不发送给 Relay、DataChannel 或 native bridge。现有 key owner 只输出本次握手的 proof 与方向密钥；这些派生值在 attempt 结束时清除。

## 3. Signaling Surface

### 3.1 Endpoint and authentication

- 新增 `GET /direct-signal` WebSocket；不复用 terminal `/client`、`/uplink` frame stream。
- Browser 使用现有 `Sec-WebSocket-Protocol: atterm-token.<token>`，native client 使用现有 Bearer/session auth 规则。
- Origin、IP rate limit、per-user connection limit 和 max message size 使用 Relay 现有安全策略。
- signaling schema 有独立 `version: 1`，不占用 `proto.Type`，也不改变 terminal frame payload。
- 单条 JSON message 上限 64 KiB；未知字段忽略，未知 message kind 拒绝当前 attempt 而非断开账户会话。

### 3.2 Attempt creation

Client sends:

```json
{
  "version": 1,
  "kind": "direct_request",
  "request_id": "client-generated UUID",
  "session_id": "UUID",
  "client_instance_id": "random install/boot scoped id",
  "since_seq": 42
}
```

Relay verifies the authenticated user owns the requested session, the requested permission is still available, and the owning uplink has advertised direct capability. It then creates:

- `attempt_id`: random UUID, used for routing and log correlation.
- `ticket`: 32 random bytes, base64url without padding.
- exact claims: `user_id`, `session_id`, `host_id`, `client_instance_id`, effective permission, `expires_at_unix_ms`.
- expiry: 30 seconds from issue.

The ticket is an opaque single-use capability, not a self-contained signed token. Both endpoints receive it only over their authenticated Relay signaling connections. The host stores a hash of the ticket with the exact claims in a bounded pending-attempt table. A DataChannel handshake is accepted only if it matches that pending entry; successful authentication, expiry, explicit cancel, or connection close consumes it atomically.

This avoids adding a second persistent Relay signing identity. It does not weaken end-to-end authentication because possession of the ticket alone is insufficient without the account-key proof.

### 3.3 Routed messages

After `direct_offer`, Relay only routes bounded opaque signaling payloads associated with the attempt:

- `offer`, `answer`: one each, max 32 KiB.
- `ice_candidate`: max 8 KiB each, max 64 per side.
- `ice_end`: one per side.
- `cancel`: terminal for the attempt.

Relay never parses SDP to make authorization decisions and never logs SDP, ICE candidates, ticket bytes, proof bytes or DataChannel payloads. Diagnostics may retain attempt duration, coarse candidate type, terminal result and a random attempt id for at most the normal metrics retention period.

### 3.4 ICE discovery and privacy

Stage 1 uses `stun:stun.cloudflare.com:3478` for server-reflexive ICE candidate discovery and does not provide TURN. STUN is not a tunnel or terminal-data relay: it observes the connecting endpoint's source IP and request timing, while terminal records flow directly between the two WebRTC peers. Direct peers necessarily learn the candidate addresses needed to connect, and the authenticated atterm Relay routes those candidate strings in memory without logging them. If public STUN or peer-to-peer UDP is unavailable, the attempt fails into the existing Relay path; terminal availability does not depend on STUN.

## 4. Pluggable Handshake Authentication

Transport depends on this narrow contract:

```go
type HandshakeAuthenticator interface {
    BuildProof(transcript []byte, role Role) ([]byte, error)
    VerifyProof(transcript []byte, role Role, proof []byte) error
    KeyBinding(transcript []byte) ([]byte, error)
}
```

The v0.6 implementation is `AccountKeyAuthenticator`. Stage 2 adds `PeerMembershipAuthenticator` without changing P-256 ECDH, record framing, fragmentation or route handover.

The raw authentication secret must remain inside its current owner. Implementations may expose a derived per-attempt proof/key binding to the transport, never the root `account_key` or Peer epoch key.

## 5. Canonical Transcript and Handshake

### 5.1 Canonical transcript

All integers are unsigned big-endian. All variable fields are `u16 length || bytes`; values over their declared bounds are rejected before allocation.

```text
literal "atterm-direct-handshake-v1"  (26 bytes)
attempt_id                            (16-byte UUID)
ticket                               (u16 + 32 bytes)
session_id                            (16-byte UUID)
user_id                               (u16 + UTF-8, max 128)
host_id                               (u16 + UTF-8, max 128)
client_instance_id                    (u16 + UTF-8, max 128)
permission                            (u8: view=1, control=2, full=3)
expires_at_unix_ms                    (u64)
client_ephemeral_public               (u16 + 65-byte uncompressed P-256 point)
host_ephemeral_public                 (u16 + 65-byte uncompressed P-256 point)
```

Both endpoints reconstruct these bytes from their locally validated pending authorization and exchanged public keys. Any mismatch produces a different proof.

### 5.2 Key schedule

```text
transcript_hash = SHA-256(transcript)
proof_key = HKDF-SHA256(
  ikm = account_key,
  salt = transcript_hash,
  info = "atterm-direct-proof-v1",
  len = 32)

client_proof = HMAC-SHA256(proof_key,
  "atterm-direct-client-v1" || transcript_hash)
host_proof = HMAC-SHA256(proof_key,
  "atterm-direct-host-v1" || transcript_hash)

shared_secret = P-256 ECDH(local_private, peer_public)
traffic_secret = HKDF-SHA256(
  ikm = shared_secret,
  salt = proof_key,
  info = "atterm-direct-traffic-v1" || transcript_hash,
  len = 96)

traffic_secret[0:32]  = client_to_host_key
traffic_secret[32:64] = host_to_client_key
traffic_secret[64:80] = client_to_host_nonce_prefix
traffic_secret[80:96] = host_to_client_nonce_prefix
```

The transcript is authenticated before either endpoint accepts application records. P-256 public points must pass the runtime's curve validation; all-zero or malformed shared secrets are rejected.

### 5.3 Message order

The ordered reliable DataChannel label is `atterm-terminal-v1`, negotiated in-band, binary only.

```text
client -> host  CLIENT_HELLO(attempt_id, ticket, client_pub)
host   -> client HOST_HELLO(host_pub, host_proof)
client -> host  CLIENT_FINISH(client_proof, HMAC(proof_key, "atterm-direct-finish-v1" || transcript_hash))
host   -> client AUTH_OK(initial host record counter = 0)
```

`client_proof` 不能放进 `CLIENT_HELLO`：canonical transcript 包含 `host_pub`，client 在收到 `HOST_HELLO` 前无法构造该 proof。Host 先用已鉴权 signaling 下发的 pending authorization 校验 `attempt_id + ticket`，收到 `client_pub` 后才能构造完整 transcript 并证明 host role；client 验证 host proof 后在 `CLIENT_FINISH` 同时证明 client role和确认完整 transcript。Host 不在 `CLIENT_FINISH` 验证通过前发送 terminal data。任何 parse、expiry、claim、proof 或 state mismatch 都关闭 DataChannel 并消费 attempt。Handshake timeout 是 DataChannel open 后 10 秒。

握手消息全部是 binary DataChannel message，固定布局如下；`version=1`，kind 分别为 `1..4`，不允许 trailing bytes：

```text
CLIENT_HELLO  = version(1B) || kind=1(1B) || attempt_id(16B) || ticket(32B) || client_pub(65B)
HOST_HELLO    = version(1B) || kind=2(1B) || host_pub(65B) || host_proof(32B)
CLIENT_FINISH = version(1B) || kind=3(1B) || client_proof(32B) || finish_proof(32B)
AUTH_OK       = version(1B) || kind=4(1B)
```

P-256 public key 使用 SEC1 uncompressed point（`0x04 || X(32B) || Y(32B)`）。任一 text message、长度错误、曲线点错误、顺序错误或重复握手消息都关闭当前 direct attempt。

## 6. Encrypted Record Layer

All post-handshake DataChannel messages contain exactly one record:

```text
version       u8   = 1
kind          u8
counter       u64 big-endian
plain_len     u32 big-endian
ciphertext    plain_len + 16 byte Poly1305 tag
```

Nonce is `direction_nonce_prefix(16) || counter_be64`. AAD is:

```text
transcript_hash(32) || version(1) || kind(1) || counter(8) || plain_len(4)
```

Cipher is XChaCha20-Poly1305. Counters start at zero independently in each direction and must equal the receiver's next expected counter. Ordered reliable DataChannel makes a replay window unnecessary; duplicate, gap or wrap closes the direct route and triggers Relay fallback. A reconnect always creates fresh ECDH keys, nonce prefixes and counters.

Record kinds:

| Kind | Value | Payload |
|---|---:|---|
| FRAME | 1 | Existing `proto.Marshal(Frame)` bytes |
| FRAGMENT | 2 | Fragment header + a slice of one marshaled frame |
| DIRECT_READY | 3 | `last_replayed_out_seq` u64 |
| PING | 4 | opaque 8 bytes |
| PONG | 5 | echoed 8 bytes |
| CLOSE | 6 | bounded code/reason |

Unknown kinds are deny-by-default. They do not become permission-bearing merely because a newer terminal `proto.Type` exists.

### 6.1 Fragmentation and limits

- A record plaintext is at most 16 KiB.
- A marshaled terminal frame is at most 16 MiB.
- FRAGMENT header is `message_id u64 || offset u32 || total_len u32`.
- Offset must be contiguous, one reassembly per direction, with a 10-second deadline.
- Sender backpressure starts when `bufferedAmount > 1 MiB` and resumes below 256 KiB.
- Limit or ordering violations close only the direct route; the Relay account connection stays alive.

## 7. Route Handover

The client owns one route generation and one committed OUT cursor per `session_id`:

```text
relay_attached -> direct_connecting -> direct_replay -> direct_active
       ^                                      |
       +------------- fallback ---------------+
```

1. Client attaches via Relay with existing semantics and records the greatest OUT seq delivered to xterm as `committed_seq`.
2. Direct authentication succeeds. Host subscribes to the same local `session.Session` with `since_seq = committed_seq`.
3. During direct replay, Relay remains subscribed. Frames from both routes are accepted only for the current generation and deduplicated by OUT seq before delivery.
4. Host sends `DIRECT_READY(last_replayed_out_seq)` only after replay catch-up and live handoff are ordered on the direct subscriber.
5. Client validates `DIRECT_READY`, switches the route generation, then closes its old `/client` terminal socket so Relay removes that subscriber. The account, signaling, and session-list connections remain. The signaling attempt itself was already atomically removed when host sent `consumed` after account-key authentication, so readiness is not acknowledged against that expired attempt.
6. Direct failure freezes IN/RESIZE/CLAIM_DRIVER, increments route generation, and reattaches Relay using the last `committed_seq`. Input resumes only after Relay attach/replay completes.

Late frames from an older route generation are dropped. OUT may arrive twice at the transport boundary during overlap but is delivered to xterm once. IN, RESIZE, paste and driver claim have exactly one active writer route and are never mirrored during transition.

## 8. Driver, Permission and Lazy Streaming

- Relay checks effective permission before ticket issue; desktop host checks permission again before local PTY or filesystem access.
- View permission permits OUT/META/replay only. Control permits IN/RESIZE/driver operations. `full` does not implicitly enable file, preview or future frame types on direct transport.
- Direct route uses the same client principal and driver lease as the preceding Relay attach. Route change is not a new claim.
- The desktop treats Relay and direct overlap as one logical remote subscriber. Transition must never create a logical N→0→N edge.
- The physical direct subscriber is registered before Relay subscription release. `SetSubscriberLifecycle` therefore preserves 0→1/N→0 lazy upload semantics.
- Config sync and signaling are not terminal subscribers and cannot trigger `STREAM_REQUEST`.

## 9. Failure Codes and Fallback

Stable local error categories:

| Code | Meaning | Retry |
|---|---|---|
| `direct_disabled` | client, host or Relay kill switch off | no |
| `direct_unsupported` | peer version/platform lacks capability | no |
| `direct_unauthorized` | account/session/permission mismatch | after auth change |
| `ticket_expired` | pending authorization expired | new attempt |
| `auth_failed` | proof/transcript mismatch | no automatic loop |
| `ice_failed` | no viable candidate pair | after cooldown/network change |
| `handshake_timeout` | channel opened but auth did not finish | one new attempt |
| `record_invalid` | AEAD/counter/limit violation | Relay fallback; no immediate direct retry |
| `route_lost` | active channel closed | Relay fallback, cooldown |

Fallback is expected behavior, not a session error. UI remains connected and changes the route indicator to Relay. Authentication failures are security events and use a longer cooldown than ordinary ICE failures.

## 10. Rollout and Compatibility

- v0.6.0 beta: client preference default off and Relay server kill switch default off unless explicitly enabled.
- Capability negotiation happens on signaling connection; old Relay/clients never see direct messages.
- Relay-only `/client` and `/uplink` behavior is unchanged.
- Existing sealed `TypeOut` envelopes remain intact inside the encrypted record. Nested encryption is deliberate: Relay fallback and direct transport share one frame representation.
- Direct diagnostics redact IP addresses to candidate class (`host`, `srflx`, `relay`), never include ticket/proof/key/SDP/ICE strings, and never log terminal payloads.
- Default-on requires 100 route-flap runs without duplicate IN or missing/duplicate OUT, plus packaged Chromium/WebKit/iOS NAT matrix soak.

## 11. Implementation Boundaries

- `internal/peertransport`: canonical transcript, authenticator interface, key schedule, record codec, fragmentation and Pion adapter.
- `internal/relay`: authenticated signaling broker and bounded pending attempt registry; no terminal frame parsing on this endpoint.
- `desktop/uplink.go`: owns host orchestration, local session lookup, permission recheck, logical subscriber and route lease.
- `desktop/frontend/src/lib`: shared browser signaling/DataChannel transport used by Wails, Web and Capacitor.
- Existing `internal/proto` frame encoding remains unchanged. Production signaling wire details must be mirrored into `docs/spec/protocol.md` when Stage 1 is connected.

## 12. Acceptance Tests

- Go/browser golden vectors cover transcript bytes, P-256 ECDH, both proofs, directional keys/nonces and records.
- Wrong account key, session, ticket, role, public key, expiry and mutated transcript all fail authentication.
- Record replay, gap, wrong direction, wrong kind/length and tamper fail closed.
- 10 MiB replay under concurrent input respects bounds and reaches one final `committed_seq`.
- Forced failures at each handover edge fall back through the existing Relay path.
- Race tests prove one input writer, one driver lease and no lazy-stream zero edge during overlap.
