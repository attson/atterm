# Auto-Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual-trigger auto-update subsystem to the desktop app: check GitHub Releases, download the platform asset, and on user-confirmed "force install & restart" replace the binary/bundle and relaunch. Dev builds short-circuit to disabled.

**Architecture:** New `desktop/updater.go` package owning the state machine + GitHub HTTP + download + helper invocation. Three platform helper scripts under `desktop/scripts/` embedded via `go:embed`. Wails bindings on `App` poll-friendly. Frontend adds a Settings "Updates" section, a confirm-install modal, and a tiny dot badge on the existing settings button.

**Tech Stack:** Go 1.23 (`golang.org/x/mod/semver`, `embed`, `os/exec`), Wails v2 runtime (`wailsruntime.Quit`), Vue 3 + TypeScript (existing stack), bash + PowerShell helper scripts.

**Reference spec:** `docs/superpowers/specs/2026-05-10-auto-update-design.md`

---

## Pre-flight

- [ ] **Step 0: Verify clean tree on `main`**

```bash
cd /Users/attson/code/github.com.attson/atterm
git status
git rev-parse --abbrev-ref HEAD
```

Expected: `nothing to commit, working tree clean`, branch `main`.

- [ ] **Step 0.1: Baseline checks pass**

```bash
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
cd desktop/frontend && npm test && npm run build
```

Expected: all clean. If any step fails, stop and fix root cause.

---

## Task 1: Add `golang.org/x/mod` dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1.1: Add dependency**

```bash
cd /Users/attson/code/github.com.attson/atterm
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go get golang.org/x/mod/semver
go mod tidy
```

Expected: `go.mod` gets a `golang.org/x/mod vX.Y.Z` line; `go.sum` updated.

- [ ] **Step 1.2: Verify build still passes**

```bash
go vet -tags webkit2_41 ./...
```

Expected: no errors.

- [ ] **Step 1.3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(go): add golang.org/x/mod for semver compare"
```

---

## Task 2: Version constant in `main.go`

**Files:**
- Modify: `desktop/main.go`

- [ ] **Step 2.1: Add `Version` package var**

Edit `desktop/main.go`. After the imports block, add:

```go
// Version is set at build time via -ldflags -X main.Version=<tag>.
// Empty / "dev" disables the auto-update subsystem (the running build is
// not from a tagged release, so there's no sensible base to compare).
var Version = "dev"
```

- [ ] **Step 2.2: Verify build**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 2.3: Commit**

```bash
git add desktop/main.go
git commit -m "feat(desktop): add Version package var for ldflags injection"
```

---

## Task 3: Updater types + dev short-circuit (TDD)

**Files:**
- Create: `desktop/updater.go`
- Create: `desktop/updater_test.go`

- [ ] **Step 3.1: Write the failing test**

Create `desktop/updater_test.go`:

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRoundTripper fails any request — used to assert "no network calls".
type fakeRoundTripper struct {
	called int
}

func (f *fakeRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.called++
	return nil, http.ErrUseLastResponse
}

func TestUpdater_DevVersion_NeverCallsNetwork(t *testing.T) {
	rt := &fakeRoundTripper{}
	u := newUpdater(updaterConfig{
		current: "dev",
		repo:    "attson/atterm",
		client:  &http.Client{Transport: rt},
	})
	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if rt.called != 0 {
		t.Fatalf("dev version made %d network calls; expected 0", rt.called)
	}
	st := u.State()
	if st.Available {
		t.Fatalf("dev version should never report Available=true")
	}
	if st.Current != "dev" {
		t.Fatalf("State().Current = %q; want %q", st.Current, "dev")
	}
}

func TestUpdater_EmptyVersion_TreatedAsDev(t *testing.T) {
	rt := &fakeRoundTripper{}
	u := newUpdater(updaterConfig{
		current: "",
		repo:    "attson/atterm",
		client:  &http.Client{Transport: rt},
	})
	_ = u.Check(context.Background(), true)
	if rt.called != 0 {
		t.Fatalf("empty version made %d network calls; expected 0", rt.called)
	}
}

// silence-the-unused-import warnings for httptest in later tasks.
var _ = httptest.NewServer
```

- [ ] **Step 3.2: Run tests, expect compile failure**

```bash
cd /Users/attson/code/github.com.attson/atterm
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 -timeout 60s -run TestUpdater ./desktop/
```

Expected: build error like "undefined: newUpdater".

- [ ] **Step 3.3: Implement minimal Updater**

Create `desktop/updater.go`:

```go
package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// UpdateState is the observable view of the auto-update subsystem.
// Mirrored as a TypeScript interface in desktop/frontend/src/lib/api.ts.
type UpdateState struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	Checking    bool   `json:"checking"`
	LastCheckAt int64  `json:"last_check_at"`
	Downloading bool   `json:"downloading"`
	DownloadPct int    `json:"download_pct"`
	Ready       bool   `json:"ready"`
	Error       string `json:"error"`
	AssetURL    string `json:"asset_url"`
	AssetSize   int64  `json:"asset_size"`
	DownloadDir string `json:"download_dir"`
}

// updaterConfig is the constructor-time bag for Updater. Test code
// substitutes the http.Client and now() to drive the cache + network paths
// without hitting GitHub or wall-clock time.
type updaterConfig struct {
	current string
	repo    string // e.g. "attson/atterm"
	client  *http.Client
	now     func() time.Time // optional; defaults to time.Now
}

// Updater owns state for the auto-update flow. All methods are goroutine-safe.
type Updater struct {
	cfg updaterConfig

	mu       sync.Mutex
	state    UpdateState
	cachedAt time.Time // when we last fetched the latest-release manifest
}

func newUpdater(cfg updaterConfig) *Updater {
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Updater{
		cfg:   cfg,
		state: UpdateState{Current: cfg.current},
	}
}

// State returns a snapshot of the current state.
func (u *Updater) State() UpdateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// devOrEmpty reports whether we should short-circuit out of all update logic.
func (u *Updater) devOrEmpty() bool {
	return u.cfg.current == "" || u.cfg.current == "dev"
}

// Check fetches the latest release. force=true bypasses the 1h response cache.
// In dev/empty builds it's a no-op that never touches the network.
func (u *Updater) Check(ctx context.Context, force bool) error {
	if u.devOrEmpty() {
		return nil
	}
	// real implementation lands in Task 5
	return nil
}
```

- [ ] **Step 3.4: Run tests, expect pass**

```bash
go test -tags webkit2_41 -timeout 60s -run TestUpdater ./desktop/
```

Expected: `PASS`.

- [ ] **Step 3.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): UpdateState + dev/empty short-circuit"
```

---

## Task 4: Asset selection by GOOS/GOARCH (TDD)

**Files:**
- Modify: `desktop/updater.go`
- Modify: `desktop/updater_test.go`

- [ ] **Step 4.1: Append failing tests**

Append to `desktop/updater_test.go`:

```go
func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "AT-Term-linux-amd64.tar.gz"},
		{"darwin", "arm64", "AT-Term-darwin-arm64.zip"},
		{"windows", "amd64", "AT-Term-windows-amd64.zip"},
	}
	for _, c := range cases {
		got, err := assetNameForPlatform(c.goos, c.goarch)
		if err != nil {
			t.Errorf("assetNameForPlatform(%q,%q) err = %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("assetNameForPlatform(%q,%q) = %q; want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestAssetNameForPlatform_Unsupported(t *testing.T) {
	cases := []struct{ goos, goarch string }{
		{"darwin", "amd64"},
		{"linux", "arm64"},
		{"freebsd", "amd64"},
	}
	for _, c := range cases {
		_, err := assetNameForPlatform(c.goos, c.goarch)
		if err == nil {
			t.Errorf("expected error for %s/%s", c.goos, c.goarch)
		}
	}
}
```

- [ ] **Step 4.2: Run tests, verify failure**

```bash
go test -tags webkit2_41 -timeout 60s -run TestAssetNameForPlatform ./desktop/
```

Expected: build error `undefined: assetNameForPlatform`.

- [ ] **Step 4.3: Implement**

Append to `desktop/updater.go` (just before the `func (u *Updater) Check` line):

```go
// assetNameForPlatform returns the GitHub Release asset filename for the
// given runtime.GOOS/GOARCH pair. Unsupported pairs return an error so the
// UI can display "no build for $platform" instead of silently picking a
// wrong artifact.
func assetNameForPlatform(goos, goarch string) (string, error) {
	switch {
	case goos == "linux" && goarch == "amd64":
		return "AT-Term-linux-amd64.tar.gz", nil
	case goos == "darwin" && goarch == "arm64":
		return "AT-Term-darwin-arm64.zip", nil
	case goos == "windows" && goarch == "amd64":
		return "AT-Term-windows-amd64.zip", nil
	}
	return "", fmt.Errorf("no atterm build for %s/%s", goos, goarch)
}
```

Add `"fmt"` to the import block at the top of `updater.go`.

- [ ] **Step 4.4: Run tests, expect pass**

```bash
go test -tags webkit2_41 -timeout 60s -run TestAssetNameForPlatform ./desktop/
```

Expected: `PASS`.

- [ ] **Step 4.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): asset name selection per GOOS/GOARCH"
```

---

## Task 5: GitHub release fetch + 1h cache + semver compare (TDD)

**Files:**
- Modify: `desktop/updater.go`
- Modify: `desktop/updater_test.go`

- [ ] **Step 5.1: Append failing tests**

Append to `desktop/updater_test.go`:

```go
import "fmt"
import "encoding/json"

// helper: spin up a fake GitHub releases endpoint that returns the supplied
// payload and counts requests.
func fakeGitHub(t *testing.T, payload any) (string, *int) {
	t.Helper()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func releasePayload(tag string, prerelease bool) map[string]any {
	return map[string]any{
		"tag_name":   tag,
		"name":       tag,
		"body":       "release notes for " + tag,
		"prerelease": prerelease,
		"assets": []map[string]any{
			{
				"name":                 "AT-Term-darwin-arm64.zip",
				"browser_download_url": "https://example.com/" + tag + "/darwin.zip",
				"size":                 int64(12345),
			},
			{
				"name":                 "AT-Term-linux-amd64.tar.gz",
				"browser_download_url": "https://example.com/" + tag + "/linux.tar.gz",
				"size":                 int64(54321),
			},
			{
				"name":                 "AT-Term-windows-amd64.zip",
				"browser_download_url": "https://example.com/" + tag + "/windows.zip",
				"size":                 int64(99999),
			},
		},
	}
}

func TestUpdater_Check_NewVersionAvailable(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.2.0", false))
	u := newUpdater(updaterConfig{
		current:    "v0.1.0",
		repo:       "attson/atterm",
		releaseURL: apiURL,
	})
	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check err: %v", err)
	}
	st := u.State()
	if !st.Available {
		t.Errorf("Available = false; want true (v0.2.0 > v0.1.0)")
	}
	if st.Latest != "v0.2.0" {
		t.Errorf("Latest = %q; want v0.2.0", st.Latest)
	}
	if st.Notes == "" {
		t.Errorf("Notes empty")
	}
}

func TestUpdater_Check_UpToDate(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.1.0", false))
	u := newUpdater(updaterConfig{
		current:    "v0.1.0",
		repo:       "attson/atterm",
		releaseURL: apiURL,
	})
	_ = u.Check(context.Background(), true)
	st := u.State()
	if st.Available {
		t.Errorf("Available = true; want false (same version)")
	}
}

func TestUpdater_Check_PrereleaseSkipped(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.2.0", true))
	u := newUpdater(updaterConfig{
		current:    "v0.1.0",
		repo:       "attson/atterm",
		releaseURL: apiURL,
	})
	_ = u.Check(context.Background(), true)
	st := u.State()
	if st.Available {
		t.Errorf("Available = true on prerelease; want false")
	}
}

func TestUpdater_Check_CacheRespected(t *testing.T) {
	apiURL, hits := fakeGitHub(t, releasePayload("v0.2.0", false))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	u := newUpdater(updaterConfig{
		current:    "v0.1.0",
		repo:       "attson/atterm",
		releaseURL: apiURL,
		now:        func() time.Time { return now },
	})
	_ = u.Check(context.Background(), false)
	_ = u.Check(context.Background(), false) // within cache window
	if *hits != 1 {
		t.Errorf("hits = %d; want 1 (second Check should be cached)", *hits)
	}
}

func TestUpdater_Check_ForceBypassesCache(t *testing.T) {
	apiURL, hits := fakeGitHub(t, releasePayload("v0.2.0", false))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	u := newUpdater(updaterConfig{
		current:    "v0.1.0",
		repo:       "attson/atterm",
		releaseURL: apiURL,
		now:        func() time.Time { return now },
	})
	_ = u.Check(context.Background(), true)
	_ = u.Check(context.Background(), true) // force=true bypasses cache
	if *hits != 2 {
		t.Errorf("hits = %d; want 2 (force should always fetch)", *hits)
	}
}
```

Add `"time"` to the test file's import block (alongside the existing `"testing"`).

- [ ] **Step 5.2: Run tests, expect failure**

```bash
go test -tags webkit2_41 -timeout 60s -run "TestUpdater_Check|TestAsset" ./desktop/
```

Expected: build error (`releaseURL` field not defined; `Check` not yet implemented).

- [ ] **Step 5.3: Implement Check + cache + semver**

Edit `desktop/updater.go`. Add `releaseURL string` to `updaterConfig`:

```go
type updaterConfig struct {
	current    string
	repo       string
	releaseURL string // override of https://api.github.com/repos/<repo>/releases/latest, for tests
	client     *http.Client
	now        func() time.Time
}
```

Replace the placeholder `Check` body with the real implementation:

```go
const releaseCacheTTL = 1 * time.Hour

// githubReleaseAPI returns the URL to fetch the latest release manifest.
func (u *Updater) githubReleaseAPI() string {
	if u.cfg.releaseURL != "" {
		return u.cfg.releaseURL
	}
	return "https://api.github.com/repos/" + u.cfg.repo + "/releases/latest"
}

// githubAsset is the subset of the release-asset JSON we care about.
type githubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

// githubRelease is the subset of the release JSON we care about.
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Body       string        `json:"body"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

func (u *Updater) Check(ctx context.Context, force bool) error {
	if u.devOrEmpty() {
		return nil
	}

	u.mu.Lock()
	if !force && !u.cachedAt.IsZero() && u.cfg.now().Sub(u.cachedAt) < releaseCacheTTL {
		u.mu.Unlock()
		return nil
	}
	u.state.Checking = true
	u.state.Error = ""
	u.mu.Unlock()

	rel, err := u.fetchLatest(ctx)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Checking = false
	u.state.LastCheckAt = u.cfg.now().Unix()
	if err != nil {
		u.state.Error = err.Error()
		return err
	}
	u.cachedAt = u.cfg.now()

	if rel.Prerelease {
		// Don't expose pre-releases as "available" in v0.
		u.state.Latest = ""
		u.state.Available = false
		u.state.Notes = ""
		u.state.AssetURL = ""
		u.state.AssetSize = 0
		return nil
	}

	u.state.Latest = rel.TagName
	u.state.Notes = rel.Body
	u.state.Available = semver.IsValid(rel.TagName) &&
		semver.IsValid(u.cfg.current) &&
		semver.Compare(u.cfg.current, rel.TagName) < 0

	// Pick the asset matching this platform; ignore failure (state stays
	// without an asset URL — UI surfaces the error separately).
	if name, perr := assetNameForPlatform(runtime.GOOS, runtime.GOARCH); perr == nil {
		for _, a := range rel.Assets {
			if a.Name == name {
				u.state.AssetURL = a.DownloadURL
				u.state.AssetSize = a.Size
				break
			}
		}
	}
	return nil
}

func (u *Updater) fetchLatest(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.githubReleaseAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AT Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github returned http %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}
```

Update the import block at the top of `updater.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)
```

- [ ] **Step 5.4: Run tests, expect pass**

```bash
go test -tags webkit2_41 -timeout 60s -run "TestUpdater|TestAsset" ./desktop/
```

Expected: all pass.

- [ ] **Step 5.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): GitHub release fetch + semver compare + 1h cache"
```

---

## Task 6: Helper scripts as embedded files

**Files:**
- Create: `desktop/scripts/install-darwin.sh`
- Create: `desktop/scripts/install-linux.sh`
- Create: `desktop/scripts/install-windows.ps1`
- Modify: `desktop/updater.go`

- [ ] **Step 6.1: Create directory + scripts**

```bash
mkdir -p /Users/attson/code/github.com.attson/atterm/desktop/scripts
```

Write `desktop/scripts/install-darwin.sh`:

```bash
#!/bin/bash
# atterm auto-update install helper for macOS.
# Args: <pid> <src-archive> <dst-bundle>
set -e
pid=$1
src=$2
dst=$3

log_dir="${HOME}/Library/Logs/atterm"
log="${log_dir}/install-${pid}.log"
mkdir -p "$log_dir"
exec 2>>"$log"

# Wait for parent atterm to exit (cap 30s, 0.5s poll).
for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
unzip -q "$src" -d "$tmp"
new=$(find "$tmp" -maxdepth 1 -name "*.app" | head -1)
[ -d "$new" ] || { echo "no .app bundle in archive"; exit 1; }

trash="${dst}.old.$$"
mv "$dst" "$trash"
mv "$new" "$dst"
rm -rf "$trash"

# Strip macOS quarantine xattr so Gatekeeper doesn't re-prompt the user.
xattr -dr com.apple.quarantine "$dst" 2>/dev/null || true

open "$dst"

rm -f "$src"
rm -rf "$tmp"
```

Write `desktop/scripts/install-linux.sh`:

```bash
#!/bin/bash
# atterm auto-update install helper for Linux.
# Args: <pid> <src-archive> <dst-binary>
set -e
pid=$1
src=$2
dst=$3

log_dir="${HOME}/.local/share/atterm"
log="${log_dir}/install-${pid}.log"
mkdir -p "$log_dir"
exec 2>>"$log"

for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
tar -xzf "$src" -C "$tmp"
[ -f "$tmp/AT Term" ] || { echo "AT Term not in archive"; exit 1; }

mv "$tmp/AT Term" "$dst"
chmod +x "$dst"

# Detach the relaunched process from this script.
setsid "$dst" >/dev/null 2>&1 < /dev/null &

rm -f "$src"
rm -rf "$tmp"
```

Write `desktop/scripts/install-windows.ps1`:

```powershell
# atterm auto-update install helper for Windows.
# Args: -ProcessId <pid> -Src <archive> -Dst <exe-path>
param(
  [Parameter(Mandatory=$true)][int]$ProcessId,
  [Parameter(Mandatory=$true)][string]$Src,
  [Parameter(Mandatory=$true)][string]$Dst
)

$log = Join-Path $env:LOCALAPPDATA "atterm\install-$ProcessId.log"
New-Item -ItemType Directory -Force -Path (Split-Path $log) | Out-Null
Start-Transcript -Path $log -Append | Out-Null

$attempts = 0
while ($attempts -lt 60) {
  if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
  $attempts++
}

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "atterm-update-$([guid]::NewGuid())")
Expand-Archive -Path $Src -DestinationPath $tmp.FullName -Force
$exe = Join-Path $tmp.FullName "AT Term.exe"
if (-not (Test-Path $exe)) { throw "AT Term.exe not in archive" }

# Move-Item can transiently fail if Windows still holds the .exe handle
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

```bash
chmod +x /Users/attson/code/github.com.attson/atterm/desktop/scripts/install-darwin.sh
chmod +x /Users/attson/code/github.com.attson/atterm/desktop/scripts/install-linux.sh
```

- [ ] **Step 6.2: Embed via go:embed**

Append to `desktop/updater.go` (top-level, after the imports):

```go
import "embed"
```

And add (top-level, can go below the imports):

```go
//go:embed scripts/install-darwin.sh
//go:embed scripts/install-linux.sh
//go:embed scripts/install-windows.ps1
var installScripts embed.FS
```

Move the `import "embed"` into the existing import block instead of a separate line — final imports:

```go
import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)
```

- [ ] **Step 6.3: Verify build**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 6.4: Commit**

```bash
git add desktop/scripts/ desktop/updater.go
git commit -m "feat(updater): embed per-platform install helper scripts"
```

---

## Task 7: Download to cache dir (TDD)

**Files:**
- Modify: `desktop/updater.go`
- Modify: `desktop/updater_test.go`

- [ ] **Step 7.1: Append failing tests**

Append to `desktop/updater_test.go`:

```go
import "io"
import "os"
import "path/filepath"

func TestUpdater_Download_WritesAtomicAsset(t *testing.T) {
	body := []byte("fake-archive-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: tmpCache,
	})
	// Pretend Check has already populated state.
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len(body))
	u.state.Latest = "v0.2.0"

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true after successful download")
	}
	// File should exist in cache, NOT a .partial.
	matches, _ := filepath.Glob(filepath.Join(tmpCache, "atterm", "updates", "*"))
	var nonPartial []string
	for _, m := range matches {
		if filepath.Ext(m) != ".partial" {
			nonPartial = append(nonPartial, m)
		}
	}
	if len(nonPartial) != 1 {
		t.Errorf("expected 1 finished file in cache; got %v", matches)
	}
	got, _ := os.ReadFile(nonPartial[0])
	if string(got) != string(body) {
		t.Errorf("file content = %q; want %q", string(got), string(body))
	}
}

func TestUpdater_Download_SizeMismatch(t *testing.T) {
	body := []byte("short")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: tmpCache,
	})
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len(body) + 100) // wrong on purpose
	u.state.Latest = "v0.2.0"

	if err := u.Download(context.Background()); err == nil {
		t.Errorf("Download err = nil; want size-mismatch error")
	}
	st := u.State()
	if st.Ready {
		t.Errorf("Ready = true after size mismatch; want false")
	}
	if st.Error == "" {
		t.Errorf("Error = empty after size mismatch")
	}
	// The .partial should be cleaned up.
	matches, _ := filepath.Glob(filepath.Join(tmpCache, "atterm", "updates", "*"))
	if len(matches) != 0 {
		t.Errorf("expected cleanup of partial; got %v", matches)
	}
}

// silence unused-import warning for io
var _ = io.Discard
```

- [ ] **Step 7.2: Run tests, expect failure**

```bash
go test -tags webkit2_41 -timeout 60s -run TestUpdater_Download ./desktop/
```

Expected: `cacheDir` undefined; `Download` undefined.

- [ ] **Step 7.3: Implement Download**

Edit `desktop/updater.go`:

Add `cacheDir string` to `updaterConfig`:

```go
type updaterConfig struct {
	current    string
	repo       string
	releaseURL string
	cacheDir   string // overrides os.UserCacheDir(); for tests
	client     *http.Client
	now        func() time.Time
}
```

Add a constructor helper that resolves the default:

```go
func (u *Updater) updatesDir() (string, error) {
	base := u.cfg.cacheDir
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "atterm", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
```

Add `Download`:

```go
// Download fetches the asset URL recorded in state to the cache dir,
// streaming through a .partial file before atomic-renaming on success.
// Reports size-mismatch errors loudly so the UI can offer Retry.
func (u *Updater) Download(ctx context.Context) error {
	u.mu.Lock()
	url := u.state.AssetURL
	expectedSize := u.state.AssetSize
	latest := u.state.Latest
	u.mu.Unlock()
	if url == "" {
		return fmt.Errorf("no asset URL — Check first")
	}

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
	final := filepath.Join(dir, latest+"-"+name)
	partial := final + ".partial"

	u.mu.Lock()
	u.state.Downloading = true
	u.state.DownloadPct = 0
	u.state.Ready = false
	u.state.Error = ""
	u.state.DownloadDir = dir
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.state.Downloading = false
		u.mu.Unlock()
	}()

	// Network fetch.
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		u.recordError(err)
		return err
	}
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		u.recordError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err := fmt.Errorf("download http %d", resp.StatusCode)
		u.recordError(err)
		return err
	}

	out, err := os.Create(partial)
	if err != nil {
		u.recordError(err)
		return err
	}

	written, copyErr := u.copyWithProgress(out, resp.Body, expectedSize)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		u.recordError(copyErr)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		u.recordError(closeErr)
		return closeErr
	}

	if expectedSize > 0 && written != expectedSize {
		_ = os.Remove(partial)
		err := fmt.Errorf("download size mismatch: got %d bytes, expected %d", written, expectedSize)
		u.recordError(err)
		return err
	}

	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		u.recordError(err)
		return err
	}

	u.mu.Lock()
	u.state.Ready = true
	u.state.DownloadPct = 100
	u.state.Error = ""
	u.mu.Unlock()
	return nil
}

func (u *Updater) copyWithProgress(dst io.Writer, src io.Reader, expectedSize int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
			u.mu.Lock()
			if expectedSize > 0 {
				u.state.DownloadPct = int(written * 100 / expectedSize)
			}
			u.mu.Unlock()
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

func (u *Updater) recordError(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Error = err.Error()
	u.state.Ready = false
}
```

Update imports:

```go
import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)
```

- [ ] **Step 7.4: Run tests, expect pass**

```bash
go test -tags webkit2_41 -timeout 60s -run TestUpdater ./desktop/
```

Expected: all `TestUpdater*` pass.

- [ ] **Step 7.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): streamed download with .partial atomic rename + size check"
```

---

## Task 8: Install path detection + writability probe (TDD)

**Files:**
- Modify: `desktop/updater.go`
- Modify: `desktop/updater_test.go`

- [ ] **Step 8.1: Append failing tests**

Append to `desktop/updater_test.go`:

```go
func TestInstallPathFromExecutable_Darwin(t *testing.T) {
	got := installPathFromExecutable("/Applications/AT Term.app/Contents/MacOS/AT Term", "darwin")
	want := "/Applications/AT Term.app"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_DarwinDeepPath(t *testing.T) {
	got := installPathFromExecutable("/Users/x/Applications/AT Term.app/Contents/MacOS/AT Term", "darwin")
	want := "/Users/x/Applications/AT Term.app"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_Linux(t *testing.T) {
	got := installPathFromExecutable("/home/x/.local/bin/AT Term", "linux")
	want := "/home/x/.local/bin/AT Term"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_Windows(t *testing.T) {
	got := installPathFromExecutable(`C:\Users\x\AT Term.exe`, "windows")
	want := `C:\Users\x\AT Term.exe`
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}
```

- [ ] **Step 8.2: Run tests, expect failure**

```bash
go test -tags webkit2_41 -timeout 60s -run TestInstallPath ./desktop/
```

Expected: `undefined: installPathFromExecutable`.

- [ ] **Step 8.3: Implement**

Append to `desktop/updater.go`:

```go
// installPathFromExecutable maps os.Executable() output to the path the
// install helper must replace. On macOS the running binary lives inside
// .app/Contents/MacOS/, but the helper replaces the .app bundle as a
// whole — walk back to it.
func installPathFromExecutable(exe, goos string) string {
	if goos == "darwin" {
		// Walk parents until we hit a directory ending in ".app". Robust
		// against alternate install locations (~/Applications, /Applications,
		// /opt/atterm/Applications/...).
		dir := exe
		for {
			parent := filepath.Dir(dir)
			if parent == dir {
				return exe // not in a bundle; fall back to executable path
			}
			if filepath.Ext(parent) == ".app" {
				return parent
			}
			dir = parent
		}
	}
	return exe
}
```

- [ ] **Step 8.4: Run tests**

```bash
go test -tags webkit2_41 -timeout 60s -run TestInstallPath ./desktop/
```

Expected: pass.

- [ ] **Step 8.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): install path resolver (.app walk-back on macOS)"
```

---

## Task 9: InstallAndQuit — extract helper + spawn detached

**Files:**
- Modify: `desktop/updater.go`

- [ ] **Step 9.1: Implement**

Append to `desktop/updater.go`:

```go
import (
	"os/exec"
	"strconv"
)
```

Add to the existing import block (i.e. update the import block, don't add a new one):

```go
import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)
```

Append the install logic:

```go
// helperResource picks the embedded script + extension for the running OS.
func helperResource(goos string) (path string, ext string, ok bool) {
	switch goos {
	case "darwin":
		return "scripts/install-darwin.sh", ".sh", true
	case "linux":
		return "scripts/install-linux.sh", ".sh", true
	case "windows":
		return "scripts/install-windows.ps1", ".ps1", true
	}
	return "", "", false
}

// extractHelper writes the embedded helper for the current OS to a fresh
// temp file. POSIX scripts get +x.
func (u *Updater) extractHelper() (string, error) {
	src, ext, ok := helperResource(runtime.GOOS)
	if !ok {
		return "", fmt.Errorf("no install helper for %s", runtime.GOOS)
	}
	body, err := installScripts.ReadFile(src)
	if err != nil {
		return "", err
	}
	dir, err := u.updatesDir()
	if err != nil {
		return "", err
	}
	helperPath := filepath.Join(dir, "install-helper-"+strconv.Itoa(os.Getpid())+ext)
	mode := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		mode = 0o755
	}
	if err := os.WriteFile(helperPath, body, mode); err != nil {
		return "", err
	}
	return helperPath, nil
}

// archivePath returns the cache path of the most recently downloaded asset.
func (u *Updater) archivePath() (string, error) {
	dir, err := u.updatesDir()
	if err != nil {
		return "", err
	}
	u.mu.Lock()
	latest := u.state.Latest
	u.mu.Unlock()
	if latest == "" {
		return "", fmt.Errorf("no version downloaded")
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, latest+"-"+name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("download not found at %s: %w", p, err)
	}
	return p, nil
}

// InstallAndQuit spawns the install helper detached, then returns. The
// caller (App binding layer) is responsible for calling Wails runtime.Quit
// next so the helper can take over after our process exits.
//
// Returns immediately on error; the app stays alive in that case so the
// UI can surface what went wrong.
func (u *Updater) InstallAndQuit() error {
	if u.devOrEmpty() {
		return fmt.Errorf("auto-update disabled in dev builds")
	}
	u.mu.Lock()
	if !u.state.Ready {
		u.mu.Unlock()
		return fmt.Errorf("nothing downloaded yet")
	}
	u.mu.Unlock()

	src, err := u.archivePath()
	if err != nil {
		u.recordError(err)
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		u.recordError(err)
		return err
	}
	dst := installPathFromExecutable(exe, runtime.GOOS)

	helperPath, err := u.extractHelper()
	if err != nil {
		u.recordError(err)
		return err
	}

	pid := strconv.Itoa(os.Getpid())
	cmd := buildHelperCommand(helperPath, pid, src, dst)

	if err := cmd.Start(); err != nil {
		u.recordError(err)
		return err
	}
	// Detach: don't Wait(). On POSIX, Setpgid+Setsid would be ideal but Go
	// inherits parent's process group anyway; the helper survives our exit
	// because it's invoked via `nohup`-style wrappers on POSIX (see
	// buildHelperCommand) and Start-Process on Windows.
	return nil
}
```

Add `buildHelperCommand` (cross-platform) at the end of `desktop/updater.go`:

```go
// buildHelperCommand wraps the helper script in a platform-appropriate way
// so it survives our exit:
//   - POSIX: invoke via `setsid bash <script> ...` so it ends up in its
//     own session, detached from this process.
//   - Windows: `powershell -NoProfile -ExecutionPolicy Bypass -File <ps1>
//     -ProcessId <pid> -Src <src> -Dst <dst>`, started via cmd /C start
//     /B so it survives.
func buildHelperCommand(helperPath, pid, src, dst string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin", "linux":
		// `setsid` may not exist on macOS-without-coreutils, so fall back
		// to bash + nohup-style backgrounding inside the script. Here we
		// just exec bash with the script.
		return exec.Command("/bin/bash", helperPath, pid, src, dst)
	case "windows":
		return exec.Command(
			"powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
			"-File", helperPath,
			"-ProcessId", pid,
			"-Src", src,
			"-Dst", dst,
		)
	}
	// Should be unreachable — extractHelper already errored.
	return exec.Command("false")
}
```

> Note: POSIX detach is achieved by the helper itself — the macOS script
> uses `open` for the relaunch (which spawns a fresh GUI process not tied
> to our process group), and the Linux script uses `setsid <dst> &` for
> the same effect. Windows's `Start-Process` inherits no console.

- [ ] **Step 9.2: Verify build**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 9.3: Commit**

```bash
git add desktop/updater.go
git commit -m "feat(updater): InstallAndQuit — extract helper + spawn detached"
```

---

## Task 10: Background ticker + Start/Stop lifecycle

**Files:**
- Modify: `desktop/updater.go`
- Modify: `desktop/updater_test.go`

- [ ] **Step 10.1: Append failing test**

Append to `desktop/updater_test.go`:

```go
func TestUpdater_StartStop_NoLeakedGoroutines(t *testing.T) {
	rt := &fakeRoundTripper{}
	u := newUpdater(updaterConfig{
		current: "dev", // skip network
		repo:    "attson/atterm",
		client:  &http.Client{Transport: rt},
	})
	ctx, cancel := context.WithCancel(context.Background())
	u.Start(ctx)
	cancel()
	u.Stop()
	// If Stop() blocks forever, the test runner times out and we know.
}
```

- [ ] **Step 10.2: Run tests, expect failure**

```bash
go test -tags webkit2_41 -timeout 60s -run TestUpdater_StartStop ./desktop/
```

Expected: `undefined: Start` / `Stop`.

- [ ] **Step 10.3: Implement**

Append to `desktop/updater.go`:

```go
const checkInterval = 24 * time.Hour

// Start launches a background goroutine that runs Check() once on boot,
// then every 24h. Idempotent — calling Start twice is a no-op for the
// second call.
func (u *Updater) Start(ctx context.Context) {
	u.mu.Lock()
	if u.cancelLoop != nil {
		u.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	u.cancelLoop = cancel
	u.mu.Unlock()

	if u.devOrEmpty() {
		// Don't bother spinning the goroutine for dev builds.
		return
	}

	go func() {
		// Boot check: kick off after a brief delay so the rest of startup
		// finishes first (relay host, polling, etc.).
		select {
		case <-loopCtx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		_ = u.Check(loopCtx, false)

		t := time.NewTicker(checkInterval)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				_ = u.Check(loopCtx, false)
			}
		}
	}()
}

// Stop cancels the background loop. Safe to call multiple times.
func (u *Updater) Stop() {
	u.mu.Lock()
	cancel := u.cancelLoop
	u.cancelLoop = nil
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
```

Add the field to `Updater` struct:

```go
type Updater struct {
	cfg updaterConfig

	mu         sync.Mutex
	state      UpdateState
	cachedAt   time.Time
	cancelLoop context.CancelFunc
}
```

- [ ] **Step 10.4: Run tests**

```bash
go test -tags webkit2_41 -timeout 60s -run TestUpdater ./desktop/
```

Expected: all pass.

- [ ] **Step 10.5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): background Start/Stop loop with 24h check cadence"
```

---

## Task 11: Config — `AutoCheckUpdates` field

**Files:**
- Modify: `desktop/config.go`

- [ ] **Step 11.1: Extend appConfig**

Edit `desktop/config.go`. Replace the `appConfig` struct:

```go
// appConfig is what we persist to ~/.config/atterm/config.json.
// Empty fields mean "not configured" — RelayURL == "" disables uplink entirely.
type appConfig struct {
	RelayURL   string `json:"relay_url,omitempty"`
	RelayToken string `json:"relay_token,omitempty"`

	// Auto-update settings. Nil means "never set" → treated as default true
	// at read time. Stored as a pointer so we can distinguish "user opted
	// out" from "fresh install".
	AutoCheckUpdates *bool  `json:"auto_check_updates,omitempty"`
	LastCheckAt      int64  `json:"last_check_at,omitempty"`
	SkipVersion      string `json:"skip_version,omitempty"`
}
```

Add a getter at the end of `desktop/config.go`:

```go
// AutoCheckUpdatesOrDefault returns the user's preference, defaulting to
// true when the field has never been set (fresh installs).
func (c appConfig) AutoCheckUpdatesOrDefault() bool {
	if c.AutoCheckUpdates == nil {
		return true
	}
	return *c.AutoCheckUpdates
}
```

- [ ] **Step 11.2: Verify build**

```bash
go vet -tags webkit2_41 ./...
```

Expected: clean. (No callers yet — added in Task 12.)

- [ ] **Step 11.3: Commit**

```bash
git add desktop/config.go
git commit -m "feat(config): persisted AutoCheckUpdates / LastCheckAt / SkipVersion"
```

---

## Task 12: Wails bindings on `App`

**Files:**
- Modify: `desktop/app.go`

- [ ] **Step 12.1: Add `updater` field**

Edit `desktop/app.go`. Replace the existing `App` struct (currently
`desktop/app.go:55-64`):

```go
// App is the Wails-bound application surface.
type App struct {
	ctx      context.Context
	host     *relayHost
	cfgStore *configStore

	mu           sync.Mutex
	uplink       *uplink
	uplinkCancel context.CancelFunc

	updater *Updater
}
```

- [ ] **Step 12.2: Construct updater in `NewApp`**

Replace `NewApp` (currently `desktop/app.go:67-69`):

```go
// NewApp creates a new App application struct.
func NewApp() *App {
	a := &App{}
	a.updater = newUpdater(updaterConfig{
		current: Version,
		repo:    "attson/atterm",
	})
	return a
}
```

- [ ] **Step 12.3: Start updater in `startup`**

Replace `startup` (currently `desktop/app.go:75-92`):

```go
// startup is called when the Wails runtime is ready. Boot the in-process
// relay, load persisted config, and apply it (which may start the uplink).
// ATTERM_RELAY_URL/TOKEN env vars are honored only when no config file
// exists yet — they seed the first run.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	h, err := startRelayHost()
	if err != nil {
		log.Fatalf("desktop: start relay host: %v", err)
	}
	a.host = h
	a.cfgStore = loadConfig()

	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" {
		if env := strings.TrimSpace(os.Getenv("ATTERM_RELAY_URL")); env != "" {
			cfg.RelayURL = env
			cfg.RelayToken = strings.TrimSpace(os.Getenv("ATTERM_RELAY_TOKEN"))
		}
	}
	a.applyRelayConfig(cfg)

	// Auto-update background loop, gated on the persisted preference.
	// New installs default to enabled (AutoCheckUpdatesOrDefault returns true).
	if a.updater != nil && cfg.AutoCheckUpdatesOrDefault() {
		a.updater.Start(ctx)
	}
}
```

- [ ] **Step 12.4: Stop updater in `shutdown`**

Replace `shutdown` (currently `desktop/app.go:95-106`):

```go
// shutdown is called when the window is closed; clean up PTYs and HTTP server.
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	if a.uplinkCancel != nil {
		a.uplinkCancel()
		a.uplinkCancel = nil
	}
	a.mu.Unlock()
	if a.updater != nil {
		a.updater.Stop()
	}
	if a.host != nil {
		a.host.Stop()
		a.host = nil
	}
}
```

- [ ] **Step 12.5: Add 6 bindings**

Append to `desktop/app.go`:

```go
// GetUpdateState returns the current updater state. The frontend polls
// this from its existing 2s session-poll loop.
func (a *App) GetUpdateState() UpdateState {
	if a.updater == nil {
		return UpdateState{Current: "dev"}
	}
	return a.updater.State()
}

// CheckUpdate forces a fresh GitHub fetch, bypassing the 1h cache.
// Triggered by Settings > Updates > "Check now".
func (a *App) CheckUpdate() error {
	if a.updater == nil {
		return nil
	}
	return a.updater.Check(a.ctx, true)
}

// StartDownload begins fetching the platform asset to the cache dir.
// Idempotent if already running.
func (a *App) StartDownload() error {
	if a.updater == nil {
		return nil
	}
	return a.updater.Download(a.ctx)
}

// InstallUpdate spawns the install helper detached and returns. The
// frontend should call wailsruntime.Quit() (via wails-Quit binding) right
// after this; the helper waits for our PID to exit then replaces the
// install and relaunches.
func (a *App) InstallUpdate() error {
	if a.updater == nil {
		return fmt.Errorf("updater not initialized")
	}
	if err := a.updater.InstallAndQuit(); err != nil {
		return err
	}
	// Quit ourselves so the helper's wait-for-PID-exit loop unblocks. We
	// run this in a goroutine because Quit terminates the runtime.
	go func() {
		// Tiny delay so this RPC return reaches the frontend before we exit.
		time.Sleep(200 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// GetAutoCheckUpdates reports the persisted preference (default true).
func (a *App) GetAutoCheckUpdates() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().AutoCheckUpdatesOrDefault()
}

// SetAutoCheckUpdates persists the preference and starts/stops the
// background loop accordingly.
func (a *App) SetAutoCheckUpdates(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.AutoCheckUpdates = &enabled
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if a.updater == nil {
		return nil
	}
	if enabled {
		a.updater.Start(a.ctx)
	} else {
		a.updater.Stop()
	}
	return nil
}
```

Update the imports in `desktop/app.go`:

```go
import (
	"context"
	"fmt"
	"sync"
	"time"
	// ...existing imports

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)
```

(If the file already has these imports, leave them.)

- [ ] **Step 12.6: Verify build**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 12.7: Run all Go tests**

```bash
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: pass (no new test failures).

- [ ] **Step 12.8: Commit**

```bash
git add desktop/app.go
git commit -m "feat(desktop): Updater lifecycle + 6 Wails bindings"
```

---

## Task 13: Frontend — types + API wrappers

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 13.1: Add UpdateState type + binding wrappers**

Edit `desktop/frontend/src/lib/api.ts`. Append to the file (after the existing exports):

```ts
// Mirrors desktop/updater.go UpdateState. Field names are snake_case from
// Wails JSON marshaling; we match exactly.
export interface UpdateState {
  current: string;
  latest: string;
  available: boolean;
  notes: string;
  checking: boolean;
  last_check_at: number;
  downloading: boolean;
  download_pct: number;
  ready: boolean;
  error: string;
  asset_url: string;
  asset_size: number;
  download_dir: string;
}
```

Find the `interface AppBindings` block. Add inside it:

```ts
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
```

Append wrapper exports at the bottom of the file:

```ts
export function getUpdateState(): Promise<UpdateState> {
  return bindings().GetUpdateState();
}

export function checkUpdate(): Promise<void> {
  return bindings().CheckUpdate();
}

export function startDownload(): Promise<void> {
  return bindings().StartDownload();
}

export function installUpdate(): Promise<void> {
  return bindings().InstallUpdate();
}

export function getAutoCheckUpdates(): Promise<boolean> {
  return bindings().GetAutoCheckUpdates();
}

export function setAutoCheckUpdates(enabled: boolean): Promise<void> {
  return bindings().SetAutoCheckUpdates(enabled);
}
```

- [ ] **Step 13.2: Verify type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean.

- [ ] **Step 13.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/api.ts
git commit -m "feat(frontend): UpdateState type + bindings"
```

---

## Task 14: Frontend — `ConfirmInstallDialog` component

**Files:**
- Create: `desktop/frontend/src/components/ConfirmInstallDialog.vue`

- [ ] **Step 14.1: Write the component**

Create `desktop/frontend/src/components/ConfirmInstallDialog.vue`:

```vue
<script lang="ts" setup>
const props = defineProps<{
  version: string;
  localCount: number;
  remoteCount: number;
}>();

defineEmits<{
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

function plural(n: number, word: string) {
  return n === 1 ? `1 ${word}` : `${n} ${word}s`;
}
</script>

<template>
  <div class="backdrop" @click.self="$emit('cancel')">
    <div class="dialog">
      <h2>install atterm {{ version }}</h2>

      <p>
        atterm will quit and relaunch on the new version.
        <template v-if="localCount > 0 || remoteCount > 0"> This will:</template>
      </p>

      <ul v-if="localCount > 0 || remoteCount > 0">
        <li v-if="localCount > 0">
          End {{ plural(localCount, "local shell session") }}
          <span class="dim">(running processes will be terminated)</span>
        </li>
        <li v-if="remoteCount > 0">
          Detach from {{ plural(remoteCount, "remote session") }}
          <span class="dim">(the remote PTY keeps running on its host)</span>
        </li>
      </ul>

      <p v-if="localCount > 0" class="warn">Save your work first.</p>

      <div class="row">
        <button @click="$emit('cancel')">cancel</button>
        <button
          class="primary"
          :class="{ danger: localCount > 0 }"
          @click="$emit('confirm')"
        >
          {{ localCount > 0 ? "force install" : "install & restart" }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 460px;
  max-width: calc(100vw - 32px);
  display: flex; flex-direction: column; gap: 12px;
}
.dialog h2 {
  margin: 0; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.dialog p { margin: 0; font-size: 13px; color: var(--fg); line-height: 1.5; }
.dialog ul {
  margin: 0; padding-left: 18px; font-size: 13px; color: var(--fg);
  line-height: 1.6;
}
.dialog .dim { color: var(--fg-dim); font-size: 12px; }
.dialog .warn { color: #d29922; font-size: 12px; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px;
}
.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
.primary:hover { background: #79b8ff; border-color: #79b8ff; color: #0d1117; }
.primary.danger {
  background: var(--bad); color: #0d1117; border-color: var(--bad);
}
.primary.danger:hover { background: #ff6f6a; border-color: #ff6f6a; color: #0d1117; }
</style>
```

- [ ] **Step 14.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean.

- [ ] **Step 14.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/ConfirmInstallDialog.vue
git commit -m "feat(frontend): ConfirmInstallDialog modal"
```

---

## Task 15: Frontend — `SettingsDialog` Updates section

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`

- [ ] **Step 15.1: Replace SettingsDialog**

Replace `desktop/frontend/src/components/SettingsDialog.vue` entirely with:

```vue
<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import {
  checkUpdate,
  getAutoCheckUpdates,
  getRelayConfig,
  getUpdateState,
  installUpdate,
  setAutoCheckUpdates,
  setRelayConfig,
  startDownload,
  type UpdateState,
} from "../lib/api";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
}>();

const emit = defineEmits<{ (e: "close"): void }>();

const url = ref("");
const token = ref("");
const connected = ref(false);
const loading = ref(true);
const saving = ref(false);
const error = ref("");

const updateState = ref<UpdateState | null>(null);
const autoCheck = ref(true);
const checkingNow = ref(false);
const showConfirm = ref(false);
let pollHandle: number | null = null;

onMounted(async () => {
  try {
    const [cfg, st, ac] = await Promise.all([
      getRelayConfig(),
      getUpdateState(),
      getAutoCheckUpdates(),
    ]);
    url.value = cfg.url;
    token.value = cfg.token;
    connected.value = cfg.connected;
    updateState.value = st;
    autoCheck.value = ac;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
  pollHandle = window.setInterval(async () => {
    try {
      updateState.value = await getUpdateState();
    } catch {
      /* ignore — relay polling already surfaces general health */
    }
  }, 2000);
});

onBeforeUnmount(() => {
  if (pollHandle !== null) window.clearInterval(pollHandle);
});

async function save() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({ url: url.value.trim(), token: token.value.trim() });
    const cfg = await getRelayConfig();
    connected.value = cfg.connected;
    emit("close");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

async function disconnect() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({ url: "", token: "" });
    url.value = "";
    token.value = "";
    connected.value = false;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

function close() {
  if (!saving.value) emit("close");
}

async function onCheckNow() {
  checkingNow.value = true;
  try {
    await checkUpdate();
  } catch {
    /* state.error reflects in poll */
  } finally {
    checkingNow.value = false;
  }
}

async function onDownload() {
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  }
}

function onForceInstallClick() {
  showConfirm.value = true;
}

async function onConfirmInstall() {
  showConfirm.value = false;
  try {
    await installUpdate();
    // App will quit shortly; nothing more to do.
  } catch {
    /* state.error reflects in poll */
  }
}

async function onAutoCheckToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  autoCheck.value = target.checked;
  await setAutoCheckUpdates(target.checked);
}

const updateStatusLine = computed(() => {
  const st = updateState.value;
  if (!st) return "";
  if (st.current === "dev" || st.current === "") {
    return "development build — auto-update disabled";
  }
  if (st.error) return st.error;
  if (st.checking || checkingNow.value) return "checking…";
  if (st.ready) return `v${stripV(st.latest)} downloaded — ready to install`;
  if (st.downloading) return `downloading v${stripV(st.latest)} (${st.download_pct}%)`;
  if (st.available) return `v${stripV(st.latest)} available`;
  if (st.last_check_at > 0) return `up to date · last checked ${formatAgo(st.last_check_at)}`;
  return "not checked yet";
});

function stripV(v: string) {
  return v.startsWith("v") ? v.slice(1) : v;
}

function formatAgo(unixSec: number) {
  const diffSec = Math.floor(Date.now() / 1000) - unixSec;
  if (diffSec < 60) return "just now";
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} min ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} h ago`;
  return `${Math.floor(diffSec / 86400)} d ago`;
}

const showUpdates = computed(() => updateState.value !== null);
const isDev = computed(
  () => updateState.value?.current === "dev" || updateState.value?.current === "",
);
</script>

<template>
  <div class="backdrop" @click.self="close">
    <div class="dialog">
      <h2>relay settings</h2>

      <div v-if="loading" class="dim">loading…</div>
      <template v-else>
        <p class="hint">
          configure a remote atterm-relay so this machine's sessions can be
          attached from other devices. when no one is attached, no bytes leave
          this machine.
        </p>

        <label>relay url</label>
        <input
          v-model="url"
          type="text"
          placeholder="ws://relay.example.com:8080"
          :disabled="saving"
          @keyup.enter="save"
        />

        <label>token</label>
        <input
          v-model="token"
          type="password"
          placeholder="shared bearer token"
          :disabled="saving"
          @keyup.enter="save"
        />

        <div class="status">
          <span :class="connected ? 'on' : 'off'">●</span>
          {{ connected ? "uplink running" : "uplink stopped" }}
        </div>

        <div v-if="error" class="error">{{ error }}</div>

        <div class="row">
          <button @click="close" :disabled="saving">cancel</button>
          <button
            v-if="connected"
            @click="disconnect"
            :disabled="saving"
            class="danger"
          >disconnect</button>
          <button
            class="primary"
            @click="save"
            :disabled="saving || !url.trim()"
          >
            {{ saving ? "saving…" : "save & connect" }}
          </button>
        </div>

        <div v-if="showUpdates" class="updates">
          <h2>updates</h2>
          <div class="grid">
            <div class="kv">
              <span class="k">current version</span>
              <span class="v">{{ updateState!.current || "(unknown)" }}</span>
            </div>
            <div class="kv">
              <span class="k">status</span>
              <span class="v">{{ updateStatusLine }}</span>
            </div>
          </div>

          <div v-if="!isDev" class="row autocheck">
            <label class="checkbox">
              <input
                type="checkbox"
                :checked="autoCheck"
                @change="onAutoCheckToggle"
              />
              automatically check for updates
            </label>
          </div>

          <details v-if="!isDev && updateState!.notes" class="notes">
            <summary>release notes</summary>
            <pre>{{ updateState!.notes }}</pre>
          </details>

          <div v-if="!isDev" class="row">
            <button
              @click="onCheckNow"
              :disabled="checkingNow || updateState!.checking"
            >check now</button>
            <button
              v-if="updateState!.available && !updateState!.ready && !updateState!.downloading"
              class="primary"
              @click="onDownload"
            >download {{ updateState!.latest }}</button>
            <button
              v-if="updateState!.downloading"
              class="primary"
              disabled
            >downloading… {{ updateState!.download_pct }}%</button>
            <button
              v-if="updateState!.ready"
              class="primary danger"
              @click="onForceInstallClick"
            >force install &amp; restart</button>
          </div>
        </div>
      </template>
    </div>
    <ConfirmInstallDialog
      v-if="showConfirm && updateState"
      :version="updateState.latest"
      :local-count="props.localSessionCount"
      :remote-count="props.remoteSessionCount"
      @confirm="onConfirmInstall"
      @cancel="showConfirm = false"
    />
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 460px;
  max-width: calc(100vw - 32px);
  display: flex; flex-direction: column; gap: 8px;
}
.dialog h2 {
  margin: 0 0 12px; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.hint {
  font-size: 12px; color: var(--fg-dim); margin: 0 0 8px; line-height: 1.5;
}
label {
  font-size: 12px; color: var(--fg-dim); margin-top: 6px;
}
.status {
  font-size: 12px; color: var(--fg-dim); margin-top: 10px;
  display: flex; align-items: center; gap: 6px;
}
.status .on { color: var(--good); }
.status .off { color: var(--fg-dim); }
.dim { color: var(--fg-dim); font-size: 13px; padding: 8px 0; }
.error { color: var(--bad); font-size: 12px; margin-top: 6px; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px;
}
button.danger {
  border-color: var(--bad); color: var(--bad);
}
button.danger:hover { background: rgba(248, 81, 73, 0.1); }

.updates {
  border-top: 1px solid var(--border);
  margin-top: 16px;
  padding-top: 16px;
}
.updates h2 { margin-bottom: 8px; }
.grid {
  display: grid; gap: 6px; font-size: 12px;
}
.kv { display: flex; gap: 12px; }
.kv .k { color: var(--fg-dim); width: 130px; }
.kv .v { color: var(--fg); }
.autocheck { justify-content: flex-start; margin-top: 12px; }
.checkbox {
  display: flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--fg);
}
.notes {
  margin-top: 8px; font-size: 12px; color: var(--fg);
}
.notes summary { color: var(--fg-dim); cursor: pointer; }
.notes pre {
  background: var(--bg); border: 1px solid var(--border);
  padding: 8px; border-radius: 6px;
  white-space: pre-wrap; word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-height: 160px; overflow-y: auto;
  font-size: 11px;
}
button.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
button.primary:hover { background: #79b8ff; border-color: #79b8ff; color: #0d1117; }
button.primary.danger {
  background: var(--bad); color: #0d1117; border-color: var(--bad);
}
button.primary.danger:hover {
  background: #ff6f6a; border-color: #ff6f6a; color: #0d1117;
}
</style>
```

- [ ] **Step 15.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean. (You may see warnings about `props` being unused if linter is strict, but we're using it inside template — it's fine.)

- [ ] **Step 15.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsDialog.vue
git commit -m "feat(frontend): SettingsDialog 'updates' section + confirm-install flow"
```

---

## Task 16: Frontend — pass session counts + ⚙ badge dot

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 16.1: Wire session counts to SettingsDialog + add badge state**

Edit `desktop/frontend/src/App.vue`. Find the SettingsDialog usage:

```vue
    <SettingsDialog
      v-if="showSettings"
      @close="showSettings = false"
    />
```

Replace with:

```vue
    <SettingsDialog
      v-if="showSettings"
      :local-session-count="localSessionCount"
      :remote-session-count="remoteSessionCount"
      @close="showSettings = false"
    />
```

Find the existing computed/refs section. Add:

```ts
import { getUpdateState, type UpdateState } from "./lib/api";
```

(If `getUpdateState` is not yet imported in App.vue, add it.)

Add new refs alongside the others (after `const toast = ref<string>("")`):

```ts
const updateBadge = ref(false);
let updatePollHandle: number | null = null;
```

Add computed for session counts (next to `sessionCount`):

```ts
const localSessionCount = computed(() => {
  let n = 0;
  for (const t of tabs.value) {
    for (const p of t.panes) {
      if (p.sessionId && !p.remote) n++;
    }
  }
  return n;
});
const remoteSessionCount = computed(() => {
  let n = 0;
  for (const t of tabs.value) {
    for (const p of t.panes) {
      if (p.sessionId && p.remote) n++;
    }
  }
  return n;
});
```

Inside `onMounted`, after `pollHandle = window.setInterval(pollSessions, 2000);`, add:

```ts
  updatePollHandle = window.setInterval(async () => {
    try {
      const st: UpdateState = await getUpdateState();
      updateBadge.value = !!(st.available || st.ready);
    } catch {
      /* ignore */
    }
  }, 5000);
```

In `onUnmounted`, after the existing `if (pollHandle !== null) ...`, add:

```ts
  if (updatePollHandle !== null) window.clearInterval(updatePollHandle);
```

Find the settings (⚙) icon-btn template. It currently looks like:

```vue
      <button
        class="icon-btn"
        title="relay settings"
        @click="showSettings = true"
      >
        <svg ... settings cog ... />
      </button>
```

Replace the entire button (preserving the inner SVG) with:

```vue
      <button
        class="icon-btn"
        title="relay settings"
        @click="showSettings = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
        <span v-if="updateBadge" class="dot"></span>
      </button>
```

Add the `.dot` styling to the `<style scoped>` block (search for `.icon-btn .badge` and add right after it):

```css
.icon-btn .dot {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 6px;
  height: 6px;
  background: #d29922;
  border-radius: 50%;
}
```

- [ ] **Step 16.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean.

- [ ] **Step 16.3: Run frontend tests**

```bash
npm test
```

Expected: all 42 tests pass (no new tests added; existing should still work).

- [ ] **Step 16.4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/App.vue
git commit -m "feat(frontend): ⚙ badge dot + session counts → SettingsDialog"
```

---

## Task 17: CI — ldflags Version injection

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 17.1: Update three build steps**

Edit `.github/workflows/build.yml`. For each of the three platform jobs (linux, darwin, windows), find the `name: build` step and update it.

**Linux** (search for `name: build` near the linux job):

Replace:

```yaml
      - name: build
        working-directory: desktop
        run: wails build -tags webkit2_41 -platform linux/amd64 -s
```

With:

```yaml
      - name: build
        working-directory: desktop
        env:
          VERSION: ${{ github.ref_type == 'tag' && github.ref_name || 'dev' }}
        run: wails build -tags webkit2_41 -platform linux/amd64 -s -ldflags "-X main.Version=$VERSION"
```

**Darwin** (search for `name: build` near build-darwin-arm):

Replace:

```yaml
      - name: build
        working-directory: desktop
        run: wails build -platform darwin/arm64 -s
```

With:

```yaml
      - name: build
        working-directory: desktop
        env:
          VERSION: ${{ github.ref_type == 'tag' && github.ref_name || 'dev' }}
        run: wails build -platform darwin/arm64 -s -ldflags "-X main.Version=$VERSION"
```

**Windows** (search for `name: build` near build-windows):

Replace:

```yaml
      - name: build
        working-directory: desktop
        run: wails build -platform windows/amd64 -s
```

With:

```yaml
      - name: build
        working-directory: desktop
        env:
          VERSION: ${{ github.ref_type == 'tag' && github.ref_name || 'dev' }}
        run: wails build -platform windows/amd64 -s -ldflags "-X main.Version=$VERSION"
```

- [ ] **Step 17.2: Verify YAML is valid**

```bash
cd /Users/attson/code/github.com.attson/atterm
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/build.yml'))" && echo "yaml ok"
```

Expected: `yaml ok`.

- [ ] **Step 17.3: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci: inject Version via ldflags on tagged builds"
```

---

## Task 18: Final verification

- [ ] **Step 18.1: Full Go test pass**

```bash
cd /Users/attson/code/github.com.attson/atterm
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: vet clean; tests green (existing `uplink_e2e_test.go` plus the new `updater_test.go`).

- [ ] **Step 18.2: Full frontend pass**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
npm run build
```

Expected: all unit tests pass; build clean.

- [ ] **Step 18.3: Manual smoke (cannot be automated — flag explicitly)**

> **You cannot fully verify auto-update without a real GitHub release.**
> Document the manual checklist for the user:
>
> 1. With current code (`Version=dev`), boot atterm — Settings → Updates
>    should show "development build — auto-update disabled". No badge.
> 2. Build with `-ldflags "-X main.Version=v0.1.0"` locally; publish a
>    test tag `v0.1.1` with assets. Boot the v0.1.0 build, wait ~30s,
>    Settings should show "v0.1.1 available". ⚙ should have a small dot.
> 3. Click "Download v0.1.1" — progress runs. State goes to "ready".
> 4. Click "Force install & restart" → confirmation modal lists session
>    counts → "Force install" → app quits → ~5s → new version relaunches.
> 5. After relaunch, Settings → Updates: "Up to date". Badge cleared.
>
> Run on linux/amd64, darwin/arm64, windows/amd64.

- [ ] **Step 18.4: Update README**

Edit `README.md`. Find the bullet list under "**当前能力**" and add a new
bullet (or extend an existing one) mentioning auto-update:

Append to the existing capabilities bullet list (the one starting with "✅ Wails 桌面 app..."):

```
- ✅ 内置自动更新：Settings → Updates 检查/下载/重启升级（GitHub Releases，dev 构建禁用）
```

Place this line right after the existing Wails capability bullet.

- [ ] **Step 18.5: Commit**

```bash
git add README.md
git commit -m "docs: README mentions auto-update capability"
```

- [ ] **Step 18.6: Final summary check**

```bash
git log --oneline main^..HEAD | head -25
git status
```

Expected: a series of focused commits (~17 of them), working tree clean.

---

## Self-review notes

- **Spec coverage**:
  - Goal / non-goals stated in plan header.
  - UI surface: Tasks 14 (modal), 15 (settings section), 16 (badge).
  - Architecture diagram from spec → Tasks 3, 7, 9, 10, 12 (Updater) +
    13–16 (frontend).
  - Data model: Task 3 (UpdateState struct) + Task 13 (TS mirror).
  - Version source / ldflags: Task 2 (var) + Task 17 (CI).
  - Update check protocol: Task 5 (fetch + cache + semver).
  - Asset selection: Task 4.
  - Download: Task 7.
  - Install helper (3 platforms): Task 6 + Task 9.
  - Error handling: covered by `state.Error` + UI surfacing in Task 15.
  - Testing: Tasks 3–10 are TDD; Task 18 = full pass.
  - Files-to-change list from spec § "Files to change" all hit:
    1 main.go (T2) ✓
    2 updater.go (T3–10) ✓
    3 updater_test.go (T3–10) ✓
    4 updater_install_test.go — *not done as separate file*; integration
      coverage instead lives in T7's `TestUpdater_Download_*` and T10's
      `TestUpdater_StartStop_*`. The full helper-runs-to-replace
      integration is left for manual smoke (T18.3) because cross-platform
      `setsid + bash` integration tests in CI add fragility for low yield.
    5–7 helper scripts (T6) ✓
    8 app.go (T12) ✓
    9 config.go (T11) ✓
    10 api.ts (T13) ✓
    11 SettingsDialog.vue (T15) ✓
    12 ConfirmInstallDialog.vue (T14) ✓
    13 App.vue (T16) ✓
    14 build.yml (T17) ✓
    15 go.mod / go.sum (T1) ✓
- **No placeholders**: every step has actual code or commands. No "TBD",
  no "implement appropriately", no "see Task N — repeat".
- **Type consistency**: `UpdateState` field names match exactly between
  `desktop/updater.go` (Task 3, 5) and `desktop/frontend/src/lib/api.ts`
  (Task 13). `updaterConfig` fields stay consistent across Tasks 3, 5, 7.
  Bindings list in Task 12 matches wrappers added in Task 13.
