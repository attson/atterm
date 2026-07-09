# Updater Cancel + Existing-File Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship two Settings → Updates capabilities: a clickable "Cancel (N%)" button that aborts an in-flight download, and a lazy detection of already-downloaded archives that transitions the UI to `Ready` and prompts the user whether to re-download.

**Architecture:** All the download-lifecycle logic lives in `desktop/updater.go`. A new `Cancel()` method interrupts the download context and deletes the `.partial`; `tryClaimExistingArchive` short-circuits to `Ready` when a validated archive is on disk; `ForceRedownload` deletes the on-disk file and delegates to `Download`. Frontend wiring adds two Wails bindings, one new `UpdateState` field (`DownloadedExists`), and updates `SettingsUpdates.vue` with a cancel button, a redownload button, a click-scoped `window.confirm` on lazy hits, and 4 new i18n keys per locale.

**Tech Stack:** Go (`desktop/` package, standard `context`/`net/http`), Vue 3 + TypeScript (`desktop/frontend/`), Wails v2.12.0 bindings, vitest for the frontend.

**Spec:** `docs/superpowers/specs/2026-07-09-updater-cancel-and-existing-detect-design.md`

## Global Constraints

- Timeout (`updaterDownloadTimeout = 10 * time.Minute`) and user-cancel MUST be distinguishable: nest `context.WithCancel(parent) → context.WithTimeout(cancelCtx)`. Timeout produces `context.DeadlineExceeded`, user-cancel produces `context.Canceled`.
- `recordError` swallows ONLY `context.Canceled`. Every other error, including `context.DeadlineExceeded`, continues to `state.Error`.
- `DownloadedExists` lifecycle: set true by `tryClaimExistingArchive` when the on-disk file validates. Reset to false at the top of `Download` (fresh HTTP attempt) and by `ForceRedownload`. **Never** modified by `Cancel`.
- Keychain slot keys and pairing state are unrelated to this work — do NOT touch them.
- User-facing prose in Chinese; code, commits, comments stay English.
- No HTTP Range / partial resume — cancellation deletes the `.partial` and next download restarts from byte 0.
- No on-boot disk scan — detection is lazy on click only.

---

## File Structure

| File | New / Modified | Purpose |
|---|---|---|
| `desktop/updater.go` | MODIFIED | Add `UpdateState.DownloadedExists`, `Updater.cancelDownload` field, `Updater.Cancel()`, `Updater.tryClaimExistingArchive()`, `Updater.ForceRedownload()`, `recordError` guard, and rewrite `Download(ctx)` prologue. |
| `desktop/updater_cancel_test.go` | NEW | 8 backend tests (cancel, lazy detect, force redownload, recordError guard). |
| `desktop/app.go` | MODIFIED | Add wails-bound `CancelDownload()` and `ForceRedownload(tag string) error` methods next to existing `StartDownload`. |
| `desktop/frontend/wailsjs/go/main/App.d.ts` | MODIFIED | Add generated `CancelDownload` and `ForceRedownload` declarations. |
| `desktop/frontend/wailsjs/go/main/App.js` | MODIFIED | Add generated `CancelDownload` and `ForceRedownload` runtime wrappers. |
| `desktop/frontend/src/lib/api.ts` | MODIFIED | Add `downloaded_exists: boolean` to `UpdateState`; add `CancelDownload` / `ForceRedownload` on `AppBindings`; export `cancelDownload()` / `forceRedownload(tag)` wrappers. |
| `desktop/frontend/src/components/SettingsUpdates.vue` | MODIFIED | Add `cancelling`/`clickInFlight`/`confirmingRedownload` refs; `onCancelDownload`/`onRedownload` handlers; watcher on `state.downloaded_exists` false→true; template swap of "Downloading (N%)" for "Cancel (N%)"; secondary "Redownload" button when Ready; `.secondary` scoped style. |
| `desktop/frontend/src/i18n/messages/en.ts` | MODIFIED | Add 4 keys under `settings.updates.*`. |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | MODIFIED | Add same 4 keys, zh-CN copy. |
| `desktop/frontend/src/components/__tests__/SettingsUpdates.test.ts` | NEW | 6 vitest cases for the new UI behavior. |

---

## Task 1: Backend — Cancel, Lazy Detect, ForceRedownload

**Files:**
- Modify: `desktop/updater.go`
- Create: `desktop/updater_cancel_test.go`

**Interfaces:**
- Consumes: existing `Updater.cfg.client`, `Updater.state`, `Updater.mu`, `Updater.updatesDir()`, `Updater.verifyDownloadedArchive(ctx, path, name) error`, `Updater.copyWithProgress`, `Updater.recordError(err)`, `assetNameForPlatform(goos, goarch) (string, error)`, `signedSumsForAsset` (test helper in `updater_test.go`), `newUpdater(updaterConfig{...})`. The existing `Download(ctx)` and `DownloadVersion(ctx, tag)` bodies are extended (not replaced).
- Produces:
  - `UpdateState.DownloadedExists bool` (json tag `downloaded_exists,omitempty`).
  - `Updater.Cancel()` — no args, no return.
  - `Updater.ForceRedownload(ctx context.Context, tag string) error`.
  - `Updater.tryClaimExistingArchive(ctx context.Context, latest string) (bool, error)` — internal, called from `Download`.
  - New private field `Updater.cancelDownload context.CancelFunc`.

- [ ] **Step 1: Write the failing tests**

Create `desktop/updater_cancel_test.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Reuse existing helpers from updater_test.go:
//   - signedSumsForAsset(t, assetName, body) (pub, sums, sig)
//   - assetExtForRuntime(t) string
//
// Reuse the existing seed pattern:
//   u := newUpdater(updaterConfig{current, repo, cacheDir, verifyPublicKey})
//   u.state.AssetURL = srv.URL
//   u.state.AssetSize = int64(len(body))
//   u.state.Latest = "v0.2.0"
//   u.checksumURL = "mem://SHA256SUMS"
//   u.checksumSigURL = "mem://SHA256SUMS.sig"
//   u.fetchBytes = ... in-memory shim

func assetNameForTest(t *testing.T) string {
	t.Helper()
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("assetNameForPlatform: %v", err)
	}
	return name
}

// writeValidArchive drops a signed archive into the updates dir under the
// name `<tag>-<asset>` and wires the fetchBytes shim to return the matching
// sums. Returns the final path.
func writeValidArchive(t *testing.T, u *Updater, tmpCache, tag string, body []byte) string {
	t.Helper()
	assetName := assetNameForTest(t)
	pub, sums, sig := signedSumsForAsset(t, assetName, body)
	u.cfg.verifyPublicKey = pub
	u.checksumURL = "mem://SHA256SUMS"
	u.checksumSigURL = "mem://SHA256SUMS.sig"
	u.fetchBytes = func(_ context.Context, url string, _ int64) ([]byte, error) {
		switch url {
		case "mem://SHA256SUMS":
			return sums, nil
		case "mem://SHA256SUMS.sig":
			return sig, nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}
	dir := filepath.Join(tmpCache, "atterm", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, tag+"-"+assetName)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}

func TestUpdater_Download_HitsExistingFile(t *testing.T) {
	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: tmpCache,
	})
	u.state.Latest = "v0.2.0"
	// Point AssetURL at an httptest server that FAILS the test if hit, so
	// we can assert "no HTTP" via a natural failure signal.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("HTTP server should NOT be hit on lazy-detect success")
	}))
	defer srv.Close()
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len("fake-archive-bytes"))
	body := []byte("fake-archive-bytes")
	finalPath := writeValidArchive(t, u, tmpCache, "v0.2.0", body)

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true (lazy hit)")
	}
	if !st.DownloadedExists {
		t.Errorf("DownloadedExists = false; want true (lazy hit)")
	}
	if st.DownloadPath != finalPath {
		t.Errorf("DownloadPath = %q; want %q", st.DownloadPath, finalPath)
	}
	if st.DownloadPct != 100 {
		t.Errorf("DownloadPct = %d; want 100", st.DownloadPct)
	}
}

func TestUpdater_Download_MissingFile_GoesToHTTP(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:         "v0.1.0",
		repo:            "attson/atterm",
		cacheDir:        tmpCache,
		verifyPublicKey: pub,
	})
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len(body))
	u.state.Latest = "v0.2.0"
	u.checksumURL = "mem://SHA256SUMS"
	u.checksumSigURL = "mem://SHA256SUMS.sig"
	u.fetchBytes = func(_ context.Context, url string, _ int64) ([]byte, error) {
		switch url {
		case "mem://SHA256SUMS":
			return sums, nil
		case "mem://SHA256SUMS.sig":
			return sig, nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true")
	}
	if st.DownloadedExists {
		t.Errorf("DownloadedExists = true; want false (fresh HTTP path)")
	}
}

func TestUpdater_Download_CorruptedFile_DeletesAndGoesToHTTP(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:         "v0.1.0",
		repo:            "attson/atterm",
		cacheDir:        tmpCache,
		verifyPublicKey: pub,
	})
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len(body))
	u.state.Latest = "v0.2.0"
	u.checksumURL = "mem://SHA256SUMS"
	u.checksumSigURL = "mem://SHA256SUMS.sig"
	u.fetchBytes = func(_ context.Context, url string, _ int64) ([]byte, error) {
		switch url {
		case "mem://SHA256SUMS":
			return sums, nil
		case "mem://SHA256SUMS.sig":
			return sig, nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}

	// Pre-write a CORRUPTED archive at the target path — verify will fail.
	dir := filepath.Join(tmpCache, "atterm", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	assetName := assetNameForTest(t)
	finalPath := filepath.Join(dir, "v0.2.0-"+assetName)
	if err := os.WriteFile(finalPath, []byte("garbage-bytes"), 0o644); err != nil {
		t.Fatalf("write corrupted: %v", err)
	}

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true (HTTP succeeded)")
	}
	if st.DownloadedExists {
		t.Errorf("DownloadedExists = true; want false (verify failed → HTTP)")
	}
	// The freshly downloaded file has the CORRECT bytes; overwriting is fine.
	got, _ := os.ReadFile(finalPath)
	if string(got) != string(body) {
		t.Errorf("final file content = %q; want %q (corrupted should have been replaced)", string(got), string(body))
	}
}

func TestUpdater_Cancel_InterruptsInFlightDownload(t *testing.T) {
	// Server writes 1 byte then blocks until unblock is closed.
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte{'x'})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-unblock
	}))
	defer srv.Close()
	defer close(unblock)

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: tmpCache,
	})
	u.state.AssetURL = srv.URL
	u.state.AssetSize = 100
	u.state.Latest = "v0.2.0"

	errCh := make(chan error, 1)
	go func() {
		errCh <- u.Download(context.Background())
	}()

	// Wait until Downloading=true — that's the signal that HTTP started.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.State().Downloading {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !u.State().Downloading {
		t.Fatalf("download never started")
	}

	u.Cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("Download returned nil; wanted a cancel-related error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Download err = %v; want wrap of context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Download did not return within 3s of Cancel")
	}

	st := u.State()
	if st.Downloading {
		t.Errorf("Downloading = true; want false")
	}
	if st.Error != "" {
		t.Errorf("Error = %q; want empty (cancel path)", st.Error)
	}
	if st.DownloadPct != 0 {
		t.Errorf("DownloadPct = %d; want 0", st.DownloadPct)
	}

	// .partial must be gone
	partial := filepath.Join(tmpCache, "atterm", "updates", "v0.2.0-"+assetNameForTest(t)+".partial")
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Errorf("partial still exists at %s: %v", partial, err)
	}
}

func TestUpdater_Cancel_NoOpWhenIdle(t *testing.T) {
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: t.TempDir(),
	})
	before := u.State()
	u.Cancel() // must not panic
	after := u.State()
	if before.Error != after.Error {
		t.Errorf("idle Cancel mutated Error: before=%q after=%q", before.Error, after.Error)
	}
	if before.Downloading != after.Downloading {
		t.Errorf("idle Cancel mutated Downloading")
	}
}

func TestUpdater_ForceRedownload_DeletesExistingAndRefetches(t *testing.T) {
	body := []byte("fresh-download-bytes")
	assetName := "AT-Term-" + runtime.GOOS + "-" + runtime.GOARCH + assetExtForRuntime(t)
	pub, sums, sig := signedSumsForAsset(t, assetName, body)

	// Asset server: returns the fresh body for GET /<tag>/asset. Any other
	// path is a test bug.
	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.2.0/asset" {
			t.Errorf("unexpected asset request path: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer assetSrv.Close()

	// Custom releases payload whose asset URL points at OUR asset server.
	payload := map[string]any{
		"tag_name":   "v0.2.0",
		"name":       "v0.2.0",
		"body":       "release notes",
		"prerelease": false,
		"assets": []map[string]any{
			{"name": assetName, "browser_download_url": assetSrv.URL + "/v0.2.0/asset", "size": int64(len(body))},
			{"name": "SHA256SUMS", "browser_download_url": "mem://SHA256SUMS", "size": int64(len(sums))},
			{"name": "SHA256SUMS.sig", "browser_download_url": "mem://SHA256SUMS.sig", "size": int64(len(sig))},
		},
	}
	apiURL, _ := fakeGitHub(t, payload)

	tmpCache := t.TempDir()
	u := newUpdater(updaterConfig{
		current:         "v0.1.0",
		repo:            "attson/atterm",
		cacheDir:        tmpCache,
		verifyPublicKey: pub,
		releaseURL:      apiURL,
		releasesURL:     apiURL + "/releases",
	})
	// Wire the in-memory checksum shim so verifyDownloadedArchive succeeds
	// without a real HTTP for SHA256SUMS.
	u.fetchBytes = func(_ context.Context, url string, _ int64) ([]byte, error) {
		switch url {
		case "mem://SHA256SUMS":
			return sums, nil
		case "mem://SHA256SUMS.sig":
			return sig, nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}

	// Pre-write a valid archive with STALE bytes at the target path. Also
	// mark state as if a previous lazy-hit populated it.
	dir := filepath.Join(tmpCache, "atterm", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	finalPath := filepath.Join(dir, "v0.2.0-"+assetName)
	if err := os.WriteFile(finalPath, []byte("stale-bytes"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	u.mu.Lock()
	u.state.Ready = true
	u.state.DownloadedExists = true
	u.mu.Unlock()

	if err := u.ForceRedownload(context.Background(), "v0.2.0"); err != nil {
		t.Fatalf("ForceRedownload err: %v", err)
	}

	got, _ := os.ReadFile(finalPath)
	if string(got) != string(body) {
		t.Errorf("file content = %q; want %q (stale bytes should have been overwritten)", string(got), string(body))
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true after ForceRedownload")
	}
	if st.DownloadedExists {
		t.Errorf("DownloadedExists = true; want false (fresh HTTP path)")
	}
}

func TestUpdater_RecordError_SwallowsContextCanceled_KeepsDeadlineExceeded(t *testing.T) {
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: t.TempDir(),
	})
	// Seed a prior state so we can assert "did NOT overwrite".
	u.mu.Lock()
	u.state.Error = "previous"
	u.mu.Unlock()

	u.recordError(context.Canceled)
	if got := u.State().Error; got != "previous" {
		t.Errorf("after Canceled: Error = %q; want %q (should have been swallowed)", got, "previous")
	}

	u.recordError(context.DeadlineExceeded)
	if got := u.State().Error; got == "previous" || got == "" {
		t.Errorf("after DeadlineExceeded: Error = %q; want a non-empty non-'previous' value (timeout must surface)", got)
	}
}

func TestUpdater_RecordError_SwallowsWrappedContextCanceled(t *testing.T) {
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: t.TempDir(),
	})
	u.mu.Lock()
	u.state.Error = "previous"
	u.mu.Unlock()

	wrapped := fmt.Errorf("copy failed: %w", context.Canceled)
	u.recordError(wrapped)
	if got := u.State().Error; got != "previous" {
		t.Errorf("wrapped Canceled leaked through: Error = %q; want %q", got, "previous")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./desktop -run 'TestUpdater_(Download_Hits|Download_Missing|Download_Corrupted|Cancel_|ForceRedownload_|RecordError_)' -v`
Expected: FAIL — some with "undefined: `Updater.Cancel`", some with `DownloadedExists` field not present on state, some passing accidentally (e.g. current Download that just misses on file-existence and goes HTTP path). Read the failures carefully; you're establishing the RED baseline.

- [ ] **Step 3: Add the `DownloadedExists` field**

Edit `desktop/updater.go`. Find the `UpdateState` struct (starts around line 41). Add this field immediately after `Ready bool`:

```go
// DownloadedExists signals that the most recent DownloadVersion /
// StartDownload call short-circuited to Ready because a validated
// archive was already on disk. The frontend watches false→true to
// decide whether to prompt "redownload?" Cleared at Download's HTTP
// entry and by ForceRedownload. Never touched by Cancel.
DownloadedExists bool `json:"downloaded_exists,omitempty"`
```

- [ ] **Step 4: Add the `cancelDownload` field on `Updater`**

In the `Updater` struct (around line 77) add a field, under the `mu.Mutex` guard, next to `cancelLoop`:

```go
// cancelDownload interrupts an in-flight Download. Set at Download's
// HTTP entry, cleared on Download return, called by Cancel(). nil when
// no download is running.
cancelDownload context.CancelFunc
```

- [ ] **Step 5: Add the `tryClaimExistingArchive` helper**

Find `clearStaleReadyLocked` (around line 365 in `desktop/updater.go`). Add this immediately after it:

```go
// tryClaimExistingArchive checks whether the target archive for latest
// already exists on disk and verifies OK. Returns (hit=true, nil) if
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
		_ = os.Remove(final) // corrupted; treat as miss
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

- [ ] **Step 6: Rewrite the `Download(ctx)` prologue**

Find `Download(ctx)` (around line 594). Replace the prologue — everything **from the function signature down to and including `defer func() { ... u.state.Downloading = false ... }()`** — with the following. Every line below that defer stays exactly as it is today.

BEFORE (delete this block):
```go
func (u *Updater) Download(ctx context.Context) error {
	downloadCtx, cancelDownload := context.WithTimeout(ctx, updaterDownloadTimeout)
	defer cancelDownload()

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
	u.state.DownloadPath = final
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.state.Downloading = false
		u.mu.Unlock()
	}()
```

AFTER (paste this):
```go
func (u *Updater) Download(ctx context.Context) error {
	u.mu.Lock()
	url := u.state.AssetURL
	expectedSize := u.state.AssetSize
	latest := u.state.Latest
	u.mu.Unlock()
	if url == "" {
		return fmt.Errorf("no asset URL — Check first")
	}

	// Lazy-hit: if the target archive is already on disk and validates,
	// short-circuit to Ready without any HTTP traffic. Must run BEFORE
	// setting up the cancellable context so idle-Download-with-hit never
	// touches u.cancelDownload.
	if hit, err := u.tryClaimExistingArchive(ctx, latest); err != nil {
		u.recordError(err)
		return err
	} else if hit {
		return nil
	}

	// Nested contexts distinguish cancel vs timeout. External Cancel()
	// produces context.Canceled; timeout produces context.DeadlineExceeded.
	// recordError swallows only Canceled.
	cancelCtx, cancelDownload := context.WithCancel(ctx)
	downloadCtx, timeoutCancel := context.WithTimeout(cancelCtx, updaterDownloadTimeout)
	defer timeoutCancel()
	defer cancelDownload()

	u.mu.Lock()
	u.cancelDownload = cancelDownload
	u.state.DownloadedExists = false
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.cancelDownload = nil
		u.mu.Unlock()
	}()

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
	u.state.DownloadPath = final
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.state.Downloading = false
		u.mu.Unlock()
	}()
```

Every line from `req, err := http.NewRequestWithContext(downloadCtx, "GET", u.downloadURL(url), nil)` down to the closing brace of `Download` stays as it is today.

- [ ] **Step 7: Add the `Cancel()` method**

Immediately after `Download(ctx)`'s closing brace, add:

```go
// Cancel interrupts an in-flight Download (if any). Idempotent: does
// nothing when no download is running. The .partial file is removed
// best-effort so the next download restarts cleanly. State fields
// touched: Downloading, DownloadPct, Error. Does NOT touch
// DownloadedExists (that field's lifecycle is scoped to Download
// entry and ForceRedownload).
func (u *Updater) Cancel() {
	u.mu.Lock()
	cancel := u.cancelDownload
	downloadPath := u.state.DownloadPath
	u.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()

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

- [ ] **Step 8: Add the `ForceRedownload(ctx, tag)` method**

Immediately after `DownloadVersion(ctx, tag)` (around line 724), add:

```go
// ForceRedownload skips the lazy-hit path by removing any existing
// archive for tag, then delegates to the standard Download flow.
// Used when the user has explicitly asked to redownload (either
// from the "Redownload" button in the Ready state, or from the
// confirm prompt after a lazy-hit).
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
	return u.Download(ctx)
}
```

- [ ] **Step 9: Add the `recordError` guard**

Find `recordError` (around line 867). Add the `context.Canceled` short-circuit as the first statement:

```go
func (u *Updater) recordError(err error) {
	// User-initiated Cancel() sends ctx.Cancel through the copy loop; the
	// resulting error wraps context.Canceled. Cancel() itself has already
	// cleared state.Error, so overwriting it here would clobber the
	// intended "no error, we cancelled cleanly" outcome. context.DeadlineExceeded
	// (timeout) is a distinct sentinel and still falls through.
	if errors.Is(err, context.Canceled) {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Error = err.Error()
	u.state.Ready = false
}
```

Also add `"errors"` to the top-of-file imports if not already present. Verify with `grep '"errors"' desktop/updater.go` (should already be present, but check).

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./desktop -run 'TestUpdater_(Download_Hits|Download_Missing|Download_Corrupted|Cancel_|ForceRedownload_|RecordError_)' -v`
Expected: PASS for all 8 tests (Download_Hits, Download_Missing, Download_Corrupted, Cancel_Interrupts, Cancel_NoOp, ForceRedownload_Deletes, RecordError_Swallows_Keeps, RecordError_SwallowsWrapped).

If `TestUpdater_Cancel_InterruptsInFlightDownload` flakes on "download never started" — bump the deadline from 2s to 5s. If `err` from the cancelled download does not wrap `context.Canceled` (e.g. it comes back as a wrapped `syscall.EINTR` or similar), inspect the error chain in `copyWithProgress` — likely the copy is done before cancel can propagate; try a smaller `Content-Length` header (10 instead of 100) to slow the flush.

- [ ] **Step 11: Run the full desktop test suite**

Run: `go test ./desktop`
Expected: PASS. Regressions in `TestUpdater_Download_*` (existing tests) would indicate the Download prologue rewrite drifted. Debug by comparing the after-block above against the actual state of `desktop/updater.go`.

- [ ] **Step 12: Commit**

```bash
git add desktop/updater.go desktop/updater_cancel_test.go
git commit -m "feat(desktop): updater supports Cancel + lazy-detect + ForceRedownload"
```

---

## Task 2: Wails bindings + frontend api.ts wiring

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/src/lib/api.ts`

**Interfaces:**
- Consumes: from Task 1 — `Updater.Cancel()` (no args, no return), `Updater.ForceRedownload(ctx, tag) error`, `UpdateState.DownloadedExists`.
- Produces:
  - `App.CancelDownload()` (Wails-bound, no return).
  - `App.ForceRedownload(tag string) error`.
  - `AppBindings.CancelDownload(): Promise<void>` and `AppBindings.ForceRedownload(tag: string): Promise<void>` on the frontend interface.
  - `UpdateState.downloaded_exists: boolean` on the frontend type.
  - `cancelDownload(): Promise<void>` and `forceRedownload(tag: string): Promise<void>` top-level wrappers exported from `lib/api.ts`.

- [ ] **Step 1: Add the wails-bound methods on `App`**

Edit `desktop/app.go`. Find `StartDownload` (around line 1442). Add these two methods immediately after `DownloadVersion` (around line 1451):

```go
// CancelDownload interrupts an in-flight update download (if any) and
// clears the download state so the UI reverts to the pre-download
// primary button. Bound to Settings → Updates "Cancel (N%)" button.
func (a *App) CancelDownload() {
	if a.updater == nil {
		return
	}
	a.updater.Cancel()
}

// ForceRedownload deletes any existing archive for tag and downloads
// fresh. Bound to Settings → Updates "Redownload" button and the
// "redownload?" confirm prompt.
func (a *App) ForceRedownload(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.ForceRedownload(a.ctx, tag)
}
```

- [ ] **Step 2: Add the wailsjs generated declaration to `App.d.ts`**

Edit `desktop/frontend/wailsjs/go/main/App.d.ts`. Find the existing `DownloadVersion` line. Add immediately after it:

```ts
export function CancelDownload():Promise<void>;

export function ForceRedownload(arg1:string):Promise<void>;
```

The blank line between declarations matches the existing file style.

- [ ] **Step 3: Add the runtime wrappers to `App.js`**

Edit `desktop/frontend/wailsjs/go/main/App.js`. Find the existing `DownloadVersion` function. Add immediately after it:

```js
export function CancelDownload() {
  return window['go']['main']['App']['CancelDownload']();
}

export function ForceRedownload(arg1) {
  return window['go']['main']['App']['ForceRedownload'](arg1);
}
```

- [ ] **Step 4: Update `UpdateState` in `lib/api.ts`**

Edit `desktop/frontend/src/lib/api.ts`. Find the `UpdateState` interface (around line 249). Add `downloaded_exists: boolean;` at the end, next to `lines: VersionLine[];`:

```ts
export interface UpdateState {
  // ...existing fields, unchanged...
  lines: VersionLine[];
  // downloaded_exists is true when the most recent DownloadVersion /
  // StartDownload call short-circuited to Ready because the archive was
  // already on disk. The frontend watches false→true to prompt the user
  // whether to redownload.
  downloaded_exists: boolean;
}
```

- [ ] **Step 5: Add the two methods to `AppBindings`**

In the same file, find the `AppBindings` interface. Add these two methods immediately after `DownloadVersion(tag: string): Promise<void>;`:

```ts
CancelDownload(): Promise<void>;
ForceRedownload(tag: string): Promise<void>;
```

- [ ] **Step 6: Export the top-level wrappers**

Find `downloadVersion` wrapper (around line 631). Add these two immediately after it:

```ts
export function cancelDownload(): Promise<void> {
  return bindings().CancelDownload();
}
export function forceRedownload(tag: string): Promise<void> {
  return bindings().ForceRedownload(tag);
}
```

- [ ] **Step 7: TypeScript compile check**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: PASS or same pre-existing errors as before this task. Zero errors mentioning `CancelDownload`, `ForceRedownload`, `cancelDownload`, `forceRedownload`, or `downloaded_exists`.

- [ ] **Step 8: Commit**

```bash
git add desktop/app.go \
        desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/src/lib/api.ts
git commit -m "feat(desktop): wire CancelDownload + ForceRedownload through wailsjs + api.ts"
```

---

## Task 3: Settings UI — cancel button, redownload button, confirm prompt, i18n, tests

**Files:**
- Modify: `desktop/frontend/src/components/SettingsUpdates.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Create: `desktop/frontend/src/components/__tests__/SettingsUpdates.test.ts`

**Interfaces:**
- Consumes: from Task 2 — `cancelDownload()`, `forceRedownload(tag)`, `UpdateState.downloaded_exists`.
- Produces: user-visible Cancel + Redownload buttons + confirm prompt.

- [ ] **Step 1: Add the 4 i18n keys to `en.ts`**

Edit `desktop/frontend/src/i18n/messages/en.ts`. Find the `settings.updates.*` block (grep for `downloadingButton:` — an existing sibling key). Add these 4 entries near it:

```ts
cancelDownload: 'Cancel ({pct}%)',
cancelling: 'Cancelling…',
redownload: 'Redownload',
redownloadPrompt: '{version} is already downloaded on this device. Redownload?',
```

- [ ] **Step 2: Add the same 4 keys to `zh-CN.ts`**

Edit `desktop/frontend/src/i18n/messages/zh-CN.ts`. Find the matching `settings.updates.*` block. Add:

```ts
cancelDownload: '取消 ({pct}%)',
cancelling: '取消中…',
redownload: '重新下载',
redownloadPrompt: '本机已下载过 {version} 的安装包。是否重新下载？',
```

- [ ] **Step 3: Write the failing frontend tests**

Create `desktop/frontend/src/components/__tests__/SettingsUpdates.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest'
import { nextTick } from 'vue'

const { fake } = vi.hoisted(() => ({
  fake: {
    events: { on: vi.fn().mockReturnValue(() => {}), off: vi.fn(), emit: vi.fn() },
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const pairs = Object.entries(params).map(([kk, vv]) => `${kk}=${vv}`).join(',')
      return `${k}[${pairs}]`
    },
  }),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => fake,
}))

import SettingsUpdates from '../SettingsUpdates.vue'
import * as api from '../../lib/api'

function baseState(overrides: Partial<api.UpdateState> = {}): api.UpdateState {
  return {
    current: 'v0.1.0',
    latest: 'v0.2.168',
    available: true,
    notes: '',
    checking: false,
    last_check_at: 1_700_000_000,
    downloading: false,
    download_pct: 0,
    ready: false,
    error: '',
    asset_url: 'https://example.test/asset.tar.gz',
    asset_size: 1024,
    download_dir: '/tmp',
    download_path: '/tmp/v0.2.168-file.tar.gz',
    lines: [],
    downloaded_exists: false,
    ...overrides,
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(api, 'getAutoCheckUpdates').mockResolvedValue(true as never)
  vi.spyOn(api, 'getUpdateGHProxyURL').mockResolvedValue('' as never)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('SettingsUpdates cancel + redownload', () => {
  let confirmSpy: MockInstance<[message?: string], boolean>

  beforeEach(() => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    confirmSpy.mockRestore()
  })

  it('renders Cancel (N%) button while downloading and clicking calls cancelDownload', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ downloading: true, download_pct: 42 }) as never,
    )
    const cancelSpy = vi.spyOn(api, 'cancelDownload').mockResolvedValue()
    const w = mount(SettingsUpdates)
    await flushPromises()

    const btn = w.find('button.primary.danger')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('settings.updates.cancelDownload')
    expect(btn.text()).toContain('pct=42')
    await btn.trigger('click')
    await flushPromises()
    expect(cancelSpy).toHaveBeenCalledTimes(1)
  })

  it('renders both Install & Restart and Redownload while Ready', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ ready: true }) as never,
    )
    const w = mount(SettingsUpdates)
    await flushPromises()

    const btns = w.findAll('button')
    const install = btns.find((b) => b.text().includes('settings.updates.forceInstallRestart'))
    const redl = btns.find((b) => b.text().includes('settings.updates.redownload'))
    expect(install?.exists()).toBe(true)
    expect(redl?.exists()).toBe(true)
  })

  it('clicking Redownload calls forceRedownload with the current latest tag', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ ready: true, latest: 'v0.2.168' }) as never,
    )
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    const w = mount(SettingsUpdates)
    await flushPromises()

    const redl = w.findAll('button').find((b) => b.text().includes('settings.updates.redownload'))!
    await redl.trigger('click')
    await flushPromises()
    expect(forceSpy).toHaveBeenCalledWith('v0.2.168')
  })

  it('lazy-hit prompts and calls forceRedownload on confirm=true', async () => {
    // First mount: available + not ready. Second poll: downloaded_exists=true.
    const getSpy = vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false, available: true }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true, latest: 'v0.2.168' }) as never,
      )
    vi.spyOn(api, 'startDownload').mockResolvedValue()
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    confirmSpy.mockReturnValue(true)

    const w = mount(SettingsUpdates)
    await flushPromises()

    // Click the primary "Download" button — triggers clickInFlight.
    const dl = w.findAll('button').find((b) => b.text().includes('settings.updates.downloadVersion'))!
    await dl.trigger('click')
    await flushPromises()

    // Advance the poll interval (2000ms) so getUpdateState fires again.
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(getSpy).toHaveBeenCalledTimes(2)
    expect(confirmSpy).toHaveBeenCalled()
    expect(forceSpy).toHaveBeenCalledWith('v0.2.168')
  })

  it('lazy-hit with confirm=false does not call forceRedownload', async () => {
    vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false, available: true }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true, latest: 'v0.2.168' }) as never,
      )
    vi.spyOn(api, 'startDownload').mockResolvedValue()
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    confirmSpy.mockReturnValue(false)

    const w = mount(SettingsUpdates)
    await flushPromises()
    const dl = w.findAll('button').find((b) => b.text().includes('settings.updates.downloadVersion'))!
    await dl.trigger('click')
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(forceSpy).not.toHaveBeenCalled()
  })

  it('spurious downloaded_exists (no click) does not prompt', async () => {
    // Mount with ready=false. Poll flips downloaded_exists=true without a
    // click having happened. Watcher must NOT fire the confirm.
    vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true }) as never,
      )
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()

    const w = mount(SettingsUpdates)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(confirmSpy).not.toHaveBeenCalled()
    expect(forceSpy).not.toHaveBeenCalled()
    // Prevent unused-variable warnings.
    void nextTick
    void w
  })
})
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsUpdates.test.ts`
Expected: FAIL — buttons don't exist, refs missing, watcher not wired. Read the failures to confirm the RED baseline.

- [ ] **Step 5: Update the `<script>` block in `SettingsUpdates.vue`**

Edit `desktop/frontend/src/components/SettingsUpdates.vue`. In the imports at the top, add `cancelDownload` and `forceRedownload` to the existing `import { ... } from "../lib/api"` line:

```ts
import {
  checkUpdate,
  downloadVersion,
  getAutoCheckUpdates,
  getUpdateGHProxyURL,
  getUpdateState,
  setAutoCheckUpdates,
  setUpdateGHProxyURL,
  startDownload,
  cancelDownload,
  forceRedownload,
  type UpdateState,
} from "../lib/api";
```

Next, at the end of the existing ref declarations (after `const selectedLine = ref("")`), add:

```ts
const clickInFlight = ref(false);
const confirmingRedownload = ref(false);
const cancelling = ref(false);
```

Modify `onDownload` to set `clickInFlight`:

```ts
async function onDownload() {
  clickInFlight.value = true;
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  }
}
```

Modify `onDownloadSelected` similarly:

```ts
async function onDownloadSelected() {
  if (!selectedLatest.value) return;
  clickInFlight.value = true;
  try {
    await downloadVersion(selectedLatest.value);
  } catch {
    /* state.error reflects in poll */
  }
}
```

- [ ] **Step 6: Add the watcher on `downloaded_exists`**

Immediately after `onDownloadSelected` (or before the existing `onAutoCheckToggle`), add:

```ts
watch(
  () => state.value?.downloaded_exists,
  async (now, was) => {
    if (!now || was) return;
    if (!clickInFlight.value) return;
    if (confirmingRedownload.value) return;
    const tag = state.value?.latest ?? "";
    clickInFlight.value = false;
    confirmingRedownload.value = true;
    try {
      if (window.confirm(t("settings.updates.redownloadPrompt", { version: tag }))) {
        try {
          await forceRedownload(tag);
        } catch {
          /* poll surfaces error */
        }
      }
      // Cancel branch: nothing. state.ready is already true; the Install &
      // Restart button and the Redownload button are already rendered.
    } finally {
      confirmingRedownload.value = false;
    }
  },
);
```

Make sure `watch` is imported at the top of the script block — the existing imports already include it. If not, add: `import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";` (existing line already imports these).

- [ ] **Step 7: Add the cancel + redownload handlers**

Immediately after the watcher above, add:

```ts
async function onCancelDownload() {
  cancelling.value = true;
  try {
    await cancelDownload();
  } catch {
    /* poll surfaces error */
  } finally {
    cancelling.value = false;
  }
}

async function onRedownload() {
  const tag = state.value?.latest ?? "";
  if (!tag) return;
  try {
    await forceRedownload(tag);
  } catch {
    /* poll surfaces error */
  }
}
```

- [ ] **Step 8: Update the template**

In the `<template>`, find the existing `.actions` block. Replace the DOWNLOADING button and REPLACE the READY button block with the following. Every other button (Check Now, Download primary, Download Version primary) is UNCHANGED.

BEFORE (delete these two `<button>` elements):
```html
<button
  v-if="state.downloading"
  class="primary"
  disabled
>{{ t("settings.updates.downloadingButton", { pct: state.download_pct }) }}</button>
<button
  v-if="state.ready"
  class="primary danger"
  @click="$emit('request-install', state.latest)"
>{{ t("settings.updates.forceInstallRestart") }}</button>
```

AFTER (paste these three `<button>` elements in the same place):
```html
<button
  v-if="state.downloading"
  class="primary danger"
  :disabled="cancelling"
  @click="onCancelDownload"
>{{ cancelling
    ? t('settings.updates.cancelling')
    : t('settings.updates.cancelDownload', { pct: state.download_pct }) }}</button>
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

- [ ] **Step 9: Add the `.secondary` scoped style**

Append to the existing `<style scoped>` block (near the bottom of the file, right before the closing `</style>`):

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

- [ ] **Step 10: Run the tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsUpdates.test.ts`
Expected: PASS — 6/6 tests.

If `lazy-hit prompts and calls forceRedownload on confirm=true` fails on `expect(getSpy).toHaveBeenCalledTimes(2)`, the poll interval timer isn't advancing properly — the test uses `vi.useFakeTimers()` at the top-level `beforeEach` (see the file's shared setup). If the assertion says `.toHaveBeenCalledTimes(1)`, add `await vi.runOnlyPendingTimersAsync()` between `flushPromises()` calls.

If `spurious downloaded_exists (no click) does not prompt` fails because the WATCHER fires anyway, verify the `if (!clickInFlight.value) return` guard is present at the top of the watcher body.

- [ ] **Step 11: Run the wider vitest suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS. If pre-existing unrelated tests fail (same as before this task), note them but don't fix in this task.

- [ ] **Step 12: Run `vue-tsc` to catch type regressions**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: no new errors mentioning `SettingsUpdates.vue` or the new i18n keys.

- [ ] **Step 13: Manual smoke test (recommended, not required to gate the commit)**

Run: `make dev` from the repo root.
- Check for updates (settle on some `latest`); click "Download vX.Y.Z"; while it's mid-download click "Cancel (N%)" — form reverts to the pre-download state, no error banner.
- Trigger a download and let it complete → "Install & Restart" appears + "Redownload" appears next to it.
- Click "Redownload" — a fresh download starts (no confirm dialog on this path).
- Click Check Now, click "Download vX.Y.Z" while the archive is still on disk — the confirm dialog appears; clicking Cancel keeps the buttons visible; clicking OK triggers a fresh download.

- [ ] **Step 14: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts \
        desktop/frontend/src/components/SettingsUpdates.vue \
        desktop/frontend/src/components/__tests__/SettingsUpdates.test.ts
git commit -m "feat(desktop): Settings → Updates cancel button + redownload flow + i18n + tests"
```

---

## Post-implementation checklist

- [ ] All three tasks committed as separate commits.
- [ ] `go test ./desktop` passes (existing + 8 new backend tests).
- [ ] `cd desktop/frontend && npx vitest run` passes (existing + 6 new SettingsUpdates tests).
- [ ] Manual smoke test succeeded (or noted as skipped).
- [ ] Branch is ready for `superpowers:finishing-a-development-branch` → PR to main → squash-merge.
