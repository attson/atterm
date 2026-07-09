# Updater — Cancel & Existing-File Detection — Design

**Date:** 2026-07-09
**Status:** Draft, pending review
**Related work:**
- 2026-05-10 auto-update-design (baseline updater architecture)
- 2026-05-24 update-gh-proxy-design (proxy layer in `Updater.downloadURL`)
- 2026-06-26 update-version-line-selector-design (multi-line `DownloadVersion`)

## Summary

Add two independently-motivated capabilities to `Settings → Updates`:

1. **Cancel an in-flight download.** Today the "Downloading 42%" button is
   disabled — the user's only recourse is quit-and-relaunch. This change turns
   the button into a clickable "Cancel (42%)" that interrupts the HTTP read,
   removes the `.partial` file, and returns the UI to the pre-download state.

2. **Detect an already-downloaded package (lazily).** When the user clicks
   "Download vX.Y.Z" and the target archive already exists on disk with a
   valid checksum, don't blindly re-download. Instead, mark the state as
   `Ready` and prompt the user: "vX.Y.Z is already downloaded on this device.
   Redownload?" Answering "OK" forces a fresh download; answering "Cancel"
   keeps the existing file and shows the "Install & Restart" button plus a
   secondary "Redownload" button so the user can trigger a re-download later.

Together these give users control over what the updater does when they change
their mind mid-download, and eliminate the frequent wasted-bandwidth case
where the same archive is re-fetched because the previous session's Ready
state was lost on restart.

## Goals & non-goals

### Goals

- The `Downloading` UI presents a single clickable button labelled
  `Cancel ({pct}%)`. Clicking it interrupts the HTTP transfer, deletes the
  `.partial` file, and returns the UI to the pre-download state (button flips
  back to the standard "Download vX.Y.Z" primary).
- Cancellation is best-effort but timely: the cancel request returns
  immediately; the goroutine unwinds within milliseconds; UI reflects the
  cleared state within one 2-second poll.
- When the target archive already exists on disk with a valid checksum, a
  click on "Download vX.Y.Z" transitions to `Ready` without any HTTP traffic
  and emits `downloaded_exists=true` so the UI can prompt.
- The re-download prompt is a native `window.confirm` on the just-clicked
  download interaction. Cancelling the prompt leaves `Ready=true`; confirming
  triggers a forced re-download.
- Even after "Install & Restart" appears, a secondary "Redownload" button is
  always visible so users can trigger a re-download without navigating away
  and coming back.
- Local ptys, relay uplink, and every non-updater setting are unaffected.

### Non-goals

- No resume / partial-content HTTP support. Cancellation deletes the
  `.partial` file; the next download starts from byte 0. (Adding HTTP Range
  resumption would rewrite `copyWithProgress` + `verifyDownloadedArchive`;
  out of scope.)
- No on-boot disk scan. Detection is lazy: only fires when the user clicks
  a download button. (An eager scan would surprise users who kept a stale
  archive around intentionally.)
- No "delete downloaded file" affordance. If the user wants to free disk
  space, they use the OS file manager on the shown `download_path`.
- No changes to the `StartDownload` API's semantics for existing callers.
  Both `StartDownload` and `DownloadVersion` gain the same lazy-check
  behavior so the paths stay symmetric.

## Architecture

Four backend verbs total (2 new, 2 existing with widened semantics) + one
new `UpdateState` field + one new `window.confirm` on the frontend + two
new UI buttons ("Cancel (N%)" and "Redownload").

```
SettingsUpdates.vue
  ├─ "Download vX.Y.Z" click
  │    └─ downloadVersion(tag)                  (existing binding, lazy)
  │         └─ App.DownloadVersion(tag)
  │              └─ Updater.DownloadVersion(ctx, tag)
  │                   ├─ if <final> exists && verifyDownloadedArchive OK:
  │                   │     state.Ready=true; state.DownloadedExists=true; return nil
  │                   └─ else → Updater.Download(ctx) — HTTP flow
  │
  ├─ watch state.downloaded_exists false→true (while clickInFlight):
  │    └─ window.confirm('vX.Y.Z is already downloaded. Redownload?')
  │         ├─ OK → forceRedownload(tag)
  │         └─ Cancel → nothing (Ready=true already surfaced)
  │
  ├─ "Cancel (42%)" click while state.downloading:
  │    └─ cancelDownload()                       (new binding)
  │         └─ App.CancelDownload()
  │              └─ Updater.Cancel()
  │                   ├─ trigger stored cancelFunc() → interrupts HTTP + copy
  │                   ├─ remove <final>.partial
  │                   └─ state.Downloading=false; DownloadPct=0; Error=""
  │
  └─ "Redownload" click while state.ready:
       └─ forceRedownload(tag)                   (new binding, no confirm here)
            └─ App.ForceRedownload(tag)
                 └─ Updater.ForceRedownload(ctx, tag)
                      ├─ state.Ready=false; state.DownloadedExists=false
                      ├─ remove existing <final>
                      └─ Updater.Download(ctx) — HTTP flow, no lazy check
```

`Cancel`'s cleanup is idempotent: calling it when nothing is downloading is
a no-op. The `Cancel` semantics do NOT touch `state.DownloadedExists` — that
field is only about lazy-detection results, not about download lifecycle.

## Backend

### `UpdateState` — new field

Add to `desktop/updater.go`:

```go
type UpdateState struct {
    // ...existing fields...

    // DownloadedExists signals that the most recent DownloadVersion /
    // StartDownload call short-circuited to Ready because a validated
    // archive already existed on disk. The frontend watches this
    // transition from false→true to decide whether to prompt "redownload?"
    // Cleared to false when a real download starts (Download() top) and
    // when ForceRedownload runs. Not touched by Cancel().
    DownloadedExists bool `json:"downloaded_exists,omitempty"`
}
```

Frontend mirror in `desktop/frontend/src/lib/api.ts`:

```ts
export interface UpdateState {
  // ...existing fields...
  downloaded_exists: boolean;
}
```

### `Updater.Cancel()` — new method

Store the download's cancel func on the struct so external callers can
interrupt it.

Fields to add:

```go
type Updater struct {
    // ...existing...
    cancelDownload context.CancelFunc  // set at Download start, cleared at end
}
```

The rewired `Download(ctx)` prologue is documented in one place in the
"Consolidated `Download(ctx)` prologue" subsection below, so the
implementer doesn't have to merge two overlapping sketches. The Cancel
section here defines only the `Cancel()` method itself.

`Cancel()` method:

```go
// Cancel interrupts an in-flight download (if any). Idempotent: does
// nothing when no download is running. The .partial file is removed
// best-effort so the next download restarts cleanly.
func (u *Updater) Cancel() {
    u.mu.Lock()
    cancel := u.cancelDownload
    downloadPath := u.state.DownloadPath
    u.mu.Unlock()

    if cancel == nil {
        return
    }
    cancel()  // unblocks copyWithProgress; goroutine unwinds via existing defer paths

    // Clear .partial best-effort — cancel() only interrupts, the goroutine's
    // defer path also tries to remove the .partial on error, but we don't
    // want to race with it. os.Remove is idempotent w.r.t. "already gone".
    if downloadPath != "" {
        _ = os.Remove(downloadPath + ".partial")
    }

    u.mu.Lock()
    u.state.Downloading = false
    u.state.DownloadPct = 0
    u.state.Error = ""
    u.mu.Unlock()
}
```

`recordError` gains a `context.Canceled` short-circuit so the user-cancel
path does not surface as a UI error. Timeout continues to surface because
its sentinel is `context.DeadlineExceeded`, not `context.Canceled`:

```go
func (u *Updater) recordError(err error) {
    if errors.Is(err, context.Canceled) {
        // User-initiated Cancel(). State was already cleared by Cancel()
        // itself; do not clobber it with an error message.
        return
    }
    // context.DeadlineExceeded and every other error continues to the
    // existing body — surfaced via state.Error.
    // ...existing body...
}
```

Wails binding on `desktop/app.go`:

```go
func (a *App) CancelDownload() {
    if a.updater == nil { return }
    a.updater.Cancel()
}
```

### Lazy detection helper + `DownloadVersion` / `Download` widening

Extract a helper on `Updater`:

```go
// tryClaimExistingArchive checks whether the target archive for `latest`
// already exists on disk and verifies OK. Returns (hit=true, nil) if the
// state was updated to Ready and no HTTP download is needed. Returns
// (hit=false, nil) if there is nothing on disk or the file exists but
// fails verification (in which case the corrupted file is removed).
// Returns (hit=false, err) only for infrastructure errors (updatesDir
// lookup, platform asset name lookup).
func (u *Updater) tryClaimExistingArchive(ctx context.Context, latest string) (bool, error) {
    dir, err := u.updatesDir()
    if err != nil {
        return false, err
    }
    name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
    if err != nil {
        return false, err
    }
    final := filepath.Join(dir, latest+"-"+name)
    if _, err := os.Stat(final); err != nil {
        return false, nil // not on disk — miss
    }
    if verifyErr := u.verifyDownloadedArchive(ctx, final, name); verifyErr != nil {
        _ = os.Remove(final)  // corrupted; treat as miss
        return false, nil
    }
    u.mu.Lock()
    u.state.Downloading = false
    u.state.Ready = true
    u.state.DownloadPct = 100
    u.state.DownloadDir = dir
    u.state.DownloadPath = final
    u.state.DownloadedExists = true
    u.state.Error = ""
    u.mu.Unlock()
    return true, nil
}
```

`DownloadVersion(ctx, tag)` — its current shape is `prepareVersion → Download`.
Because `Download` now does the lazy check itself, `DownloadVersion` needs no
extra logic:

```go
// Unchanged from current implementation:
func (u *Updater) DownloadVersion(ctx context.Context, tag string) error {
    if err := u.prepareVersion(ctx, tag); err != nil { return err }
    return u.Download(ctx)  // Download handles both lazy hit and real download
}
```

### Consolidated `Download(ctx)` prologue

This is the single source of truth for how `Download(ctx)`'s top changes.
Everything below `state.Downloading = true` is unchanged from today.

```go
func (u *Updater) Download(ctx context.Context) error {
    u.mu.Lock()
    latest := u.state.Latest
    assetURL := u.state.AssetURL
    u.mu.Unlock()
    if assetURL == "" {
        return fmt.Errorf("no asset URL — Check first")
    }

    // 1. Lazy-check: if the target archive is already on disk and validates,
    //    short-circuit to Ready without any HTTP traffic. tryClaimExistingArchive
    //    itself takes u.mu, so we release it above.
    if hit, err := u.tryClaimExistingArchive(ctx, latest); err != nil {
        u.recordError(err)
        return err
    } else if hit {
        return nil
    }

    // 2. Set up cancellable ctx + timeout. Nested contexts distinguish the
    //    two failure modes: an external Cancel() produces context.Canceled;
    //    the timeout produces context.DeadlineExceeded. recordError swallows
    //    only Canceled, so timeout still surfaces as a UI error (matches
    //    current behavior).
    cancelCtx, cancelDownload := context.WithCancel(ctx)
    downloadCtx, timeoutCancel := context.WithTimeout(cancelCtx, updaterDownloadTimeout)
    defer timeoutCancel()
    defer cancelDownload()

    // 3. Stash the cancel func + reset DownloadedExists (a fresh HTTP attempt
    //    supersedes any prior lazy-hit signal).
    u.mu.Lock()
    u.cancelDownload = cancelDownload
    u.state.DownloadedExists = false
    u.mu.Unlock()

    // 4. Clear cancel-func slot on any exit path.
    defer func() {
        u.mu.Lock()
        u.cancelDownload = nil
        u.mu.Unlock()
    }()

    // ------ everything below is the existing Download body, unchanged.
    //        It starts with `dir, err := u.updatesDir()` etc. and
    //        uses `downloadCtx` where the current code uses the local
    //        WithTimeout variable.
    dir, err := u.updatesDir()
    if err != nil {
        u.recordError(err)
        return err
    }
    // ...unchanged...
}
```

### `Updater.ForceRedownload(ctx, tag)` — new method

Skips the lazy check by removing any existing file before delegating to
`Download`:

```go
func (u *Updater) ForceRedownload(ctx context.Context, tag string) error {
    if err := u.prepareVersion(ctx, tag); err != nil {
        return err
    }
    u.mu.Lock()
    latest := u.state.Latest
    u.state.Ready = false
    u.state.DownloadedExists = false
    u.state.DownloadPct = 0
    u.mu.Unlock()

    dir, err := u.updatesDir()
    if err != nil {
        u.recordError(err)
        return err
    }
    name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
    if err != nil {
        u.recordError(err)
        return err
    }
    _ = os.Remove(filepath.Join(dir, latest+"-"+name))
    return u.Download(ctx)  // will lazy-miss now (file gone) → real download
}
```

Wails binding:

```go
func (a *App) ForceRedownload(tag string) error {
    if a.updater == nil { return nil }
    return a.updater.ForceRedownload(a.ctx, tag)
}
```

### API surface summary

| Wails binding | Existing / New | Semantics |
|---|---|---|
| `StartDownload()` | existing, widened | Lazy check → Ready+DownloadedExists on hit, HTTP on miss |
| `DownloadVersion(tag)` | existing, widened | Same as above but for a specific tag |
| `CancelDownload()` | **new** | Interrupts in-flight HTTP, deletes `.partial`, resets Downloading state |
| `ForceRedownload(tag)` | **new** | Deletes existing `<final>`, runs `Download` (no lazy check) |

## Frontend

### `SettingsUpdates.vue` script changes

Add imports for the two new bindings:

```ts
import {
  checkUpdate, downloadVersion, getAutoCheckUpdates, getUpdateGHProxyURL,
  getUpdateState, setAutoCheckUpdates, setUpdateGHProxyURL, startDownload,
  cancelDownload, forceRedownload,  // NEW
  type UpdateState,
} from "../lib/api"
```

Add new refs:

```ts
const clickInFlight = ref(false)         // this download click hasn't been resolved yet
const confirmingRedownload = ref(false)  // reentrance guard on the confirm dialog
const cancelling = ref(false)
```

Modify `onDownload` and `onDownloadSelected` to set `clickInFlight` before
the API call:

```ts
async function onDownload() {
  clickInFlight.value = true
  try { await startDownload() } catch { /* poll surfaces error */ }
}
async function onDownloadSelected() {
  if (!selectedLatest.value) return
  clickInFlight.value = true
  try { await downloadVersion(selectedLatest.value) } catch { /* poll surfaces */ }
}
```

Add the confirm-on-lazy-hit watcher:

```ts
watch(() => state.value?.downloaded_exists, async (now, was) => {
  if (!now || was) return          // only react to false→true
  if (!clickInFlight.value) return // not from this click — avoid spurious prompt
  if (confirmingRedownload.value) return
  const tag = state.value?.latest ?? ''
  clickInFlight.value = false
  confirmingRedownload.value = true
  try {
    if (window.confirm(t('settings.updates.redownloadPrompt', { version: tag }))) {
      await forceRedownload(tag)
    }
    // Cancel branch: do nothing. state.ready is already true so the
    // "Install & Restart" and "Redownload" buttons show themselves.
  } finally {
    confirmingRedownload.value = false
  }
})
```

Add the cancel handler:

```ts
async function onCancelDownload() {
  cancelling.value = true
  try { await cancelDownload() }
  catch { /* poll surfaces error */ }
  finally { cancelling.value = false }
}
```

Add the redownload handler (used by the always-visible-when-Ready button):

```ts
async function onRedownload() {
  const tag = state.value?.latest ?? ''
  if (!tag) return
  try { await forceRedownload(tag) } catch { /* poll surfaces error */ }
}
```

### `SettingsUpdates.vue` template changes

Inside the `.actions` block, replace the current downloading and ready
buttons with the following (other buttons — Check Now, Download primary,
DownloadVersion primary — unchanged):

```html
<!-- Downloading: was disabled "Downloading 42%", now clickable "Cancel (42%)" -->
<button
  v-if="state.downloading"
  class="primary danger"
  :disabled="cancelling"
  @click="onCancelDownload"
>{{ cancelling
    ? t('settings.updates.cancelling')
    : t('settings.updates.cancelDownload', { pct: state.download_pct }) }}</button>

<!-- Ready: Install & Restart (primary danger) + Redownload (secondary) -->
<button
  v-if="state.ready"
  class="primary danger"
  @click="$emit('request-install', state.latest)"
>{{ t('settings.updates.forceInstallRestart') }}</button>
<button
  v-if="state.ready"
  class="secondary"
  @click="onRedownload"
>{{ t('settings.updates.redownload') }}</button>
```

Scoped styles for the new `.secondary` variant (append to `<style scoped>`):

```css
button.secondary {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
}
button.secondary:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
```

### i18n keys

Add to both `desktop/frontend/src/i18n/messages/en.ts` and
`desktop/frontend/src/i18n/messages/zh-CN.ts`, inside the existing
`settings.updates.*` block:

| key | en | zh-CN |
|---|---|---|
| `cancelDownload` | `Cancel ({pct}%)` | `取消 ({pct}%)` |
| `cancelling` | `Cancelling…` | `取消中…` |
| `redownload` | `Redownload` | `重新下载` |
| `redownloadPrompt` | `{version} is already downloaded on this device. Redownload?` | `本机已下载过 {version} 的安装包。是否重新下载？` |

### Frontend bindings

`desktop/frontend/src/lib/api.ts` gains two entries in `AppBindings` and
two top-level wrappers, matching the existing `startDownload` pattern:

```ts
interface AppBindings {
  // ...
  CancelDownload(): Promise<void>;
  ForceRedownload(tag: string): Promise<void>;
}

export function cancelDownload(): Promise<void> {
  return bindings().CancelDownload();
}
export function forceRedownload(tag: string): Promise<void> {
  return bindings().ForceRedownload(tag);
}
```

The wailsjs generated `App.d.ts` / `App.js` gain matching declarations
(hand-synced; a subsequent `wails build` regenerates them identically).

## Data flow — end-to-end

Happy path A: user clicks Download, no existing file.

1. `onDownload` sets `clickInFlight=true`, calls `startDownload`.
2. Backend: `Download` → lazy check misses → HTTP flow starts.
3. Poll picks up `downloading=true, download_pct=…`. UI shows "Cancel (N%)".
4. HTTP completes, `Rename(partial→final)`, `state.Ready=true`,
   `Downloading=false`.
5. Poll picks up `ready=true`. UI shows "Install & Restart" + "Redownload".
6. `clickInFlight` was consumed by the watcher? No — it only fires on
   `downloaded_exists=false→true`. In this path, `downloaded_exists` stayed
   `false`. `clickInFlight` remains `true` but is idle; the next click
   overwrites it. Harmless.

Happy path B: user clicks Download, file already on disk and valid.

1. `onDownload` sets `clickInFlight=true`, calls `startDownload`.
2. Backend: `Download` → lazy check hits → `Ready=true`,
   `DownloadedExists=true`.
3. Poll picks up. UI watcher sees `downloaded_exists=false→true` with
   `clickInFlight=true` → shows `window.confirm`.
4a. User confirms: `forceRedownload(tag)` → backend removes file, clears
    `Ready`/`DownloadedExists`, runs `Download` (miss now, HTTP flow).
4b. User cancels: nothing. `Ready=true` already; buttons already rendered
    as "Install & Restart" + "Redownload".

Happy path C: user cancels an in-flight download.

1. State is `downloading=true, download_pct=42`.
2. User clicks "Cancel (42%)". `cancelling.value=true`, `cancelDownload()`.
3. Backend: `cancelDownload` triggers stored `cancelFunc`. HTTP copy loop
   returns `context.Canceled`. `copyErr != nil` branch removes `.partial`
   (existing code) and calls `recordError(context.Canceled)` → short-circuits.
4. `Cancel()` itself also removes `.partial` (idempotent), sets
   `Downloading=false, DownloadPct=0, Error=""`.
5. `cancelling.value=false`. Poll picks up cleared state. UI reverts to
   "Download vX.Y.Z" primary.

Failure paths:

- **Cancel race — HTTP already completed the copy, Rename in-flight**:
  `cancel()` no-ops on the completed context. `state.Ready` will be true on
  the next poll; UI shows install + redownload. User can click "Redownload"
  to explicitly restart. Acceptable — the cancel button is on a 2s stale
  view anyway.
- **Verify fails for on-disk file**: `tryClaimExistingArchive` removes the
  file and returns `hit=false`. `Download` proceeds normally. No UI signal
  needed — this is an internal correction.
- **`os.Remove(partial)` fails during Cancel**: swallowed by `_ = os.Remove`.
  State is still cleared. The stray `.partial` will be overwritten by the
  next download attempt (existing behavior — `os.Create(partial)` truncates).
- **`ForceRedownload` fails after removing the file**: user is left with
  `state.error` set. UI shows the error. `Ready` is false. Next Check + Download
  will re-fetch. Same behavior as any download failure today.

## Testing

### Backend

New file `desktop/updater_cancel_test.go`:

- `TestDownload_HitsExistingFile`: pre-write a fixture archive that passes
  `verifyDownloadedArchive` into the `updatesDir`, seed `state.AssetURL` +
  `state.Latest`, call `Download`. Assert no HTTP request was made,
  `state.Ready=true`, `state.DownloadedExists=true`, `state.DownloadPath`
  points to the fixture. Uses a `fakeClient` that records requests so a
  zero-count assertion is meaningful.
- `TestDownload_MissingFile_GoesToHTTP`: no fixture, seed
  `state.AssetURL` pointing at a test server serving valid content.
  Assert `Ready=true, DownloadedExists=false` after completion, and one
  HTTP request was made.
- `TestDownload_CorruptedFile_DeletesAndGoesToHTTP`: pre-write a file with
  content that fails `verifyDownloadedArchive`. Assert the file was
  removed AND HTTP was called AND `DownloadedExists=false`.
- `TestCancel_InterruptsInFlightDownload`: use a slow-body test server
  (write bytes, sleep, write more). Start `Download` in a goroutine, wait
  50ms for it to start streaming, call `Cancel()`. Assert
  `Downloading=false`, `Error==""`, `.partial` does not exist within 500ms
  of the cancel.
- `TestCancel_NoOpWhenIdle`: call `Cancel()` on a fresh `Updater`. Assert
  no panic, no state mutation.
- `TestForceRedownload_RemovesExistingAndDownloads`: pre-write a valid
  fixture + set `state.Ready=true, DownloadedExists=true`. Serve fresh
  content from test HTTP. Call `ForceRedownload`. Assert the fixture was
  overwritten with new bytes, `DownloadedExists=false`, `Ready=true` after
  completion.
- `TestForceRedownload_ClearsStateBeforeDownload`: instrument
  `ForceRedownload` (via captured HTTP handler) so we can assert
  `state.Ready=false` and `state.DownloadedExists=false` DURING the
  download (before the request completes).
- `TestRecordError_SwallowsContextCanceled_KeepsDeadlineExceeded`: seed
  an existing `Error`, call `recordError(context.Canceled)` — assert
  `state.Error` unchanged. Then in the same test call
  `recordError(context.DeadlineExceeded)` — assert `state.Error` becomes
  non-empty (timeout still surfaces, matches current behavior).

### Frontend

New file `desktop/frontend/src/components/__tests__/SettingsUpdates.test.ts`
(the file does not exist yet; create it following the shape of
`SettingsRelay.test.ts`):

- `renders "Cancel (N%)" button while downloading and clicking it calls
  cancelDownload()`: mount with `state.downloading=true, download_pct=42`;
  assert button text contains `Cancel (42%)`; click; assert
  `cancelDownload` spy called once.
- `renders both "Install & Restart" and "Redownload" while Ready`: mount
  with `state.ready=true`; assert both buttons rendered.
- `clicking Redownload calls forceRedownload(currentTag)`: mount with
  `state.ready=true, latest='v0.2.168'`; click Redownload; assert spy
  called with `'v0.2.168'`.
- `on downloaded_exists false→true while clickInFlight, prompts and calls
  forceRedownload on confirm`: mount with initial state, click Download,
  update mock so `getUpdateState` returns `downloaded_exists=true` on next
  poll (advance vi.useFakeTimers by 2000ms); stub `window.confirm=true`;
  assert `forceRedownload` called.
- `same flow but confirm=false does not call forceRedownload`: as above
  with `window.confirm=false`; assert `forceRedownload` NOT called.
- `spurious downloaded_exists (no click) does not prompt`: mount, then
  emit poll with `downloaded_exists=true` without clicking; assert
  `window.confirm` not called.

Use the existing i18n mock (`t: (k) => k`) so assertions check key strings
verbatim.

## Risks & mitigations

- **Race: user clicks Cancel while HTTP is already `Rename`-ing**:
  documented above under Failure paths. Cancel becomes a no-op; UI's next
  poll shows Ready. User can Redownload to force a fresh attempt. The
  window is narrow (`Rename` on a local file is near-atomic) but real.
- **Race: user clicks Download twice in quick succession**: the second
  click sets `clickInFlight=true` before the first watcher fires. If the
  first click resulted in a lazy-hit, the watcher already fired; but if
  the first click was in flight (HTTP), the second click still sets
  `clickInFlight=true` — but `downloaded_exists` won't flip to true from
  the second call because backend is still in HTTP flow from the first.
  Bottom line: no spurious confirm.
- **`context.Canceled` leaking to `state.Error`**: mitigated by the
  `recordError` guard. Tested.
- **`ForceRedownload` deletes the existing file before starting download,
  then network fails**: user is left without an archive AND no Ready.
  Same failure mode as any download attempt today, and less bad than
  keeping a stale archive of the wrong version. Accepted.
- **Timeout vs user-cancel distinguishability**: replacing
  `context.WithTimeout` with a two-layer `WithCancel(parent) +
  WithTimeout(cancelCtx)` preserves timeout behavior — timeout still fires
  after `updaterDownloadTimeout` with `context.DeadlineExceeded` — and
  gives user-cancel a distinct sentinel (`context.Canceled`). `recordError`
  swallows only `Canceled`, so timeout still surfaces to the UI as
  before. If a prior `TestDownload_TimesOut` exists in `updater_test.go`,
  it continues to assert the same visible behavior.
