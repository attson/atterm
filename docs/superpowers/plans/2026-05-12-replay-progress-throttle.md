# Replay Progress Throttle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show real history-loading progress and pace replay output when attaching to sessions with large scrollback.

**Architecture:** Add a relay-to-client `REPLAY_PROGRESS` protocol frame emitted by `internal/session` during subscribe replay. Relay client writers detect replay start/end and pace `OUT` writes. Desktop and web clients render a progress overlay from the new frame while preserving behavior with older relays.

**Tech Stack:** Go session/relay/proto packages, Vue 3 + TypeScript desktop frontend, vanilla JS web client, node:test, Vitest.

---

### Task 1: Protocol and session replay progress

**Files:**
- Modify: `internal/proto/frame.go`
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] Add a failing test that creates a session, pushes two scrollback chunks over the progress interval, subscribes from `since_seq=0`, and expects `REPLAY_PROGRESS start`, `OUT`, `OUT`, `REPLAY_PROGRESS chunk`, and `REPLAY_PROGRESS end` frames with correct byte totals.
- [ ] Run `go test ./internal/session -run TestSubscribeEmitsReplayProgress -count=1` and confirm it fails because `TypeReplayProgress` does not exist.
- [ ] Add `TypeReplayProgress = 0x13` and `ReplayProgressPayload` with `phase`, `bytes`, `total_bytes`, and `seq` JSON fields.
- [ ] Update `Session.Subscribe` to enqueue replay progress frames around scrollback replay and attach the subscriber to live fan-out after replay without dropping concurrent output.
- [ ] Run `go test ./internal/session -run TestSubscribeEmitsReplayProgress -count=1` and confirm it passes.

### Task 2: Relay replay pacing

**Files:**
- Create: `internal/relay/replay_pacer.go`
- Test: `internal/relay/replay_pacer_test.go`
- Modify: `internal/relay/client_conn.go`

- [ ] Add failing tests for a `replayPacer` helper: start frame enables pacing, `OUT` bytes trigger a pause at the threshold, end frame disables pacing.
- [ ] Run `go test ./internal/relay -run TestReplayPacer -count=1` and confirm it fails because the helper is missing.
- [ ] Implement `replayPacer.observe(frame)` and use it in the `/client` writer to sleep briefly after threshold-sized replay output batches.
- [ ] Run `go test ./internal/relay -run TestReplayPacer -count=1` and confirm it passes.

### Task 3: Desktop client progress UI

**Files:**
- Modify: `desktop/frontend/src/lib/proto.ts`
- Modify: `desktop/frontend/src/lib/connection.ts`
- Create: `desktop/frontend/src/lib/replayProgress.ts`
- Test: `desktop/frontend/src/lib/replayProgress.test.ts`
- Modify: `desktop/frontend/src/components/TerminalView.vue`

- [ ] Add failing Vitest cases for `formatReplayProgress`, covering percent and byte units.
- [ ] Run `cd desktop/frontend && npm run test -- replayProgress.test.ts` and confirm it fails because the module is missing.
- [ ] Add `TYPE.REPLAY_PROGRESS`, decode the JSON payload in `SessionConnection`, expose `onReplayProgress`, and render an overlay progress bar in `TerminalView.vue`.
- [ ] Run `cd desktop/frontend && npm run test -- replayProgress.test.ts` and confirm it passes.

### Task 4: Web client progress UI

**Files:**
- Modify: `web/app-core.js`
- Modify: `web/app-core.test.mjs`
- Modify: `web/app.js`
- Modify: `web/index.html`
- Modify: `web/style.css`

- [ ] Add failing node:test cases for web `formatReplayProgress`.
- [ ] Run `node --test web/app-core.test.mjs` and confirm it fails because the formatter is missing.
- [ ] Add `REPLAY_PROGRESS` handling, terminal progress markup, and CSS.
- [ ] Run `node --test web/app-core.test.mjs` and confirm it passes.

### Task 5: Docs and full verification

**Files:**
- Modify: `docs/spec/protocol.md`
- Modify: `AGENTS.md`

- [ ] Document the new `REPLAY_PROGRESS` frame and attach-loading behavior.
- [ ] Run `gofmt` on touched Go files.
- [ ] Run `go test -tags webkit2_41 ./...`.
- [ ] Run `go vet -tags webkit2_41 ./...`.
- [ ] Run `node --test web/*.test.mjs`.
- [ ] Run `cd desktop/frontend && npm run build && npm run test`.
