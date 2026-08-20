# Sync visibility — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make preference sync visible and steerable: a status indicator, a notice when an item changed on another device, and a manual sync trigger.

**Architecture:** Serialise every `prefssync.Engine` call through one owning goroutine fed by a request channel — the serial goroutine the engine's own doc comment already claims exists but which was never built. `Pull` gains a result describing what it adopted and where it conflicted. Status is derived from the queue and the last outcome, and pushed to the frontend as events.

**Tech Stack:** Go 1.23.12, Wails v2.12.0, Vue 3, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-20-sync-visibility-design.md`

## Global Constraints

- **Redline #5:** `internal/` must not import `desktop/`. `prefssync` stays desktop-free.
- **Redline #4:** no changes to the payload structure of any existing protocol frame. This item adds none.
- **Redline #2:** do not touch `SetSubscriberLifecycle` or the uplink subscriber count.
- Go: pinned toolchain **go1.23.12** (`GO_VERSION` in `.github/workflows/build.yml`). Node: **20**. `gofmt` is now gated in CI — run `gofmt -w` on every file you touch.
- Any change under `desktop/frontend/src` or `web/src` requires rebuilding `internal/relay/web-dist/` via `./scripts/build-web.sh`, or CI's drift gate fails.
- i18n: `en.ts` and `zh-CN.ts` key sets must match exactly — enforced by `satisfies Messages`, so a one-sided key is a compile error.
- **Shared components ship to web and iOS.** `web/vite.config.ts` aliases the web build's `@` to `desktop/frontend/src` and Capacitor mounts the same shell. Anything desktop-only must be gated on `platform.caps.wailsBindings` AND have a test that mounts it under both shapes. This bit us in item 29; do not repeat it.

---

### Task 1: The serial sync loop

**Files:**
- Create: `desktop/prefs_sync_loop.go`
- Test: `desktop/prefs_sync_loop_test.go`
- Modify: `desktop/app.go` (the `App` struct; the `markPrefDirtyAndPush` at ~line 1785; the startup `Pull` at ~line 465), `desktop/app_relay.go` (~lines 98-130), `desktop/prefs_watch.go` (~line 82)

**Why this task exists (read before deciding it is scope creep):** `internal/prefssync/sync.go` says at the `Engine` type "NOT safe for concurrent calls — wire it into a serial goroutine via the desktop boot code." No such goroutine exists. `grep -rn "prefsSyncMu\|syncMu" desktop/*.go` returns nothing, and the engine is called from four places, one of which (`markPrefDirtyAndPush`) spawns `go Push()` **on every preference setter call**. `Push` is a check-then-act across a network round trip its own comment says "can take seconds". Item 30 adds a user-facing button that triggers the same path, so this is the prerequisite, not a side quest.

**Interfaces:**
- Consumes: `a.prefsSync *prefssync.Engine`, `a.ctx`.
- Produces:
  ```go
  // syncRequest is one queued unit of work for the serial loop.
  type syncRequest struct {
      pull bool
      push bool
  }

  func (a *App) startPrefsSyncLoop()          // called once, from the same place the engine is constructed
  func (a *App) enqueueSync(req syncRequest)  // non-blocking; coalesces
  ```

**Behaviour the tests must pin:**
- Two concurrent `enqueueSync` calls never produce two concurrent engine calls. Assert with a counter recording the observed maximum in-flight count, not with timing.
- Enqueuing while a sync is in flight **coalesces**: a second request does not queue a second round trip. Pin the count of engine calls, not just the absence of an error.
- A panicking or erroring engine call does not kill the loop — the next request is still served.
- The loop exits when `a.ctx` is cancelled, and `enqueueSync` after that does not block or panic.
- Every existing caller (`markPrefDirtyAndPush`, the startup pull, the post-login pull/seed/push, the watcher) goes through the loop. Verify with a grep assertion in the test or a review note: no `a.prefsSync.` call outside `prefs_sync_loop.go`.

- [ ] **Step 1: Write the failing tests**

Substitute a fake engine. The real `*prefssync.Engine` is a concrete struct, so introduce a small interface in `desktop/` that it satisfies — do NOT change `internal/prefssync` for testability alone:
```go
type prefsSyncEngine interface {
    Pull(ctx context.Context) (prefssync.PullResult, error)  // after Task 2; use `error` only until then
    Push(ctx context.Context) error
    MarkDirty(key string, updatedAtLocalMs int64)
    SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64)
}
```
Order matters: implement Task 1 against today's `Pull(ctx) error`, and let Task 2 widen it.

```go
func TestSyncLoopSerialisesConcurrentRequests(t *testing.T)
func TestSyncLoopCoalescesWhileInFlight(t *testing.T)
func TestSyncLoopSurvivesAnEngineError(t *testing.T)
func TestSyncLoopStopsOnContextCancel(t *testing.T)
func TestEnqueueAfterShutdownDoesNotBlock(t *testing.T)
```

- [ ] **Step 2: Run them, watch them fail**
- [ ] **Step 3: Implement the loop**

One goroutine, one buffered request channel of size 1, coalescing by OR-ing the flags of a pending request rather than queueing a second. A channel rather than a mutex because the queue depth is what the status indicator reports as "changes waiting" — a mutex would hide it.

- [ ] **Step 4: Rewire all four call sites**

`markPrefDirtyAndPush` keeps calling `MarkDirty` synchronously (it is a local write and ordering matters), then enqueues a push instead of spawning a goroutine.

- [ ] **Step 5: Run tests + `go test ./desktop/ -race -count=5`**
- [ ] **Step 6: Mutation check**

Replace the serialisation with a direct call and confirm `TestSyncLoopSerialisesConcurrentRequests` fails. Remove the coalescing and confirm `TestSyncLoopCoalescesWhileInFlight` fails. Report the exact output. A mutation the compiler catches proves nothing.

- [ ] **Step 7: Commit**

---

### Task 2: `Pull` reports what it did

**Files:**
- Modify: `internal/prefssync/sync.go`
- Test: `internal/prefssync/sync_test.go`

**Interfaces:**
- Produces:
  ```go
  type PullResult struct {
      Adopted  []string // server value taken; local had no competing edit
      Conflict []string // server was newer BUT local was dirty; local kept
  }
  func (e *Engine) Pull(ctx context.Context) (PullResult, error)
  ```

**Behaviour the tests must pin:**
- A key adopted from the server appears in `Adopted` and nowhere else.
- A key where the server is newer but local is `Dirty` appears in `Conflict`, and its local value is unchanged. This branch is currently a bare `continue` — the user is never told two devices disagreed and that a timestamp picked the winner.
- A key the server does not have appears in neither.
- A key whose `WriteValue` fails is in neither, and the loop still processes later keys — this is the log-and-continue behaviour added after the key count went 8 → 17; do not regress it.
- Both slices are deterministic in order (sort them) so a test can compare without set gymnastics.

- [ ] **Step 1: Write the failing tests**
- [ ] **Step 2: Run, watch fail**
- [ ] **Step 3: Implement**, then update every caller for the widened signature.
- [ ] **Step 4: Run `go test ./internal/prefssync/ ./desktop/ -race`**
- [ ] **Step 5: Mutation check** — make the conflict branch not record, confirm the test fails.
- [ ] **Step 6: Commit**

---

### Task 3: Status, and the bindings for it

**Files:**
- Modify: `desktop/prefs_sync_loop.go`
- Test: `desktop/prefs_sync_loop_test.go`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`, `desktop/frontend/src/lib/api/*.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`, `zh-CN.ts`

**Interfaces:**
- Produces:
  ```go
  type SyncStatus struct {
      State        string `json:"state"`          // "idle" | "syncing" | "offline" | "error"
      LastSyncedAt int64  `json:"last_synced_at"` // ms, 0 = never
      PendingKeys  int    `json:"pending_keys"`
      LastError    string `json:"last_error,omitempty"`
  }
  func (a *App) GetSyncStatus() SyncStatus
  func (a *App) SyncNow() error
  ```
  Events: `sync:status` (`SyncStatus`), `sync:pulled` (`prefssync.PullResult`).

**Behaviour the tests must pin:**
- `offline` when there is no relay URL, no session token, or `RelayPaused` — and specifically NOT `error`. "Not configured" is not a failure and must not show a red indicator.
- `error` carries the last failure's message and **clears on the next success**. The `markPrefDirtyAndPush` comment records that a silent 400 kept `ssh_hosts_encrypted` broken for months; this is where that stops being invisible, so a test must prove the message actually surfaces.
- `PendingKeys` counts dirty keys.
- `SyncNow` enqueues pull-then-push and returns immediately; it errors only for "cannot start" (offline).
- A `sync:status` event fires on every state transition, and `sync:pulled` fires with the `PullResult` after a pull that changed something.

- [ ] Steps as per Tasks 1-2: failing tests → implement → run → mutate → typecheck (`npx vue-tsc --noEmit`) → commit.

---

### Task 4: The indicator and the notice

**Files:**
- Create: `desktop/frontend/src/components/SyncStatusIndicator.vue` + test
- Modify: the settings header component that will host it

**Behaviour the tests must pin:**
- Each of the four states renders distinctly; `offline` is not styled as an error.
- `last_synced_at: 0` renders "never", not "1970".
- The "sync now" button calls `SyncNow` and is disabled while `syncing`.
- A `sync:pulled` event with adopted keys renders a dismissible notice naming them by their human-readable setting names, not raw keys like `terminal_font_head`.
- Conflicted keys are named as "changed on another device too — this machine's version was kept", distinctly from adopted ones.
- The event listener is removed on unmount (item 29 shipped this bug's twin; pin it).
- **Platform gate:** if this is desktop-only, gate it and test both shapes. If sync status is meaningful on web/iOS too, say so explicitly in the report rather than leaving it ambiguous.

---

### Task 5: Rebuild the embed, tick the roadmap

- [ ] Tick item 30 in `docs/roadmap.md`, in the same honest register as items 26-29: say what was left out (no merge UI, no per-key selection, LWW is still the resolution rule) and why.
- [ ] Note in the roadmap that the serial sync loop was a prerequisite, and that the engine's own comment had promised it for some time.
- [ ] `nvm use 20 && export PATH="$HOME/sdk/go1.23.12/bin:$PATH" && ./scripts/build-web.sh`
- [ ] `go build ./... && go test ./... -race && (cd desktop/frontend && npx vue-tsc --noEmit && npm test)`
- [ ] `gofmt -l $(git ls-files '*.go')` must be empty — CI gates this now.
- [ ] Commit.
