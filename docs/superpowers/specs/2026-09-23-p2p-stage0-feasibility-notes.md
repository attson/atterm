# P2P Stage 0 Feasibility Notes

> Status: Accepted inputs for v0.6 implementation
> Date: 2026-09-23
> Parent: [Relay-assisted P2P acceleration](./2026-09-22-relay-p2p-acceleration-design.md)

## 1. Pion and DataChannel

Decision: **GO**, with Pion pinned to `github.com/pion/webrtc/v4 v4.1.8` while the repository stays on Go 1.23.

- Pion v4.2.20 requires Go 1.24 and cannot be adopted without changing the repository toolchain contract.
- v4.1.8 declares Go 1.21 and interoperates with the current Go 1.23 build.
- The spike is test-only. No current app startup path creates a PeerConnection.
- Channel contract is label `atterm-terminal-v1`, ordered, reliable, binary.
- Frame plaintext fragments stay at or below 16 KiB; one reassembly is capped at 16 MiB and 10 seconds.
- Backpressure high/low watermarks remain 1 MiB / 256 KiB for the Stage 1 adapters.

Measured on 2026-09-23 in the Linux development environment:

| Pair | Payload | Fragments | Result |
|---|---:|---:|---|
| Pion ↔ Pion, Go race test | 10 MiB | 641 | pass, setup + transfer ~5.68 s |
| Chromium 140 → Pion | 10 MiB | 641 | pass, setup + transfer ~1.14 s |
| TypeScript framing in jsdom | 10 MiB | 641 | pass, reassembly test ~0.2 s |
| WebKit → Pion | 10 MiB | 641 | not measured; no automatable WebKit browser in this environment |
| Firefox → Pion | 10 MiB | 641 | not measured; Firefox not installed in this environment |

The optional Chromium test auto-detects `google-chrome`/`chromium`; it skips cleanly where no browser is installed. WebKit remains an explicit Stage 0 exit gate and must be run in the packaged Linux/macOS/iOS matrix before direct transport is wired into a default-on build.

## 2. Route Handover

Decision: **GO** for the state model; integration remains Stage 1.

Go and TypeScript now share the same states and invariants:

```text
relay_attached -> direct_connecting -> direct_replay -> direct_active
       ^                                      |
       +---------- relay_reattaching <--------+
```

- One generation invalidates late callbacks after route loss.
- Relay remains the sole input writer during connect/replay.
- Input is frozen while Relay fallback attaches.
- OUT is committed once by monotonic session sequence even during route overlap.
- `DIRECT_READY.replayed_seq` must equal the client's committed cursor before switching input to direct.

Both implementations pass 100 forced Direct/Relay transitions without duplicate committed OUT or a second input route. The Stage 1 integration still has to prove the same property with real `session.Subscribe`, `/client` attach and `SetSubscriberLifecycle` callbacks.

## 3. Quick Tunnel

Decision: **GO for v0.7**, conditional on explicit executable supply and verification.

- Quick Tunnel can expose one loopback HTTP/WebSocket peer gateway without introducing a platform account database.
- It is reachability only. Peer membership proof remains the authorization source.
- WebRTC signaling uses the tunnel first; application-encrypted WSS is an optional fallback over the same record codec.
- `cloudflared` is not installed in the current development environment, so process output parsing and URL lifetime were not measured in Stage 0.
- The app must never silently download an unverified `cloudflared`. Stage 2 must choose one of: user-provided executable, separately signed bundled artifact, or explicit package-manager prerequisite.
- A `trycloudflare.com` URL is ephemeral route metadata. Rotation must not revoke or recreate Peer membership.

No Quick Tunnel code enters v0.6; this decision cannot delay Relay acceleration.

## 4. Peer Identity Storage

Decision: **GO for v0.7** with platform-specific non-exportable storage where available.

- Desktop: P-256 private identity sealed in OS keyring, with the existing guarded fallback posture documented separately.
- iOS: Keychain through the existing Capacitor secure-storage registration pattern.
- Web/PWA: non-exportable WebCrypto `CryptoKey` in IndexedDB; membership export requires a separately designed encrypted transfer, never raw automatic export.
- The identity is distinct from Relay `account_key`; neither derives the other.

An invitation may bootstrap membership, but every transport handshake verifies the resulting signed membership/capability chain. A temporary route URL is never an identity.

## 5. Rendezvous

Decision: **GO for v0.8** as a separate stateless binary and protocol contract.

- TLS/WSS presence and opaque-topic signaling only.
- No Relay account login, terminal forwarding, config database or TURN behavior.
- Restart may lose presence/mailbox entries but never membership or configuration truth.
- Official and self-hosted deployments must run the same black-box contract suite.
- The Stage 1 signaling message envelope can be reused, but Relay authorization claims are replaced by Peer membership proof before any direct transport is accepted.

Rendezvous is deliberately after Quick Tunnel so stable discovery does not become a hidden prerequisite for accountless trust.

## 6. Decentralized Config Replication

Decision: **GO for v0.7**, with a signed op log rather than multi-master writes to the existing Relay preference rows.

- Each device owns a monotonic counter and signs immutable ops.
- Version vectors drive anti-entropy; HLC/LWW handles concurrent scalar updates.
- Record collections use remove-wins tombstones and stable positions, avoiding whole-array LWW for profiles/templates/SSH hosts.
- Membership and sync/vault epoch keys gate replicas; Relay preference sync becomes a compatibility adapter, not a competing writer.
- No always-online device is required, so offline Spaces visibly retain pending changes until any two members meet.

Before Stage 2 production wiring, randomized three-device partition tests must pass at least 10,000 seeds, including revoke/epoch-rotation and snapshot compaction cases.

## 7. Remaining Stage 0 Gate

Only the packaged browser matrix remains unproven:

- WebKit/WKWebView ↔ Pion ordered/reliable DataChannel.
- Firefox ↔ Pion message-size and backpressure behavior.
- Network switch / ICE restart teardown on packaged desktop and iOS targets.

These measurements may change adapter details and timeouts, but not the accepted transcript, proof, record, fragment or route-generation formats.
