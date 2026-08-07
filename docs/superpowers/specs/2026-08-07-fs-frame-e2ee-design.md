# FS frame end-to-end encryption — design

Date: 2026-08-07
Status: Draft

## 1. Problem

`FS_REQUEST` (`0x38`) / `FS_RESPONSE` (`0x39`) / `FS_EVENT` (`0x3a`) carry
JSON plaintext through the relay: paths, file names, directory listings,
metadata, and whole file bodies. `docs/spec/protocol.md` §"Plaintext /
E2EE posture" documents this as a known posture and pre-reserves the AAD
bytes for a future sealed design.

The user-visible consequence is `fsaccess.go`'s deny list. `.env`,
`.env.local`, and `.env.example` are unreadable in the file explorer
because `isDenied` groups them with `.ssh` / `.gnupg` / `.aws`. Lifting
that deny without encrypting first would push secrets through the relay
in the clear. This design encrypts the FS frames, then lifts the deny
conditionally on encryption actually being in effect.

## 2. Goals

- File contents, paths, file names, directory listings, and agent-side
  error strings are unreadable by the relay.
- `.env*` becomes browsable wherever it cannot leak: always locally,
  and remotely only when sealing is active.
- No regression in transfer size relative to today's plaintext path.

## 3. Non-goals

- Hiding `op`, `request_id`, `ok`, or `watch_id`. The relay enforces its
  read-only allow-list on `op` (`isReadOnlyFSOperation`) and routes on
  `request_id` / `watch_id`. Encrypting these would remove a real
  security control and break routing.
- Changing the local file explorer path. `createLocalFSBridge` calls the
  Wails bindings directly and never produces a frame; it is untouched.
- Sealing `FS_REQUEST` bodies for writes. The relay already rejects every
  non-read-only op (`client_conn.go:273`), so no write payload reaches
  the wire.
- Backward compatibility with older clients. Per project convention the
  wire format is replaced outright; desktop and the iOS app must ship
  together.

## 4. Decisions

### 4.1 Segmented payload instead of a base64 `sealed` JSON field

The three existing sealed sites (`MetaPayload.Sealed`,
`SessionInfo.Sealed`, `CommandEventPayload.SealedBody`) base64 an
envelope into a JSON string field. That is right for their scale — tens
of bytes of metadata — but wrong here. `FileContent.Data []byte` is
already base64'd by `encoding/json` (1.33x today); nesting an envelope
inside a JSON string would base64 a second time, for 1.78x.

FS payloads therefore become a segmented binary structure. File bytes
travel raw inside the envelope, never through JSON, which brings the
expansion to ≈1.0x plus 41 bytes per envelope — better than today.

The cost is a standing obligation: `proto.FileContent` and
`proto.FSChunkPayload` no longer map directly onto the wire, so
`json.Marshal(payload)` is no longer valid for FS frames. Explicit
`EncodeFS*` / `DecodeFS*` functions own the mapping, and anyone adding a
field must decide which segment it belongs to.

### 4.2 Per-structure sealing, not per-field

`entries[].Name` and `entries[].Size` live in one struct, as do
`meta.Path` and `meta.ModTime`. Splitting them across a plaintext array
and a ciphertext array would create index-paired structures where a
mismatch corrupts data silently. Whole structs go into the envelope.
Metadata becomes invisible as a side effect, which costs nothing: the
relay reads none of these fields (`fs_router.go` touches only
`request_id`, `ok`, `watch_id`).

### 4.3 Relay-generated errors stay plaintext

`sendFSClientError` (`client_conn.go:355`) constructs responses for
permission denials and malformed requests. The relay has no key and
should not. These strings carry no agent-side paths. Only agent-produced
errors are sealed — those are the ones that leak full paths, e.g.
`plugin_fs: path denied by policy: /home/attson/proj/.env`.

### 4.4 Sealing failure is fail-closed

`protocol.md` §612 documents `no key = no encryption` for OUT/META: on a
seal error the agent falls back to plaintext. FS does **not** inherit
that fallback. Because the `.env` deny is lifted on the basis of "this
agent holds a key", a plaintext fallback after a seal error would put
secrets on the wire precisely when the deny is off. A seal error returns
an error response instead.

The `no key = no encryption` rule itself still applies: an agent that
never had a key sends single-segment plaintext, and the `.env` deny
stays on for that agent.

### 4.5 Key state is self-describing; no handshake

Segment count is the signal: one segment means plaintext, more means
sealed. The client seals if `getCurrentAccountKey()` returns a key; the
agent seals if it holds one. Neither side's behaviour depends on the
other's, so there is no negotiation to attack. In dev mode both ends are
keyless and behave exactly as today.

Downgrade analysis: the relay can strip a segment, yielding an empty
path or empty content and a failed request. That is denial of service,
which the relay could already achieve by dropping frames. The relay
cannot downgrade a sealed session to plaintext, because an agent's
decision to seal depends only on its own key state and on no inbound
field. This is why the `.env` gate keys off "sealing is in effect on
this agent", not "the client asked for encryption".

## 5. Wire format

Segment framing, identical for all three FS frame types:

```
payload := segment_count(1B) || segment*
segment := length(4B BE) || bytes
```

Segment 0 is always plaintext JSON. Later segments are AEAD envelopes
built with `e2eecrypto.SealUnsequenced`, AAD = `session_uuid(16B) ||
frame_type(1B)` where `frame_type` is the frame's own type byte, per red
line #22.

A keyed sender always emits the sealed segment, even when the struct it
carries is empty (an `unwatch_dir` response, say). Segment count is
therefore a statement about key state, not about whether this particular
response happened to carry data — that keeps §4.5's "segment count is
the key-state signal" unambiguous.

The one exception is the trailing content segment of `FS_RESPONSE`,
which is present only when `content` or `chunk` is. This tells the relay
whether a response carries file bytes, but `op` is plaintext anyway, so
`read_file` versus `list_dir` was already distinguishable; no new
information leaks.

Segment lengths are bounded by `proto.maxPayload` (16 MiB); a decoder
rejects a payload whose declared lengths do not sum to the remaining
bytes.

### FS_REQUEST `0x38` — client seals, agent opens

| Segment | Contents |
| --- | --- |
| 0 plaintext | `request_id`, `op`, `client_id`, `max_bytes`, `offset`, `length`, `watch_id` |
| 1 sealed | `SealedFSRequestFields { path, new_path }` |

### FS_RESPONSE `0x39` — agent seals, client opens

| Segment | Contents |
| --- | --- |
| 0 plaintext | `request_id`, `ok`, `watch_id` |
| 1 sealed | `SealedFSResponseFields { entries, meta, error, content_meta, chunk_meta }` |
| 2 sealed | raw file bytes (`content.data` or `chunk.data`), absent when neither applies |

`content_meta` is `FileContent` minus `Data`; `chunk_meta` is
`FSChunkPayload` minus `Data`. The decoder reattaches segment 2 to
whichever of the two is present.

### FS_EVENT `0x3a` — agent seals, client opens

| Segment | Contents |
| --- | --- |
| 0 plaintext | `watch_id`, `event` |
| 1 sealed | `SealedFSEventFields { path }` |

Per red line #23, every field moved into an envelope is zeroed in the
plaintext struct before encoding.

## 6. Mount points

| # | Direction | Site | Action |
| --- | --- | --- | --- |
| 1 | client → agent | `connection.ts:505 sendFSRequest()` | seal `path` / `new_path` |
| 2 | client → agent | `uplink.go:429 case TypeFSRequest` | open, overlay onto `FSRequestPayload` |
| 3 | agent → client | `remote_fs.go:291 handleRemoteFSRequest()` egress | seal; all three response branches converge here |
| 4 | agent → client | `uplink.go:294` (`remoteFS.events()`) | seal FS_EVENT — watcher-driven, bypasses #3 |
| 5 | agent → client | `connection.ts handleFSResponse / handleFSEvent` | open, overlay |

`handleRemoteFSRequest` needs a new `accountKey []byte` parameter; the
key currently lives only on `uplink.go`'s `u.accountKey`. Nothing below
`remote_fs.go` — `fsaccess.go` included — becomes aware of encryption.

Session keys come from the existing derivations: `DeriveSessionKey`
(`internal/e2eecrypto/sessionkey.go`) and `deriveSessionKey`
(`desktop/frontend/src/lib/opaque.ts:127`), same HKDF on both sides.

TS currently has only `open*` helpers — all existing E2EE is Go→TS. This
adds `sealUnsequenced` to `desktop/frontend/src/lib/opaque.ts`, mirroring
`envelope.go:112-126` line for line: `0x01` prefix, 24-byte random nonce,
XChaCha20-Poly1305 over the same AAD. `web/src` needs no change — it has
no file explorer.

## 7. Error handling

| Condition | Behaviour |
| --- | --- |
| Seal fails with a key present | Fail closed — error response, never plaintext (§4.4) |
| Open fails (AEAD auth) | Drop frame, surface a preview error to the user |
| Segment count or length prefix malformed | Treat as protocol error, drop frame |
| Sealed segment present, no key to open it | Error response / preview error |
| Single-segment frame received while holding a key | Accept — the peer is legitimately keyless |

## 8. `.env` policy

`fsAccess` gains a `denyEnv bool`. `isDenied`'s `.env` rule becomes
conditional on it; `.ssh` / `.gnupg` / `.aws` remain unconditional on
both paths.

| Constructor | `denyEnv` | Rationale |
| --- | --- | --- |
| `NewPluginFS()` (`plugin_fs.go:100`) | `false` | Wails direct call, never crosses the relay |
| `newRemoteFS(newFSAccess(...))` (`uplink.go:196`) | `!accountKeyAvailable` | Remote reads are only safe once sealing is in effect |

## 9. Testing

Go:

- Segment codec round-trip, plus boundaries: zero segments, empty
  segment, truncated length prefix, count/actual mismatch, oversized
  segment against `maxPayload`.
- Seal/open round-trip per frame type, and AAD binding — an envelope
  sealed under `0x39` must fail to open under `0x38`.
- `handleRemoteFSRequest` emits sealed responses with a key present and
  single-segment plaintext without one; seal failure produces an error
  response and never plaintext.
- `fsaccess` deny matrix: `.env` / `.env.local` / `.env.example` allowed
  when `denyEnv=false`, denied when `true`; `.ssh` denied in both.
- Relay routes sealed responses unchanged — `fs_router` reads only
  segment 0.

TS:

- `sealUnsequenced` / `openUnsequenced` round-trip.
- Segment decode against the same boundary cases as Go.
- `remoteSessionFS` reassembles `content.data` from segment 2.

Cross-language vectors — the highest-value tests, since the two
implementations are independent:

- A Go-sealed envelope, checked into the TS test as a fixture, opens in
  TS to the expected plaintext.
- A TS-sealed envelope, checked into the Go test as a fixture, opens in
  Go.

## 10. Risks

- **Breaking wire change.** Desktop and the iOS app must ship together;
  a stale app sees malformed FS payloads. Accepted per §3.
- **Two sealed shapes in the codebase.** FS uses segments, the other
  three sites use base64-in-JSON. Deliberate (§4.1) and documented in
  `protocol.md`.
- **New TS crypto surface.** First code that encrypts on the client. The
  cross-language vectors in §9 are the primary control.

## 11. Documentation obligations

- `docs/spec/protocol.md`: add `0x38` / `0x39` / `0x3a` rows to the
  §E2EE 信封 AAD table (red line #22), replace the §"Plaintext / E2EE
  posture" paragraph, document the segmented format.
- `AGENTS.md` §22: extend the allocated-byte list.
