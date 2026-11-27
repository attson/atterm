# Dual-Pane SFTP Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a desktop-only singleton SFTP workspace with two independently selectable local/SSH panes and bounded streaming transfers between any two endpoints.

**Architecture:** A fixed top-level SFTP view stays outside terminal `Tab` state. Browse/edit operations reuse `FileSystemBridge`; large transfers run in a Go job manager with local and SFTP endpoint adapters, fixed-size buffers, atomic partial files, progress events, cancellation, and exact-pair TOFU retries.

**Tech Stack:** Go 1.23, github.com/pkg/sftp v1.13.9, Wails v2 bindings/events, Vue 3, TypeScript, Vitest, Vue Test Utils, lucide-vue-next.

**Spec:** `docs/superpowers/specs/2026-11-28-sftp-workspace-design.md`

## Global Constraints

- Desktop-only: require `caps.wailsBindings && caps.pluginHost`; Web/Capacitor show no entry and import no Wails-only implementation.
- Remote-to-remote bytes pass through a fixed 256 KiB Go buffer and never through Vue state, relay frames, or another server's credentials.
- `internal/` must not import `desktop/`; Relay protocol version and frames remain unchanged.
- Unknown host keys use the exact `(host, fingerprint)` acceptance pair and preserve jump-hop sequencing.
- Existing 5 MiB preview/edit bindings remain capped; large transfer uses separate streaming primitives.
- Destination files are written to `.atterm-transfer-<uuid>.partial`, then atomically renamed; failure/cancel cleans partial files best-effort.
- Default conflict policy is `skip`; `replace` and `rename` require explicit user choice.
- Symlinks, sockets, and devices are reported as skipped and never followed.
- Every visible string is present in English and Simplified Chinese.

---

### Task 1: Context-aware SFTP stream primitives

**Files:**
- Create: `internal/sftpfs/stream.go`
- Create: `internal/sftpfs/stream_test.go`
- Modify: `internal/sftpfs/sftpfs.go`

**Interfaces:**
- Consumes: `Client.sc`, `Client.posixRename`, and existing close/stall lifecycle.
- Produces: `Getwd`, `Lstat`, `OpenReader`, and `CreateStagedWriter`.

```go
type ReadHandle interface { io.Reader; io.Closer }

type StagedWriter interface {
    io.Writer
    Commit(ctx context.Context, replace bool) error
    Abort() error
}

func (c *Client) Getwd(ctx context.Context) (string, error)
func (c *Client) Lstat(ctx context.Context, name string) (os.FileInfo, error)
func (c *Client) OpenReader(ctx context.Context, name string) (ReadHandle, error)
func (c *Client) CreateStagedWriter(ctx context.Context, target string) (StagedWriter, error)
```

- [ ] **Step 1: Write failing stream tests over the existing in-memory SFTP server**

Test that `Getwd` returns an absolute directory, `Lstat` identifies symlinks without following them, a reader streams a 12 MiB file in bounded chunks, commit exposes only the final file, replace=false refuses an occupied target, and cancel/Abort removes the partial file.

Core assertion:

```go
w, err := client.CreateStagedWriter(ctx, "/dst.bin")
if err != nil { t.Fatal(err) }
_, err = io.CopyBuffer(w, bytes.NewReader(payload), make([]byte, 256<<10))
if err != nil { t.Fatal(err) }
_, err = client.FileMeta(ctx, "/dst.bin")
if err == nil { t.Fatal("destination became visible before commit") }
if err := w.Commit(ctx, false); err != nil { t.Fatal(err) }
got, err := client.ReadChunk(ctx, "/dst.bin", 0, int64(len(payload)))
if err != nil { t.Fatal(err) }
if !bytes.Equal(payload, got.Data) { t.Fatal("committed bytes differ") }
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/sftpfs -run 'Test(Getwd|Lstat|OpenReader|StagedWriter)' -count=1`

Expected: FAIL because the stream methods do not exist.

- [ ] **Step 3: Implement stream handles**

Use `pkg/sftp.Client.Getwd`, `Lstat`, `Open`, and `OpenFile`. Start a goroutine per open handle that closes it when `ctx.Done()` fires, and stop that goroutine when the handle closes. Generate partial paths with `tempPrefix + uuid`, keep them in the target directory, and use the existing POSIX rename extension when replace=true. `Abort` and `Commit` must be idempotent through `sync.Once`.

- [ ] **Step 4: Run package tests**

Run: `go test ./internal/sftpfs -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sftpfs/stream.go internal/sftpfs/stream_test.go internal/sftpfs/sftpfs.go
git commit -m "feat(sftp): add bounded streaming file handles"
```

### Task 2: Transfer manifest and copy engine

**Files:**
- Create: `desktop/file_transfer_engine.go`
- Create: `desktop/file_transfer_engine_test.go`

**Interfaces:**
- Consumes: standard `context`, `io`, and injected endpoint behavior.
- Produces: `transferEndpoint`, `buildTransferManifest`, and `runTransfer`.

```go
type transferKind string
const (
    transferFile transferKind = "file"
    transferDir transferKind = "dir"
    transferSkipped transferKind = "skipped"
)

type transferEntry struct {
    SourcePath string
    RelativePath string
    Kind transferKind
    Size int64
    ModTime int64
    SkipReason string
}

type transferEndpoint interface {
    ID() string
    Clean(string) (string, error)
    Join(string, ...string) string
    Base(string) string
    Lstat(context.Context, string) (transferEntry, error)
    List(context.Context, string) ([]transferEntry, bool, error)
    OpenReader(context.Context, string) (io.ReadCloser, error)
    CreateStagedWriter(context.Context, string) (transferStagedWriter, error)
    Mkdir(context.Context, string) error
    Remove(context.Context, string, bool) error
    Close() error
}

type transferStagedWriter interface {
    io.Writer
    Commit(context.Context, bool) error
    Abort() error
}

type FileTransferProgress struct {
    State, CurrentPath string
    BytesDone, BytesTotal int64
    EntriesDone, EntriesTotal int
}

type FileTransferResult struct {
    State, Error string
    Warnings []string
}
```

- [ ] **Step 1: Write failing fake-endpoint tests**

Cover regular file, nested/empty directory, 2000-entry truncation refusal, symlink skip, copy-into-descendant refusal, skip/replace/rename conflicts, source-changed warning, partial abort on read/write error, cancellation, and monotonic progress.

Use an in-memory fake endpoint and assert the copy buffer never exceeds `256 << 10` by recording the largest `Write` call.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./desktop -run 'TestTransferEngine' -count=1`

Expected: FAIL because the engine is absent.

- [ ] **Step 3: Implement manifest scanning and copy**

Define:

```go
type transferConflict string
const (
    conflictSkip transferConflict = "skip"
    conflictReplace transferConflict = "replace"
    conflictRename transferConflict = "rename"
)

func buildTransferManifest(ctx context.Context, src transferEndpoint, paths []string) ([]transferEntry, error)
func runTransfer(ctx context.Context, src, dst transferEndpoint, manifest []transferEntry, dstDir string, policy transferConflict, report func(FileTransferProgress)) FileTransferResult
```

Scan without following non-regular entries. Reject truncated directories. Create directories before files. For each file use `io.CopyBuffer(..., make([]byte, 256<<10))`, close the reader, then commit or abort the staged writer. Re-stat the source after copying and add a warning when size/modtime changed.

- [ ] **Step 4: Run engine tests**

Run: `go test ./desktop -run 'TestTransferEngine' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/file_transfer_engine.go desktop/file_transfer_engine_test.go
git commit -m "feat(sftp): add endpoint-neutral transfer engine"
```

### Task 3: Local and SFTP transfer endpoints

**Files:**
- Create: `desktop/file_transfer_endpoints.go`
- Create: `desktop/file_transfer_endpoints_test.go`
- Modify: `desktop/sftp_source.go`
- Modify: `desktop/sftp_source_test.go`

**Interfaces:**
- Consumes: `transferEndpoint` from Task 2 and stream primitives from Task 1.
- Produces: `newLocalTransferEndpoint`, `newSFTPTransferEndpoint`, `OpenSFTPSource`.

```go
type SFTPSourceInfo struct {
    HostID string `json:"host_id"`
    InitialDir string `json:"initial_dir"`
}

func (a *App) OpenSFTPSource(hostID string, accepted AcceptedHostKey) (SFTPSourceInfo, error)
func newLocalTransferEndpoint(access *fsAccess) transferEndpoint
func (a *App) newSFTPTransferEndpoint(hostID string, accepted AcceptedHostKey) (transferEndpoint, error)
```

- [ ] **Step 1: Write failing adapter and TOFU tests**

Assert local Lstat skips a symlink before `fsAccess.resolve`, local paths outside allowRoots fail, SFTP `Getwd` falls back to `/` only on unsupported/relative results, and an unknown jump-host key returns the same `HostKeyUnknownError` fields used by terminal/tunnel flows. Assert a wrong accepted pair still fails.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./desktop -run 'Test(Local|SFTP)TransferEndpoint|TestOpenSFTPSource' -count=1`

Expected: FAIL because adapters and preflight binding do not exist.

- [ ] **Step 3: Implement adapter lifecycle and exact TOFU dialing**

Change `sftpBrowser.dial` to accept `acceptedHostKey`; its existing browse calls pass the zero value. Add `clientWithHostKey(hostID, accepted)` for preflight/retry. Change `dialSFTP` to call `dialTunnelConnWithHostKey`. Do not cache failed dials. A transfer endpoint takes its own browser/client reference and releases it from `Close`.

Local staged writes use a partial in the target directory plus `os.Rename`; replace first moves through the existing explicit conflict decision. Both adapters return POSIX-style relative manifest names while preserving native local path joins internally.

- [ ] **Step 4: Run SFTP and adapter suites**

Run: `go test ./desktop -run 'SFTP|TransferEndpoint' -count=1`

Run: `go test ./internal/sftpfs -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/file_transfer_endpoints.go desktop/file_transfer_endpoints_test.go desktop/sftp_source.go desktop/sftp_source_test.go
git commit -m "feat(sftp): adapt local and SSH transfer sources"
```

### Task 4: Transfer job manager and Wails bindings

**Files:**
- Create: `desktop/file_transfer.go`
- Create: `desktop/file_transfer_test.go`
- Modify: `desktop/app.go`
- Modify: `desktop/app_nil_slice_test.go`
- Modify: `desktop/main.go`

**Interfaces:**
- Consumes: engine and adapters from Tasks 2-3, `App.eventsEmitter`.
- Produces: `StartFileTransfer`, `CancelFileTransfer`, `ListFileTransfers`, shutdown cleanup.

```go
type FileEndpoint struct { Kind, HostID string }
type StartFileTransferReq struct {
    Source FileEndpoint `json:"source"`
    Destination FileEndpoint `json:"destination"`
    Paths []string `json:"paths"`
    DestinationDir string `json:"destination_dir"`
    Conflict string `json:"conflict"`
    AcceptedHostKey AcceptedHostKey `json:"accepted_host_key"`
}
type FileTransferJob struct {
    ID, State, CurrentPath, Error string
    BytesDone, BytesTotal int64
    EntriesDone, EntriesTotal int
    Warnings []string
}
```

- [ ] **Step 1: Write failing manager tests**

Test immediate request validation, source/destination same-path refusal, maximum two concurrently running jobs, queued third job, monotonic `sftp:transfer-progress` events, cancellation terminal uniqueness, List returning `[]` instead of nil, and shutdown cancellation/release.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./desktop -run 'TestFileTransferManager|TestAppNilSlices' -count=1`

Expected: FAIL because manager methods are absent.

- [ ] **Step 3: Implement manager**

Store the manager on `App` behind a mutex, construct it lazily for tests, and initialize it in `NewApp`. Use a two-slot semaphore. Emit a snapshot on every phase transition and throttled byte progress at no more than 10 Hz. `CancelFileTransfer` is idempotent for terminal jobs and returns not-found for unknown IDs. `shutdown` cancels all jobs before closing the shared SFTP browser.

- [ ] **Step 4: Run desktop tests**

Run: `go test ./desktop -run 'FileTransfer|AppNil' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/file_transfer.go desktop/file_transfer_test.go desktop/app.go desktop/app_nil_slice_test.go desktop/main.go
git commit -m "feat(sftp): manage streaming transfer jobs"
```

### Task 5: Typed frontend SFTP and transfer API

**Files:**
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`
- Modify: `desktop/frontend/src/lib/api/ssh.ts`
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify generated Wails bindings under `desktop/frontend/wailsjs/go/main/`
- Test: `desktop/frontend/src/lib/api/ssh.test.ts`

**Interfaces:**
- Consumes: Go bindings from Tasks 3-4.
- Produces: typed `openSFTPSource`, `startFileTransfer`, `cancelFileTransfer`, `listFileTransfers`.

- [ ] **Step 1: Write failing API adapter tests**

Mock `window.go.main.App` and assert exact argument forwarding and the fail-closed default host-key pair:

```ts
await openSFTPSource("h1");
expect(App.OpenSFTPSource).toHaveBeenCalledWith("h1", { host: "", fingerprint: "" });
await startFileTransfer(request);
expect(App.StartFileTransfer).toHaveBeenCalledWith(request);
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd desktop/frontend && npm test -- src/lib/api/ssh.test.ts`

Expected: FAIL because exports/types are absent.

- [ ] **Step 3: Add frontend types and adapters**

Mirror JSON fields exactly, add methods to `AppBindings`, re-export from `lib/api.ts`, and regenerate Wails declarations with `cd desktop && wails build -s -nopackage`. Keep Wails imports inside the API/platform layer.

- [ ] **Step 4: Run API tests and typecheck**

Run: `cd desktop/frontend && npm test -- src/lib/api/ssh.test.ts`

Run: `cd desktop/frontend && npx vue-tsc --noEmit`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/api desktop/frontend/src/lib/api.ts desktop/frontend/wailsjs/go/main
git commit -m "feat(sftp): expose typed transfer bindings"
```

### Task 6: Dual-pane state model

**Files:**
- Create: `desktop/frontend/src/components/sftp/sftpWorkspaceModel.ts`
- Create: `desktop/frontend/src/components/sftp/sftpWorkspaceModel.test.ts`

**Interfaces:**
- Consumes: `SSHHost`, `SFTPListing`, and endpoint API types.
- Produces: pane state reducers, sorting/filtering, path helpers, request builder.

```ts
export type SftpSource = { kind: "local" } | { kind: "ssh"; hostId: string };
export interface SftpPaneState {
  source: SftpSource;
  cwd: string;
  back: string[];
  forward: string[];
  selectedPaths: Set<string>;
  query: string;
  sort: "name" | "modified" | "size";
  direction: "asc" | "desc";
}
export function navigate(state: SftpPaneState, path: string): SftpPaneState;
export function visibleEntries(entries: DirEntry[], state: SftpPaneState): DirEntry[];
export function buildTransferRequest(from: SftpPaneState, to: SftpPaneState, conflict: FileTransferConflict): StartFileTransferReq;
```

- [ ] **Step 1: Write failing model tests**

Cover independent pane history, source-switch reset, directories-first sorting in both directions, case-insensitive filter, multi-select request order, same endpoint/path rejection, and directory-into-descendant rejection.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd desktop/frontend && npm test -- src/components/sftp/sftpWorkspaceModel.test.ts`

Expected: FAIL because model is absent.

- [ ] **Step 3: Implement pure model**

Use immutable Set copies so Vue observes selection changes. Normalize remote paths with `/`; delegate local path validity to backend but preserve native absolute strings. `buildTransferRequest` uses endpoint snapshots and never includes credentials.

- [ ] **Step 4: Run model tests**

Run: `cd desktop/frontend && npm test -- src/components/sftp/sftpWorkspaceModel.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/sftp/sftpWorkspaceModel.ts desktop/frontend/src/components/sftp/sftpWorkspaceModel.test.ts
git commit -m "feat(sftp): add dual-pane workspace model"
```

### Task 7: Source picker and file pane UI

**Files:**
- Create: `desktop/frontend/src/components/sftp/SftpSourcePicker.vue`
- Create: `desktop/frontend/src/components/sftp/SftpPane.vue`
- Create: `desktop/frontend/src/components/sftp/SftpPane.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/sshHostFS.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

**Interfaces:**
- Consumes: model from Task 6, existing local/SSH `FileSystemBridge`, `openSFTPSource`.
- Produces: pane events `update:state`, `transfer-selection`, `toast`, and `host-key-required`.

- [ ] **Step 1: Write failing pane tests**

Mount with fake local and SSH bridges. Assert source options include Local and connectable hosts, selecting SSH calls `OpenSFTPSource` before listing, cwd starts at returned initial dir, Back/Forward/Up work, query filters current entries, multi-selection remains stable through sort, truncated listing displays count, and failures retain source/path with Retry. Cover create-directory, rename, permanent delete confirmation, text preview/edit through `FileEditor`, and binary/oversized metadata fallback.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd desktop/frontend && npm test -- src/components/sftp/SftpPane.test.ts`

Expected: FAIL because pane components are absent.

- [ ] **Step 3: Implement pane**

Use a compact header with source menu, Back/Forward/Up, editable path, Refresh, Search, sort menu, and icon buttons for create/rename/delete. Render a semantic file table with stable Name/Modified/Size/Kind columns, row keyboard navigation, checkbox multi-select, double-click directory navigation, and ellipsis for long names. Add tooltips to icon-only buttons.

Extend `createSSHHostFS(hostID, initialDir?)` only as needed to preserve existing bridge behavior. Keep previews/text editing through existing `FileEditor.vue`; do not copy CodeMirror setup.

- [ ] **Step 4: Run pane and existing explorer tests**

Run: `cd desktop/frontend && npm test -- src/components/sftp/SftpPane.test.ts src/plugins/fileExplorer/sshHostFS.test.ts src/plugins/fileExplorer/FileEditor.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/sftp desktop/frontend/src/plugins/fileExplorer/sshHostFS.ts desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(sftp): add selectable file panes"
```

### Task 8: Workspace transfers, conflicts, deletion and TOFU

**Files:**
- Create: `desktop/frontend/src/components/sftp/SftpWorkspace.vue`
- Create: `desktop/frontend/src/components/sftp/SftpTransferQueue.vue`
- Create: `desktop/frontend/src/components/sftp/SftpWorkspace.test.ts`
- Reuse: `desktop/frontend/src/components/ConfirmInstallDialog.vue` patterns, not updater-specific copy.

**Interfaces:**
- Consumes: panes from Task 7, transfer APIs/events from Tasks 4-5.
- Produces: full singleton workspace component.

- [ ] **Step 1: Write failing workspace tests**

Assert both panes independently select remote hosts; arrow left/right, internal drag/drop, context-menu transfer, and OS-file drop produce the correct requests; conflict modal defaults to skip and explicitly selects replace/rename; recursive remote delete requires two confirmations; progress events update matching job; cancel calls binding; completed transfer refreshes destination; TOFU retries exact pairs and can prompt sequential jump hops; credential/SFTP-unavailable errors remain scoped to one pane.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd desktop/frontend && npm test -- src/components/sftp/SftpWorkspace.test.ts`

Expected: FAIL because workspace components are absent.

- [ ] **Step 3: Implement workspace and queue**

Layout two panes in `grid-template-columns: minmax(0,1fr) 48px minmax(0,1fr)` with stable center transfer buttons. Stack below 760 px into source, controls, destination without overlap. Subscribe with `$platform.events.on("sftp:transfer-progress", ...)` before calling List; merge the returned snapshots by job id and latest state so neither an in-flight event nor an existing task is lost. Unsubscribe on unmount. Keep the component mounted while hidden at App level so active transfers and pane state remain visible on return.

Use internal drag MIME `application/x-atterm-sftp-paths`; payload contains endpoint identity and paths only. Render transfer queue as a bottom band, not nested cards. Use exact existing `parseHostKeyPrompt` and queue semantics from `SshHostsPanel.vue`.

- [ ] **Step 4: Run workspace tests and typecheck**

Run: `cd desktop/frontend && npm test -- src/components/sftp/SftpWorkspace.test.ts src/components/sftp/SftpPane.test.ts`

Run: `cd desktop/frontend && npx vue-tsc --noEmit`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/sftp
git commit -m "feat(sftp): build dual-pane transfer workspace"
```

### Task 9: Fixed top-level SFTP view and documentation

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/TabBar.test.ts`
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Modify: `docs/spec/architecture.md`
- Modify: `docs/spec/conventions.md`
- Modify: `.claude/skills/atterm-map/SKILL.md`
- Modify: `.github/scripts/check-plugin-fs-isolation.sh`

**Interfaces:**
- Consumes: `SftpWorkspace.vue` from Task 8.
- Produces: desktop-only fixed SFTP tab and mutually exclusive terminal/SFTP/admin views.

- [ ] **Step 1: Write failing TabBar and App tests**

Assert the fixed tab renders only when `sftpAvailable=true`, emits `toggle-sftp`, carries active styling, is absent from numbered/reorderable terminal tabs, and has no close button. In App tests assert SFTP hides TaskSidebar/right plugin/PaneGrid/AdminPanel, clicking a terminal tab returns to terminal, admin closes SFTP, and hidden SFTP remains mounted to retain state.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd desktop/frontend && npm test -- src/components/TabBar.test.ts src/App.test.ts`

Expected: FAIL because SFTP props/events/view are absent.

- [ ] **Step 3: Integrate fixed view**

Add `FolderSync` (or the closest lucide SFTP/file-transfer icon) to TabBar. Props/events:

```ts
sftpAvailable?: boolean;
sftpOpen?: boolean;
(e: "toggle-sftp"): void;
```

In App use one `mainView: Ref<"terminal" | "sftp" | "admin">`; adapt existing admin behavior so identity loss resets `admin` to `terminal`. `gotoTab` always sets terminal. Render SFTP with `v-show` to retain state, while terminal panes keep their existing mounted-tab behavior. Gate availability with `caps.wailsBindings && caps.pluginHost`.

- [ ] **Step 4: Update docs and isolation guard**

Document the local-only singleton view, endpoint-neutral Go transfer manager, 256 KiB bound, partial-file rule, exact TOFU retry, and no Relay exposure. Add transfer source identifiers to the existing isolation script so references from `desktop/uplink*.go` or `internal/relay` fail CI.

- [ ] **Step 5: Run feature and regression verification**

Run: `go test ./internal/sftpfs ./desktop -count=1`

Run: `cd desktop/frontend && npm test -- src/components/SshHostsPanel.test.ts src/components/TabBar.test.ts src/components/sftp src/plugins/fileExplorer`

Run: `cd desktop/frontend && npm run build:wails`

Run: `.github/scripts/check-plugin-fs-isolation.sh`

Expected: all commands PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TabBar.vue desktop/frontend/src/components/TabBar.test.ts desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts docs/spec/architecture.md docs/spec/conventions.md .claude/skills/atterm-map/SKILL.md .github/scripts/check-plugin-fs-isolation.sh
git commit -m "feat(sftp): add fixed top-level workspace"
```

### Task 10: Runtime visual and transfer verification

**Files:**
- Modify only files needed to fix failures found by the checks below, with a reproducing test added first.

**Interfaces:**
- Consumes: completed feature from Tasks 1-9.
- Produces: evidence that the actual desktop UI and large transfer path satisfy the spec.

- [ ] **Step 1: Start the desktop dev server**

Run: `cd desktop && wails dev`

Expected: AT Term opens and the fixed SFTP tab is visible on desktop.

- [ ] **Step 2: Inspect three viewports with AIPageGo**

Check 1440x900, 1024x768, and 760x900. Capture screenshots and verify: no overlapping controls, both panes retain usable widths, long host/path text truncates, transfer queue does not cover lists, and narrow layout stacks coherently.

- [ ] **Step 3: Verify remote-to-remote and bounded memory**

Using two saved test SSH hosts, copy a file larger than 100 MiB. Confirm progress is monotonic, switching to a terminal keeps PTY input responsive, destination is complete, no `.atterm-transfer-*.partial` remains, and WebView memory does not grow by the full file size.

- [ ] **Step 4: Verify cancellation and conflicts**

Cancel a large transfer and confirm the target partial disappears. Exercise skip, replace, and rename against an occupied destination. Exercise recursive directory transfer containing an empty directory and a symlink; confirm empty directory is created and symlink is reported skipped.

- [ ] **Step 5: Run full affected suites**

Run: `go test ./internal/sftpfs ./desktop -count=1`

Run: `cd desktop/frontend && npm test`

Run: `cd desktop/frontend && npm run build:wails`

Expected: all commands PASS with no warnings introduced by this feature.

- [ ] **Step 6: Commit any test-driven corrections**

```bash
git add desktop internal/sftpfs docs/spec .claude/skills/atterm-map/SKILL.md .github/scripts/check-plugin-fs-isolation.sh
git commit -m "fix(sftp): address runtime verification findings"
```

Skip this commit when no correction was needed.
