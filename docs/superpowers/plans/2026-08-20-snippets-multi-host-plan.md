# Snippets across many hosts — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run one quick-action snippet on N selected SSH hosts concurrently and show per-host output and exit status together.

**Architecture:** A new non-PTY exec method on `sshclient.Conn`, plus a desktop-side run manager that fans out over `App.dialThroughJumps` (so jump chains and per-hop host-key checks are inherited, not reimplemented) with a bounded worker pool, per-host timeout, and per-host output cap. Progress is pushed to the frontend as wails events.

**Tech Stack:** Go 1.23.12, `golang.org/x/crypto/ssh`, Wails v2.12.0, Vue 3, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-20-snippets-multi-host-design.md`

## Global Constraints

- **Redline #5:** `internal/` must not import `desktop/`. `sshclient` stays desktop-free.
- **Redline #4:** do not change the payload structure of any existing protocol frame. This feature adds no frames — it is Wails-binding only, desktop-local.
- **Redline #2:** do not touch `SetSubscriberLifecycle` or the uplink subscriber count. Batch exec is independent of session streaming, exactly like item 26's tunnels.
- `hostRunsProxyCommand` must remain at exactly **two** call sites. Batch exec dials only via `App.dialThroughJumps` and adds none.
- Every exported constructor/entry point that dials takes a `context.Context`.
- Go: the pinned toolchain is **go1.23.12** (`GO_VERSION` in `.github/workflows/build.yml`). Node: **20**.
- Any change under `desktop/frontend/src` or `web/src` requires rebuilding `internal/relay/web-dist/` via `./scripts/build-web.sh` before the final commit, or CI's drift gate fails.
- i18n: `en.ts` and `zh-CN.ts` key sets must match exactly — mismatch is a compile error.
- Bounds are exact: concurrency **8**, per-host timeout **60s**, per-host output cap **256 KiB** (`256 * 1024`).

---

### Task 1: Non-PTY exec on `sshclient.Conn`

**Files:**
- Create: `internal/sshclient/exec.go`
- Test: `internal/sshclient/exec_test.go`

**Interfaces:**
- Consumes: `Conn` from `internal/sshclient/sshclient.go` (field `client *ssh.Client`; check the actual field name before writing).
- Produces:
  ```go
  type ExecResult struct {
      Output    []byte
      ExitCode  int
      Truncated bool
  }
  func (c *Conn) Run(ctx context.Context, cmd string, limit int64) (ExecResult, error)
  ```

**Semantics that the tests must pin:**
- No PTY is requested. The session must not call `RequestPty`.
- Combined stdout+stderr, in arrival order, into one buffer.
- A **non-zero exit is not an error**: `ExitCode` carries it, returned `error` is nil, and `Output` still holds what the command printed. Only "could not run" (channel open failed, connection dropped, context expired) returns a non-nil error.
- `limit <= 0` means unlimited. Above the limit, capture stops at exactly `limit` bytes, `Truncated` is true, and the command is still allowed to finish so the exit code is real.
- `ctx` cancellation closes the session and returns `ctx.Err()`.
- Safe to call concurrently on one `Conn`.

- [ ] **Step 1: Write the failing tests**

Use the existing in-repo SSH test server helper if one exists (search `internal/sshclient` and `desktop/ssh_test_helpers_test.go` for an `ssh.NewServerConn` harness); otherwise stand up a minimal `ssh.ServerConfig` on a `net.Listen("tcp", "127.0.0.1:0")` that accepts a `"session"` channel, handles the `"exec"` request, writes canned bytes to stdout/stderr, and sends an `exit-status` reply.

```go
func TestRunCapturesStdoutAndStderrAndZeroExit(t *testing.T)
func TestRunNonZeroExitIsNotAnError(t *testing.T)   // err == nil, ExitCode == 3, Output preserved
func TestRunTruncatesAtLimitAndStillReportsExitCode(t *testing.T)
func TestRunUnlimitedWhenLimitIsZero(t *testing.T)
func TestRunCancelledContextReturnsCtxErr(t *testing.T)
func TestRunConcurrentCallsOnOneConn(t *testing.T)  // 8 goroutines, all succeed
func TestRunDoesNotRequestPty(t *testing.T)         // server records request types; "pty-req" absent
```

- [ ] **Step 2: Run them and watch them fail**

`go test ./internal/sshclient/ -run TestRun -v` → FAIL, `c.Run undefined`.

- [ ] **Step 3: Implement `Run`**

Open a session, wire `StdoutPipe`/`StderrPipe` into one `limitedBuffer` guarded by a mutex (both pipes write concurrently), `Start(cmd)`, then `Wait()`. Extract the exit code from `*ssh.ExitError` via `ExitStatus()`. Race `Wait()` against `ctx.Done()`; on cancellation close the session so `Wait` unblocks.

The limit is enforced by the buffer, not by reading the whole output and slicing — the point is never holding more than `limit` bytes.

- [ ] **Step 4: Run tests, expect PASS**

- [ ] **Step 5: Mutation check**

Delete the `Truncated` assignment and confirm `TestRunTruncatesAtLimitAndStillReportsExitCode` fails. Change the non-zero-exit path to return an error and confirm `TestRunNonZeroExitIsNotAnError` fails. If either still passes, the test is not testing.

- [ ] **Step 6: Commit**

```bash
git add internal/sshclient/exec.go internal/sshclient/exec_test.go
git commit -m "feat(sshclient): run a command over a non-PTY exec channel"
```

---

### Task 2: The batch run manager

**Files:**
- Create: `desktop/snippet_run.go`
- Test: `desktop/snippet_run_test.go`

**Interfaces:**
- Consumes: `(*App).dialThroughJumps(ctx, SSHHost, acceptedHostKey) (*jumpChain, error)` from `desktop/ssh_jump.go`; `jumpChain.Target() *sshclient.Conn`; `jumpChain.Close()`; `(*App).GetQuickTemplates() []QuickTemplate`; `a.eventsEmitter`; the SSH host store (find the accessor used by `ssh_tunnels.go:findForwardRule`).
- Produces:
  ```go
  const (
      snippetHostPending = "pending"
      snippetHostRunning = "running"
      snippetHostOK      = "ok"
      snippetHostFailed  = "failed"  // ran, non-zero exit
      snippetHostError   = "error"   // never ran
  )

  const (
      snippetMaxConcurrentHosts = 8
      snippetHostTimeout        = 60 * time.Second
      snippetMaxOutputBytes     = 256 * 1024
  )

  type SnippetHostResult struct {
      HostID    string `json:"host_id"`
      HostLabel string `json:"host_label"`
      State     string `json:"state"`
      ExitCode  int    `json:"exit_code"`
      Output    string `json:"output"`
      Truncated bool   `json:"truncated"`
      Error     string `json:"error,omitempty"`
  }

  func (a *App) RunSnippetOnHosts(snippetLabel string, snippetText string, hostIDs []string) (string, error)
  func (a *App) CancelSnippetRun(runID string) error
  ```

**Behaviour the tests must pin:**
- At most `snippetMaxConcurrentHosts` hosts run at once — assert with a counter that records the observed maximum, not with timing.
- One host's failure (dial error, non-zero exit, timeout) leaves every other host's result untouched and the run completing.
- Each host emits `snippet:run:progress` on entering `running` and again on reaching a terminal state, via `a.eventsEmitter`.
- Empty (or whitespace-only) `snippetText`, or empty `hostIDs`, returns an error and starts nothing.
- A host ID not in the store ends as `error`, not a panic.
- `CancelSnippetRun` on an unknown run ID returns an error; on a live run it moves `pending` hosts to `error` with a cancellation message and leaves finished results alone.
- The `jumpChain` is closed for every host, on every path.

- [ ] **Step 1: Write the failing tests**

Inject a fake dialer rather than talking to a real SSH server: add an unexported field on `App` (e.g. `snippetDialer func(context.Context, SSHHost) (snippetConn, error)`) defaulting to the real `dialThroughJumps`-backed implementation, where `snippetConn` is a small interface `{ Run(context.Context, string, int64) (sshclient.ExecResult, error); Close() error }`. Tests substitute it. This is the same injectability pattern `eventsEmitter` already uses.

```go
func TestRunSnippetOnHostsCapsConcurrencyAtEight(t *testing.T)
func TestRunSnippetOnHostsIsolatesOneHostFailure(t *testing.T)
func TestRunSnippetOnHostsNonZeroExitIsFailedNotError(t *testing.T)
func TestRunSnippetOnHostsEmitsRunningThenTerminalPerHost(t *testing.T)
func TestRunSnippetOnHostsRejectsUnknownSnippet(t *testing.T)
func TestRunSnippetOnHostsRejectsEmptyHostList(t *testing.T)
func TestRunSnippetOnHostsUnknownHostBecomesErrorResult(t *testing.T)
func TestRunSnippetOnHostsTimesOutOneHostWithoutStallingTheRun(t *testing.T)
func TestCancelSnippetRunLeavesFinishedResultsAlone(t *testing.T)
func TestRunSnippetOnHostsClosesEveryConn(t *testing.T)
```

- [ ] **Step 2: Run them, watch them fail**

- [ ] **Step 3: Implement**

A `snippetRun` struct holds `id`, a `context.CancelFunc`, and a mutex-guarded `map[string]SnippetHostResult`. `App` holds `snippetRuns map[string]*snippetRun` under its own mutex. Fan out with a buffered semaphore channel of size 8. Each host: `context.WithTimeout(runCtx, snippetHostTimeout)` → dial → `defer conn.Close()` → `Run(ctx, tpl.Text, snippetMaxOutputBytes)` → classify → emit.

Use `uuid.NewString()` for the run ID (the repo already depends on `github.com/google/uuid`).

- [ ] **Step 4: Run tests, expect PASS**

- [ ] **Step 5: Mutation check**

Raise the semaphore to 64 and confirm the concurrency test fails. Remove the `defer conn.Close()` and confirm `TestRunSnippetOnHostsClosesEveryConn` fails. Reject mutations the compiler catches — those prove nothing.

- [ ] **Step 6: Verify the redline**

```bash
grep -rn "hostRunsProxyCommand" desktop/ --include=*.go | grep -v _test
```
Expected: exactly 2 call sites, unchanged.

- [ ] **Step 7: Commit**

```bash
git add desktop/snippet_run.go desktop/snippet_run_test.go
git commit -m "feat(ssh): run one snippet across many hosts, bounded and isolated"
```

---

### Task 3: Bindings, events and i18n

**Files:**
- Modify: `desktop/frontend/src/lib/api/_bindings.ts` (add `RunSnippetOnHosts`, `CancelSnippetRun` mirroring the existing hand-written shim style)
- Modify: `desktop/frontend/src/i18n/messages/en.ts`, `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Test: extend `desktop/frontend/src/i18n/i18n.test.ts` only if it does not already assert key parity (check first — do not duplicate an existing assertion)

**Interfaces:**
- Consumes: Task 2's exported method signatures.
- Produces: `runSnippetOnHosts(snippetLabel: string, snippetText: string, hostIds: string[]): Promise<string>`, `cancelSnippetRun(runId: string): Promise<void>`, and the `snippet:run:progress` event payload type `SnippetHostResult`.

Keys to add under a new `snippets` namespace, both locales, same key set: `snippets.runOnHosts`, `snippets.selectHosts`, `snippets.running`, `snippets.ok`, `snippets.failed`, `snippets.error`, `snippets.exitCode`, `snippets.truncated`, `snippets.copyAll`, `snippets.cancel`, `snippets.noHostsSelected`, `snippets.hostKeyUntrusted`.

- [ ] **Step 1: Add the bindings and keys**
- [ ] **Step 2: Typecheck**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: clean. A missing key in one locale is a compile error here — that is the parity gate.

- [ ] **Step 3: Commit**

```bash
git commit -am "feat(i18n): strings and bindings for multi-host snippet runs"
```

---

### Task 4: The run panel

**Files:**
- Create: `desktop/frontend/src/components/SnippetRunPanel.vue`
- Create: `desktop/frontend/src/components/SnippetRunPanel.test.ts`
- Modify: whichever component owns the snippet/template list UI (`SettingsTemplates.vue`) to offer "run on hosts…"

**Interfaces:**
- Consumes: `runSnippetOnHosts`, `cancelSnippetRun`, the `snippet:run:progress` event, `SnippetHostResult`.

**Behaviour the tests must pin:**
- Renders one row per selected host, initially `pending`.
- A `snippet:run:progress` event updates only its own host's row.
- A `failed` row shows the exit code; an `error` row shows the message; a `truncated` row says so.
- The run button is disabled with no hosts selected.
- Cancel calls `cancelSnippetRun` with the live run ID.
- Copy-all concatenates every host's output with a per-host header.

- [ ] **Step 1: Write the failing component tests** (mock the bindings module, emit events through the same bus the app uses)
- [ ] **Step 2: Run, watch fail**
- [ ] **Step 3: Implement the panel**
- [ ] **Step 4: Run the full frontend suite**

```bash
cd desktop/frontend && npm test
```

- [ ] **Step 5: Commit**

---

### Task 5: Rebuild the embed, tick the roadmap

**Files:**
- Modify: `docs/roadmap.md` (item 29 checkboxes, with honest scope notes for the Non-Goals in spec §7)
- Modify: `internal/relay/web-dist/**` (generated)

- [ ] **Step 1: Tick item 29**, noting in the same prose style as items 26–28 what was deliberately left out: no history, no per-host substitution, no sudo prompt handling, no mobile.
- [ ] **Step 2: Rebuild the embed with the pinned toolchain**

```bash
nvm use 20
export PATH="$HOME/sdk/go1.23.12/bin:$PATH"
./scripts/build-web.sh
```

- [ ] **Step 3: Full suite**

```bash
go build ./... && go test ./... && (cd desktop/frontend && npm test)
```

- [ ] **Step 4: Commit**
