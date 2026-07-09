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
