# Auto-Update — Design

**Status**: Approved (brainstorming) → ready for implementation plan
**Date**: 2026-05-10
**Scope**: desktop app only; relay/CLI agent unchanged

## Goal

Let atterm desktop notify the user when a new version is on GitHub
Releases and let the user — explicitly, on their schedule — download and
install it. The trigger is always a manual button click. Auto-restart
on download is forbidden because atterm is a terminal and may host
foreground processes (`vim`, `ssh`, `codex`, builds) that the user must
not lose.

## Non-goals

- Silent background install (`/install on next launch` style). Always
  user-triggered.
- Pre-release / beta channel. v0 ships stable-only.
- Codesigning / notarization. Out of scope; helper strips quarantine
  xattr post-install so unsigned apps still relaunch.
- SHA / signature verification of downloaded artifacts beyond size.
  HTTPS to GitHub + `assets[*].size` field is the trust boundary.
- Updating relay / CLI agent. Only the desktop binary.
- "Last known partial download" recovery. If the user quits mid-download
  the partial is GC'd; they re-download next time.

## User-facing surface

### Topbar

- The existing settings (`⚙`) icon button gets a small amber dot when
  `state.Available || state.Ready`. No popup, no escalation, no badge
  count. Same `.icon-btn .badge` styling already used by the cast
  button — but rendered as a 6×6 dot, not a number.

### Settings dialog — new "Updates" section

| State                          | Right-side controls                                                       |
|--------------------------------|---------------------------------------------------------------------------|
| Never checked                  | `[ Check now ]`                                                           |
| Currently checking             | `[ Checking… ]` (disabled)                                                |
| Up to date                     | `Up to date · 3 hours ago    [ Check now ]`                               |
| New version available          | `[ Check now ]   [ Download v0.2.0 ]`                                     |
| Downloading                    | `[ Check now ]   [ Downloading… 45% ]` (disabled)                         |
| Ready to install               | `[ Check now ]   [ Force install & restart ]` (accent-colored)            |
| Error                          | red error text + `[ Retry ]`                                              |

Plus a checkbox: `☑ Automatically check for updates` (persisted to
`config.json`, default `true`).

Plus a release-notes panel (collapsed-by-default, shows the GitHub
release `body` rendered as plain text — no full markdown renderer).

### Force-install confirmation modal

Triggered by `[ Force install & restart ]`. Lists what's about to die:

```
Install atterm v0.2.0

atterm will quit and relaunch on the new version. This will:
  • End N local shell session(s)
    (running processes will be terminated)
  • Detach from M remote session(s)
    (the remote PTY keeps running on its host)

Save your work first.

           [ Cancel ]   [ Force install ]
```

Counts come from the live `tabs` model: `localList.length` and the
sum of `pane.remote === true` slots across all tabs. When both are
zero, the wording softens to "atterm will relaunch on the new
version" and the button reads `[ Install & restart ]`.

No second "are you sure" round. The first modal is the consent gate.

## Architecture

```
┌─ desktop/updater.go ──────────────────────┐
│ type Updater struct { … }                 │
│ Methods:                                  │
│   New(currentVersion, repo string)        │
│   Start(ctx context.Context)              │
│       boot Check + 24h ticker             │
│   Stop()                                  │
│   State() UpdateState                     │
│   Check(ctx, force bool) error            │
│   Download(ctx) error                     │
│   InstallAndQuit() error                  │
│ Internal: 1h response cache, asset pick,  │
│ writable-path probe, helper extraction    │
└──────────────┬────────────────────────────┘
               │ Wails bindings
┌──────────────▼────────────────────────────┐
│ desktop/app.go                            │
│   GetUpdateState() UpdateState            │
│   CheckUpdate() error      // force=true  │
│   StartDownload() error                   │
│   InstallUpdate() error                   │
│   GetAutoCheckUpdates() bool              │
│   SetAutoCheckUpdates(enabled bool) error │
└──────────────┬────────────────────────────┘
               │ Frontend polls every 2s
               │ (piggybacks on the existing
               │  pollSessions interval)
┌──────────────▼────────────────────────────┐
│ Frontend                                  │
│   App.vue           — ⚙ badge dot         │
│   SettingsDialog.vue — Updates section    │
│   ConfirmInstallDialog.vue — modal        │
└───────────────────────────────────────────┘
```

`Updater` is owned by `App` for its full lifetime. Created in
`startup`, started after relay host comes up, stopped in `shutdown`.

## Data model

### UpdateState (Go + TS mirror)

```go
type UpdateState struct {
    Current     string  `json:"current"`     // e.g. "v0.1.0", or "dev"
    Latest      string  `json:"latest"`      // e.g. "v0.2.0", "" if unknown
    Available   bool    `json:"available"`   // strictly newer than current
    Notes       string  `json:"notes"`       // GitHub release body
    Checking    bool    `json:"checking"`
    LastCheckAt int64   `json:"last_check_at"` // unix seconds, 0 if never
    Downloading bool    `json:"downloading"`
    DownloadPct int     `json:"download_pct"`  // 0-100
    Ready       bool    `json:"ready"`         // download complete
    Error       string  `json:"error"`         // last error, "" if none
    AssetURL    string  `json:"asset_url"`     // selected asset for current platform
    AssetSize   int64   `json:"asset_size"`
    DownloadDir string  `json:"download_dir"`  // for diagnostics
}
```

### Persisted config (additions to `appConfig`)

```go
type appConfig struct {
    // ...existing
    AutoCheckUpdates *bool  `json:"auto_check_updates,omitempty"` // nil = default true
    LastCheckAt      int64  `json:"last_check_at,omitempty"`
    SkipVersion      string `json:"skip_version,omitempty"`       // reserved for future
}
```

`SkipVersion` isn't wired in v0 (future feature: "skip this release").
Reserved in the schema so we don't bump the file format twice.

## Version source

`desktop/main.go`:

```go
package main

// Version is set at build time via -ldflags -X main.Version=<tag>.
// Empty / "dev" disables the auto-update subsystem.
var Version = "dev"
```

CI sets `Version` from the git ref:

```yaml
- name: build
  working-directory: desktop
  env:
    VERSION: ${{ github.ref_type == 'tag' && github.ref_name || 'dev' }}
  run: wails build -tags webkit2_41 -platform linux/amd64 -s -ldflags "-X main.Version=$VERSION"
```

Same change to all three build jobs (linux, darwin-arm, windows). The
`-tags webkit2_41` is linux-only; darwin/windows drop it.

When `Version == "dev"` (or empty), `Updater.Start` short-circuits to a
disabled state: never polls GitHub, `state.Available` stays false,
Settings UI shows "development build — auto-update disabled", topbar
badge never appears.

## Update check protocol

Endpoint: `GET https://api.github.com/repos/attson/atterm/releases/latest`

Headers:
- `Accept: application/vnd.github+json`
- `User-Agent: atterm-desktop/<Version>` (GitHub requires UA)

Unauthenticated → 60 req/h per source IP. Per-user 24h cadence + 1h
local response cache means we touch GitHub at most ~24/day per user.

Response cache: in-memory (not persisted). `force=true` (manual `Check
now`) bypasses. `force=false` (boot, ticker) respects the 1h freshness.

Asset selection by `runtime.GOOS` × `runtime.GOARCH`:

| Platform        | Asset name                                      |
|-----------------|-------------------------------------------------|
| `linux/amd64`   | `atterm-desktop-linux-amd64.tar.gz`             |
| `darwin/arm64`  | `atterm-desktop-darwin-arm64.zip`               |
| `windows/amd64` | `atterm-desktop-windows-amd64.zip`              |

Other (`darwin/amd64`, `linux/arm64`, etc.) → `state.Error =
"no build for $GOOS/$GOARCH"`. UI shows the error; manual download
link to the Releases page.

Version comparison: `golang.org/x/mod/semver`. `semver.Compare(current,
latest) < 0` ⇒ `Available=true`. Both must be valid (`v` prefix).
Pre-release tags (`v0.2.0-rc1`) are valid semver and would compare,
but releases with `prerelease: true` from GitHub are ignored —
`Available` stays false.

## Download

1. Path: `${UserCacheDir}/atterm/updates/<asset-filename>.partial`
   (e.g. `~/Library/Caches/atterm/updates/`).
2. Streamed write. `state.DownloadPct` updated from `Content-Length`
   accumulator.
3. On clean finish: rename `.partial` → `<asset-filename>`. `state.Ready=true`.
4. Size mismatch (received bytes ≠ `assets[].size`): delete partial,
   `state.Error`, `Ready=false`.
5. Cancellation (app quit mid-download): `connCtx` cancel → write
   stops, partial left on disk and GC'd by next download.

GC: at `Updater.Start`, scan `updates/` and remove anything except the
asset corresponding to `state.Latest` (or the whole dir if no latest).

## Install helper

Three platform-specific scripts, each ~30 lines, embedded via
`go:embed scripts/*` and extracted to a temp file at install time.

### Common contract

Args: `<pid> <src-archive-path> <dst-install-path>`

Behavior:
1. Wait for `<pid>` to exit (max 30s, 0.5s poll).
2. Extract archive into a temp dir.
3. Atomically replace `<dst>` with the extracted file/bundle.
4. Platform-specific post-step (clear quarantine, set +x, etc.).
5. Relaunch the new install.
6. Clean up archive + temp dir.

If any step fails the helper appends to `${UserLogDir}/atterm/install-<pid>.log`
and exits non-zero. The next atterm boot picks up the still-old binary,
sees an out-of-date version, and Settings can show "Last install
attempt failed — view log".

### macOS — `install-darwin.sh`

```bash
#!/bin/bash
set -e
pid=$1; src=$2; dst=$3
log="${HOME}/Library/Logs/atterm/install-${pid}.log"
mkdir -p "$(dirname "$log")"
exec 2>>"$log"

for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
unzip -q "$src" -d "$tmp"
new=$(find "$tmp" -maxdepth 1 -name "*.app" | head -1)
[ -d "$new" ] || { echo "no .app in archive"; exit 1; }

trash="${dst}.old.$$"
mv "$dst" "$trash"
mv "$new" "$dst"
rm -rf "$trash"

xattr -dr com.apple.quarantine "$dst" 2>/dev/null || true

open "$dst"

rm -f "$src"
rm -rf "$tmp"
```

### Linux — `install-linux.sh`

```bash
#!/bin/bash
set -e
pid=$1; src=$2; dst=$3
log="${HOME}/.local/share/atterm/install-${pid}.log"
mkdir -p "$(dirname "$log")"
exec 2>>"$log"

for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
tar -xzf "$src" -C "$tmp"
[ -f "$tmp/atterm-desktop" ] || { echo "atterm-desktop not in archive"; exit 1; }

mv "$tmp/atterm-desktop" "$dst"
chmod +x "$dst"

setsid "$dst" >/dev/null 2>&1 < /dev/null &

rm -f "$src"
rm -rf "$tmp"
```

### Windows — `install-windows.ps1`

```powershell
param(
  [Parameter(Mandatory=$true)][int]$ProcessId,
  [Parameter(Mandatory=$true)][string]$Src,
  [Parameter(Mandatory=$true)][string]$Dst
)

$log = Join-Path $env:LOCALAPPDATA "atterm\install-$ProcessId.log"
New-Item -ItemType Directory -Force -Path (Split-Path $log) | Out-Null
Start-Transcript -Path $log -Append | Out-Null

# Wait for parent atterm to exit
$attempts = 0
while ($attempts -lt 60) {
  if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
  $attempts++
}

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "atterm-update-$([guid]::NewGuid())")
Expand-Archive -Path $Src -DestinationPath $tmp.FullName -Force
$exe = Join-Path $tmp.FullName "atterm-desktop.exe"
if (-not (Test-Path $exe)) { throw "atterm-desktop.exe not in archive" }

# Move-Item can transiently fail if the OS still holds the .exe handle
# (close-after-exit can lag). Retry briefly before giving up.
$moved = $false
for ($i = 0; $i -lt 10; $i++) {
  try {
    Move-Item -Path $exe -Destination $Dst -Force -ErrorAction Stop
    $moved = $true
    break
  } catch {
    Start-Sleep -Milliseconds 500
  }
}
if (-not $moved) { throw "could not replace $Dst (file in use)" }

Start-Process -FilePath $Dst

Remove-Item $Src -Force -ErrorAction SilentlyContinue
Remove-Item $tmp.FullName -Recurse -Force

Stop-Transcript | Out-Null
```

### Spawn / detach

`Updater.InstallAndQuit()`:

1. Extract embedded helper script to
   `${UserCacheDir}/atterm/install-helper.<ext>`. Set +x on POSIX.
2. Detect install path:
   - macOS: walk up from `os.Executable()` until a `.app` parent.
   - Linux: `os.Executable()` directly.
   - Windows: `os.Executable()` directly.
3. Spawn the helper detached:
   - POSIX: `setsid` + `nohup`-style; orphans to init.
   - Windows: `cmd /C start /B powershell -File ...` so the shell
     doesn't die when the parent goes.
4. Pass current PID + asset path + install path as args.
5. Caller (frontend) immediately calls `runtime.Quit(ctx)` (Wails)
   for a graceful shutdown — local PTYs cleanup, uplink sends close.

If the helper exits before the app does (race), no harm — install
fails, next boot detects out-of-date version, retry.

## Error handling

| Scenario                            | Behavior                                                                                       |
|-------------------------------------|------------------------------------------------------------------------------------------------|
| GitHub API unreachable              | `state.Error = "couldn't reach github.com: <details>"`. Retry button. Main app unaffected.     |
| Rate limited (HTTP 403)             | Same as unreachable. 1h cache prevents retry storms.                                           |
| Asset download interrupted          | `.partial` left on disk; user retries. Resumable streaming not supported in v0.                |
| Asset size mismatch                 | Delete partial. Error.                                                                         |
| Helper script extraction fails      | Error. UI offers fallback link to Releases page.                                               |
| Helper exec fails                   | Error. App stays open.                                                                         |
| Helper runs but install fails       | Logged to `${UserLogDir}/atterm/install-<pid>.log`. Next boot still sees out-of-date version.  |
| Confirmation modal canceled         | State stays at `Ready`. Cached download stays on disk. Same button works next time.            |
| Running `dev` build                 | Updater short-circuits. Settings shows "development build — auto-update disabled".             |
| Latest release is `prerelease:true` | Skipped — `Available=false`.                                                                   |
| Current version equals latest       | `Available=false`. UI shows "Up to date".                                                      |
| Install path not writable           | At Check time, probe `os.OpenFile($exe, O_WRONLY)`; if fails, `state.Error="install path not writable"`. Disable Install button. UI offers link to manual download. |
| Multiple atterm windows open        | Helper waits only for the requesting PID. Other windows keep running on the old image until they themselves exit. Stated in design notes; not auto-coordinated in v0. |

## Testing

### Unit (`desktop/updater_test.go`)

- semver compare: zero-pad ordering (`v0.10.0` > `v0.9.0`), pre-release
  ordering (`v1.0.0-beta` < `v1.0.0`), invalid-tag rejection.
- Asset selection: 6 platform combos vs expected filenames; unknown
  combo returns descriptive error.
- `dev` short-circuit: `Updater` with `Version="dev"` never makes a
  network call (mock HTTP transport asserts zero requests).
- Cache: two `Check(force=false)` calls within 1h hit GitHub once.
  `force=true` always hits.
- Prerelease skip: response with `prerelease: true` → `Available=false`.

Mock GitHub via `httptest.Server` returning fixture JSON.

### Integration (`desktop/updater_install_test.go`, linux/darwin only)

- Spin up `httptest.Server` serving a tiny test archive (binary or
  zipped fake bundle).
- Call `Download()`, assert cache file exists with expected bytes.
- Call helper directly with a fake parent PID:
  - Spawn a `sleep 5` as the fake parent.
  - Run helper with that PID + archive path + dst.
  - After helper exits, assert dst contains the expected file.
- Skip on Windows runner; PowerShell variant tested manually.

### Existing test gates

`go vet -tags webkit2_41 ./...` clean. `npm run build` clean. `go test
-tags webkit2_41 -timeout 60s ./desktop/` green (existing
`uplink_e2e_test.go` plus the two new test files).

### Manual smoke (per release)

Pre-flight on a tagged release:

1. Install old version (`v0.1.0`).
2. Push tag for `v0.2.0`. CI builds + uploads assets.
3. Old atterm boots → within ~30s reports v0.2.0 available → ⚙ badge dot.
4. Settings → Updates → "Download v0.2.0" → progress bar runs.
5. "Force install & restart" → confirmation modal lists session counts
   correctly → "Force install".
6. App quits, helper waits for PID, replaces install, relaunches.
7. New atterm reports current = v0.2.0, "Up to date".

Run on linux/amd64, darwin/arm64, windows/amd64.

## Files to change

| #  | File                                                                | Type | Why |
|----|---------------------------------------------------------------------|------|-----|
| 1  | `desktop/main.go`                                                   | M    | `var Version = "dev"` ldflags target |
| 2  | `desktop/updater.go`                                                | + new | Updater core: state machine, GitHub fetch, download, helper invoke |
| 3  | `desktop/updater_test.go`                                           | + new | semver / asset / cache / dev short-circuit |
| 4  | `desktop/updater_install_test.go`                                   | + new | linux/darwin: download + helper integration |
| 5  | `desktop/scripts/install-darwin.sh`                                 | + new | macOS bundle replacement |
| 6  | `desktop/scripts/install-linux.sh`                                  | + new | Linux binary replacement |
| 7  | `desktop/scripts/install-windows.ps1`                               | + new | Windows .exe replacement |
| 8  | `desktop/app.go`                                                    | M    | 5 new bindings + Updater lifecycle wiring |
| 9  | `desktop/config.go`                                                 | M    | `AutoCheckUpdates`, `LastCheckAt`, `SkipVersion` fields |
| 10 | `desktop/frontend/src/lib/api.ts`                                   | M    | New binding wrappers + `UpdateState` TS interface |
| 11 | `desktop/frontend/src/components/SettingsDialog.vue`                | M    | Updates section + auto-check toggle |
| 12 | `desktop/frontend/src/components/ConfirmInstallDialog.vue`          | + new | Force-install confirmation modal |
| 13 | `desktop/frontend/src/App.vue`                                      | M    | ⚙ badge dot binding; emits Settings-open with `tab=updates` |
| 14 | `.github/workflows/build.yml`                                       | M    | All three build jobs: pass `-ldflags -X main.Version=$VERSION` |
| 15 | `go.mod` / `go.sum`                                                 | M    | Add `golang.org/x/mod` for `semver` |

## Out of scope (future)

- "Skip this version" button (config field reserved as `SkipVersion`).
- Pre-release / beta channel toggle.
- Resumable downloads (HTTP Range).
- SHA256 / signature verification (would require publishing checksums
  alongside releases).
- Cross-window install coordination (auto-quit other atterm windows).
- Codesigning + notarization (out of scope; mentioned only because
  helper strips quarantine xattr post-install as a workaround).
- Auto-updating the relay or CLI agent.
