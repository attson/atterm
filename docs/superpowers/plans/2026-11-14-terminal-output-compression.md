# Terminal Output Compression Implementation Plan

> Execute inline using superpowers:executing-plans; validate each task before advancing.

**Goal:** Reduce remote terminal bytes using compatible, bounded DEFLATE before E2EE.

**Spec:** `docs/superpowers/specs/2026-11-14-terminal-output-compression-design.md`

**Architecture:** One authoritative session with independently subscribed legacy and compressed output buffers; shared permissions and metadata. Add three frame types and optional JSON capability fields, preserving version 1 and existing layouts.

**Global constraints:** No new npm dependencies, no relay access to plaintext, no changes to local PTY sizing/input or background tab policy. Unique AAD 0x43; update protocol docs and AGENTS allocation. Keep old clients usable concurrently.

## 1. Codec and protocol

- [x] Add failing codec tests for exact round trips, small/incompressible fallback, malformed lengths, corrupt DEFLATE and the 64 KiB limit; run them before implementing `internal/terminalcodec.Encode/Decode`.
- [x] Implement bounded compressor reuse and independent raw DEFLATE framing. Add proto constants/capability fields, AAD registration and protocol documentation; run codec/proto/crypto tests.

## 2. Session and relay

- [x] Test mixed subscribers, independent lazy hooks, typed opaque replay and cached cursors before changing Session.
- [x] Add lazy compressed buffer and output subscription option without splitting driver/permissions state. Preserve encrypted frame boundaries in replay and reconnect opaque overflows.
- [x] Wire ANNOUNCE/ATTACH capability selection, compressed stream lifecycle, OUT_COMPRESSED storage, pacing and traffic classification. Test legacy publisher/client fallbacks and mixed viewers.

## 3. Publisher

- [x] Test compressed sealing/AAD and canonical source frame boundaries across routes.
- [x] Track uplink streams by session plus format; advertise capability only with key, compress before sealing and reuse the codec pool. Resume from requested seq and retain STOP behavior; preserve source frame boundaries for direct/proxy subscriptions.

## 4. Shared frontend

- [x] Test bounded native decode, encrypted compressed vectors, ordered async output/replay handling and stale socket cancellation.
- [x] Add capability detection, typed OUT authentication and ordered decoding; preserve existing synchronous ordinary OUT/direct behavior wherever possible.

## 5. Verification

- [x] Run focused tests after each task; run Go vet, relevant Go tests, desktop frontend build/tests, web build/tests/contracts and pinned web embed build.
- [x] Inspect final diff, verify unrelated recovery test changes are untouched, and document measured benefit plus any untested target platforms.

Validation completed in the dedicated `feat/terminal-compression` worktree:

- Go 1.23.12: full `go test -tags webkit2_41 -timeout 240s ./...` and `go vet -tags webkit2_41 ./...` passed. The desktop suite took 52.321 seconds; the initial 60-second run under parallel build/load timed out, so final validation used CI's 240-second budget. macOS linking used SDKROOT MacOSX15.4 to avoid local macOS27 SDK linker incompatibility.
- Full session and focused publisher/lifecycle race tests passed; regressions cover overflow recovery, independent lazy lanes, driver handover and stale metadata. Final scoped review has no remaining findings.
- Desktop frontend: 221 test files / 2804 tests passed, typecheck and production build passed. New compression/opaque tests also passed on pinned Node20 (25 tests).
- Web: 238 tests passed, 1 external-relay OPAQUE interoperability test skipped; pinned Node20/Go1.23.12 embed build and 16 contract tests passed.
- `git diff --check` passed. Unrelated original-workspace recovery test and concurrent AI binding documents were preserved.
- CPU and savings figures come from synthetic local microbenchmarks, not live-session or Safari/iOS measurements. Direct transport continues ordinary encrypted output; relay compression requires capable publisher and subscriber.

Reviewable changes remain uncommitted in the requested worktree. No merge, push or release was performed.
