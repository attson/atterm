package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestUpdater_DefaultClientDoesNotCapDownloadAtCheckTimeout(t *testing.T) {
	u := newUpdater(updaterConfig{
		current: "v0.1.0",
		repo:    "attson/atterm",
	})
	if u.cfg.client.Timeout != 0 {
		t.Fatalf("default client timeout = %s; want 0 so Download uses its longer request context", u.cfg.client.Timeout)
	}
	if updaterDownloadTimeout <= updaterCheckTimeout {
		t.Fatalf("download timeout = %s; want greater than check timeout %s", updaterDownloadTimeout, updaterCheckTimeout)
	}
}

// silence-the-unused-import warnings for httptest in later tasks.
var _ = httptest.NewServer

func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "AT-Term-linux-amd64.tar.gz"},
		{"linux", "arm64", "AT-Term-linux-arm64.tar.gz"},
		{"darwin", "arm64", "AT-Term-darwin-arm64.zip"},
		{"darwin", "amd64", "AT-Term-darwin-amd64.zip"},
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
		{"freebsd", "amd64"},
	}
	for _, c := range cases {
		_, err := assetNameForPlatform(c.goos, c.goarch)
		if err == nil {
			t.Errorf("expected error for %s/%s", c.goos, c.goarch)
		}
	}
}

func TestProxiedGitHubReleaseURL(t *testing.T) {
	original := "https://github.com/attson/atterm/releases/download/v0.2.13/AT-Term-windows-amd64.zip"
	got := proxiedGitHubReleaseURL(original, "https://gh-proxy.example.com/")
	want := "https://gh-proxy.example.com/" + original
	if got != want {
		t.Fatalf("proxied URL = %q; want %q", got, want)
	}

	if got := proxiedGitHubReleaseURL(original, ""); got != original {
		t.Fatalf("empty proxy URL = %q; want original", got)
	}
	if got := proxiedGitHubReleaseURL(original, "https://gh-proxy.example.com"); got != want {
		t.Fatalf("proxy without trailing slash = %q; want %q", got, want)
	}
	if got := proxiedGitHubReleaseURL("https://example.com/file.zip", "https://gh-proxy.example.com/"); got != "https://example.com/file.zip" {
		t.Fatalf("non-github URL was proxied to %q", got)
	}
	if got := proxiedGitHubReleaseURL("https://github.com/attson/atterm/releases/latest", "https://gh-proxy.example.com/"); got != "https://github.com/attson/atterm/releases/latest" {
		t.Fatalf("non-download GitHub URL was proxied to %q", got)
	}
}

func TestNormalizeUpdateGHProxyURL(t *testing.T) {
	got, err := normalizeUpdateGHProxyURL(" https://gh-proxy.example.com/ ")
	if err != nil {
		t.Fatalf("normalize err: %v", err)
	}
	if got != "https://gh-proxy.example.com/" {
		t.Fatalf("normalized = %q; want trimmed https URL", got)
	}
	if got, err := normalizeUpdateGHProxyURL(" "); err != nil || got != "" {
		t.Fatalf("empty normalize got (%q, %v); want empty nil", got, err)
	}
	if _, err := normalizeUpdateGHProxyURL("ftp://example.com/"); err == nil {
		t.Fatal("ftp proxy URL accepted; want error")
	}
	if _, err := normalizeUpdateGHProxyURL("gh-proxy.example.com"); err == nil {
		t.Fatal("relative proxy URL accepted; want error")
	}
}

// helper: spin up a fake GitHub releases endpoint that returns the supplied
// payload and counts requests.
func fakeGitHub(t *testing.T, payload any) (string, *int) {
	t.Helper()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The releases-list endpoint (used by refreshLines) returns a JSON
		// array and deliberately does NOT count toward hits, so existing
		// latest-release hit assertions (cache/force) stay accurate.
		if strings.HasSuffix(r.URL.Path, "/releases") {
			_ = json.NewEncoder(w).Encode([]any{payload})
			return
		}
		hits++
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
			{
				"name":                 "SHA256SUMS",
				"browser_download_url": "https://example.com/" + tag + "/SHA256SUMS",
				"size":                 int64(100),
			},
			{
				"name":                 "SHA256SUMS.sig",
				"browser_download_url": "https://example.com/" + tag + "/SHA256SUMS.sig",
				"size":                 int64(ed25519.SignatureSize),
			},
		},
	}
}

func TestUpdater_Check_NewVersionAvailable(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.2.0", false))
	u := newUpdater(updaterConfig{
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  apiURL,
		releasesURL: apiURL + "/releases",
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

func TestUpdater_Check_ClearsReadyWhenDownloadedAssetIsForOlderVersion(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.2.0", false))
	tmpCache := t.TempDir()
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(tmpCache, "atterm", "updates", "v0.1.5-"+name)
	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: tmpCache,
	})
	// Simulate a previous successful download that is no longer the latest
	// release after the next Check refreshes GitHub state.
	u.state.Latest = "v0.1.5"
	u.state.Ready = true
	u.state.DownloadPct = 100
	u.state.DownloadPath = oldPath
	u.cfg.releaseURL = apiURL
	u.cfg.releasesURL = apiURL + "/releases"

	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check err: %v", err)
	}
	st := u.State()
	if st.Ready {
		t.Fatalf("Ready = true with stale download %q; want false", st.DownloadPath)
	}
	if !st.Available {
		t.Fatalf("Available = false; want true so UI can offer download %s", st.Latest)
	}
	if st.DownloadPath != "" {
		t.Fatalf("DownloadPath = %q; want empty after stale ready is cleared", st.DownloadPath)
	}
}

func TestUpdater_Check_UpToDate(t *testing.T) {
	apiURL, _ := fakeGitHub(t, releasePayload("v0.1.0", false))
	u := newUpdater(updaterConfig{
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  apiURL,
		releasesURL: apiURL + "/releases",
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
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  apiURL,
		releasesURL: apiURL + "/releases",
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
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  apiURL,
		releasesURL: apiURL + "/releases",
		now:         func() time.Time { return now },
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
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  apiURL,
		releasesURL: apiURL + "/releases",
		now:         func() time.Time { return now },
	})
	_ = u.Check(context.Background(), true)
	_ = u.Check(context.Background(), true) // force=true bypasses cache
	if *hits != 2 {
		t.Errorf("hits = %d; want 2 (force should always fetch)", *hits)
	}
}

func TestUpdater_Check_FallsBackToLatestRedirectOnGitHubForbidden(t *testing.T) {
	var apiHits, latestHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			apiHits++
			http.Error(w, "API rate limit exceeded", http.StatusForbidden)
		case "/attson/atterm/releases/latest":
			latestHits++
			http.Redirect(w, r, "/attson/atterm/releases/tag/v0.2.0", http.StatusFound)
		case "/attson/atterm/releases/tag/v0.2.0":
			w.WriteHeader(http.StatusOK)
		case "/releases":
			// refreshLines list endpoint; empty array keeps this test
			// offline without affecting the redirect-fallback assertions.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:     "v0.1.0",
		repo:        "attson/atterm",
		releaseURL:  srv.URL + "/api",
		releasesURL: srv.URL + "/releases",
		latestURL:   srv.URL + "/attson/atterm/releases/latest",
	})
	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check err: %v", err)
	}
	if apiHits != 1 || latestHits != 1 {
		t.Fatalf("apiHits=%d latestHits=%d; want 1 each", apiHits, latestHits)
	}
	st := u.State()
	if st.Latest != "v0.2.0" || !st.Available {
		t.Fatalf("Latest=%q Available=%v; want v0.2.0 true", st.Latest, st.Available)
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := srv.URL + "/attson/atterm/releases/download/v0.2.0/" + name
	if st.AssetURL != wantURL {
		t.Fatalf("AssetURL=%q; want %q", st.AssetURL, wantURL)
	}
	if u.checksumURL != srv.URL+"/attson/atterm/releases/download/v0.2.0/SHA256SUMS" {
		t.Fatalf("checksumURL=%q; want fallback SHA256SUMS URL", u.checksumURL)
	}
	if u.checksumSigURL != srv.URL+"/attson/atterm/releases/download/v0.2.0/SHA256SUMS.sig" {
		t.Fatalf("checksumSigURL=%q; want fallback SHA256SUMS.sig URL", u.checksumSigURL)
	}
}

func TestUpdater_Download_WritesAtomicAsset(t *testing.T) {
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
	// Pretend Check has already populated state.
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
		default:
			return nil, fmt.Errorf("unexpected url %s", url)
		}
	}

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	st := u.State()
	if !st.Ready {
		t.Errorf("Ready = false; want true after successful download")
	}
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
	if st.DownloadPath != nonPartial[0] {
		t.Errorf("DownloadPath = %q; want %q", st.DownloadPath, nonPartial[0])
	}
	got, _ := os.ReadFile(nonPartial[0])
	if string(got) != string(body) {
		t.Errorf("file content = %q; want %q", string(got), string(body))
	}
}

func TestUpdater_Download_UsesGitHubProxyForArchive(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), body)
	original := "https://github.com/attson/atterm/releases/download/v0.2.0/AT-Term-" + runtime.GOOS + "-" + runtime.GOARCH + assetExtForRuntime(t)
	var gotPath string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer proxy.Close()

	u := newUpdater(updaterConfig{
		current:          "v0.1.0",
		repo:             "attson/atterm",
		cacheDir:         t.TempDir(),
		verifyPublicKey:  pub,
		updateGHProxyURL: proxy.URL,
	})
	u.state.AssetURL = original
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
		default:
			return nil, fmt.Errorf("unexpected url %s", url)
		}
	}

	if err := u.Download(context.Background()); err != nil {
		t.Fatalf("Download err: %v", err)
	}
	if !strings.Contains(gotPath, original) {
		t.Fatalf("proxy request path = %q; want it to contain original URL %q", gotPath, original)
	}
}

func TestUpdater_VerificationFetch_UsesGitHubProxyForChecksums(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), body)
	var gotPaths []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "SHA256SUMS.sig"):
			_, _ = w.Write(sig)
		case strings.Contains(r.URL.Path, "SHA256SUMS"):
			_, _ = w.Write(sums)
		default:
			t.Fatalf("unexpected proxy request path %q", r.URL.Path)
		}
	}))
	defer proxy.Close()

	archive := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(archive, body, 0o600); err != nil {
		t.Fatal(err)
	}
	u := newUpdater(updaterConfig{
		current:          "v0.1.0",
		repo:             "attson/atterm",
		verifyPublicKey:  pub,
		updateGHProxyURL: proxy.URL,
	})
	u.checksumURL = "https://github.com/attson/atterm/releases/download/v0.2.0/SHA256SUMS"
	u.checksumSigURL = "https://github.com/attson/atterm/releases/download/v0.2.0/SHA256SUMS.sig"

	if err := u.verifyDownloadedArchive(context.Background(), archive, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t)); err != nil {
		t.Fatalf("verifyDownloadedArchive err: %v", err)
	}
	if len(gotPaths) != 2 {
		t.Fatalf("proxy requests = %d; want 2", len(gotPaths))
	}
	for _, p := range gotPaths {
		if !strings.Contains(p, "https://github.com/attson/atterm/releases/download/v0.2.0/") {
			t.Fatalf("proxy request path = %q; want original GitHub release URL embedded", p)
		}
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
	u.state.AssetSize = int64(len(body) + 100)
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
	matches, _ := filepath.Glob(filepath.Join(tmpCache, "atterm", "updates", "*"))
	if len(matches) != 0 {
		t.Errorf("expected cleanup of partial; got %v", matches)
	}
}

func TestUpdater_Download_FailsWithoutVerificationKey(t *testing.T) {
	body := []byte("fake-archive-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:  "v0.1.0",
		repo:     "attson/atterm",
		cacheDir: t.TempDir(),
	})
	u.state.AssetURL = srv.URL
	u.state.AssetSize = int64(len(body))
	u.state.Latest = "v0.2.0"
	u.checksumURL = "mem://SHA256SUMS"
	u.checksumSigURL = "mem://SHA256SUMS.sig"

	err := u.Download(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verification public key") {
		t.Fatalf("Download err = %v; want missing verification public key", err)
	}
	if u.State().Ready {
		t.Fatal("Ready = true after missing verification key; want false")
	}
}

func TestUpdater_Download_FailsWhenChecksumSignatureInvalid(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), body)
	sig[0] ^= 0xff
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:         "v0.1.0",
		repo:            "attson/atterm",
		cacheDir:        t.TempDir(),
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
		default:
			return nil, fmt.Errorf("unexpected url %s", url)
		}
	}

	err := u.Download(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("Download err = %v; want signature failure", err)
	}
	if u.State().Ready {
		t.Fatal("Ready = true after bad signature; want false")
	}
}

func TestUpdater_Download_FailsWhenAssetHashMismatches(t *testing.T) {
	body := []byte("fake-archive-bytes")
	pub, sums, sig := signedSumsForAsset(t, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH+assetExtForRuntime(t), []byte("different"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:         "v0.1.0",
		repo:            "attson/atterm",
		cacheDir:        t.TempDir(),
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
		default:
			return nil, fmt.Errorf("unexpected url %s", url)
		}
	}

	err := u.Download(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Download err = %v; want checksum failure", err)
	}
	if u.State().Ready {
		t.Fatal("Ready = true after checksum mismatch; want false")
	}
}

// silence unused-import warnings
var _ = io.Discard

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

func TestWindowsInstallHelperSelfElevatesBeforeReplacing(t *testing.T) {
	body, err := os.ReadFile("scripts/install-windows.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	elevateAt := strings.Index(script, "-Verb RunAs")
	if elevateAt < 0 {
		t.Fatal("install-windows.ps1 does not relaunch itself with -Verb RunAs")
	}
	replaceAt := strings.Index(script, "Move-Item")
	if replaceAt < 0 {
		t.Fatal("install-windows.ps1 does not replace the downloaded executable")
	}
	if elevateAt > replaceAt {
		t.Fatal("install-windows.ps1 elevates after replacement; want elevation before touching Program Files")
	}
	for _, arg := range []string{"-ProcessId", "-Src", "-Dst"} {
		if !strings.Contains(script, arg) {
			t.Fatalf("install-windows.ps1 elevated relaunch does not preserve %s", arg)
		}
	}
}

func TestLinuxInstallHelperFallsBackToPkexecWhenDirectReplaceFails(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "mv-first-call")
	pkexecLog := filepath.Join(dir, "pkexec.log")
	setsidLog := filepath.Join(dir, "setsid.log")
	writeExecutable(t, filepath.Join(fakeBin, "mv"), fmt.Sprintf(`#!/bin/sh
if [ ! -f %q ]; then
  echo first > %q
  exit 1
fi
exec /bin/mv "$@"
`, marker, marker))
	writeExecutable(t, filepath.Join(fakeBin, "pkexec"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %q
exec "$@"
`, pkexecLog))
	writeExecutable(t, filepath.Join(fakeBin, "setsid"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %q
exit 0
`, setsidLog))

	payloadDir := filepath.Join(dir, "payload")
	if err := os.Mkdir(payloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "AT Term"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "update.tar.gz")
	run := exec.Command("tar", "-czf", archive, "-C", payloadDir, "AT Term")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	dst := filepath.Join(dir, "AT-Term")
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", "scripts/install-linux.sh", "999999", archive, dst)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(),
		"HOME="+filepath.Join(dir, "home"),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install-linux.sh: %v\nstdout/stderr:\n%s", err, out)
	}

	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("dst content = %q; want new", body)
	}
	if _, err := os.Stat(pkexecLog); err != nil {
		t.Fatalf("pkexec was not invoked after direct replace failed: %v", err)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

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

func signedSumsForAsset(t *testing.T, name string, body []byte) (ed25519.PublicKey, []byte, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	return pub, sums, ed25519.Sign(priv, sums)
}

func assetExtForRuntime(t *testing.T) string {
	t.Helper()
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(name, "AT-Term-"+runtime.GOOS+"-"+runtime.GOARCH)
}

func TestCheck_PopulatesLines(t *testing.T) {
	asset, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	listJSON := `[
	  {"tag_name":"v0.3.0","body":"n30","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.3.0","size":10}]},
	  {"tag_name":"v0.2.155","body":"n2155","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.2.155","size":20}]},
	  {"tag_name":"v0.2.154","body":"n2154","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.2.154","size":30}]}
	]`
	listJSON = strings.ReplaceAll(listJSON, "ASSET_NAME", asset)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Write([]byte(`{"tag_name":"v0.3.0","prerelease":false,"assets":[]}`))
			return
		}
		w.Write([]byte(listJSON))
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:    "v0.2.154",
		repo:       "attson/atterm",
		releaseURL: srv.URL + "/releases/latest",
		client:     srv.Client(),
		now:        time.Now,
	})
	u.cfg.releasesURL = srv.URL + "/releases"

	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check: %v", err)
	}
	lines := u.State().Lines
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].Minor != "v0.3" || lines[1].Minor != "v0.2" {
		t.Fatalf("line order = %q,%q", lines[0].Minor, lines[1].Minor)
	}
	if lines[1].Latest != "v0.2.155" || lines[1].AssetURL != "https://x/v0.2.155" {
		t.Fatalf("v0.2 line = %+v, want v0.2.155 / https://x/v0.2.155", lines[1])
	}
}

func TestParseVersionTag(t *testing.T) {
	cases := []struct {
		tag   string
		minor string
		patch int
		ok    bool
	}{
		{"v0.2.155", "v0.2", 155, true},
		{"v0.3.0", "v0.3", 0, true},
		{"v1.10.3", "v1.10", 3, true},
		{"0.2.155", "", 0, false},
		{"v0.2", "", 0, false},
		{"dev", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		minor, patch, ok := parseVersionTag(c.tag)
		if ok != c.ok || minor != c.minor || patch != c.patch {
			t.Errorf("parseVersionTag(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.tag, minor, patch, ok, c.minor, c.patch, c.ok)
		}
	}
}

func TestGroupLines(t *testing.T) {
	releases := []lineCandidate{
		{tag: "v0.2.155", assetURL: "u-2-155", notes: "n2155"},
		{tag: "v0.2.154", assetURL: "u-2-154", notes: "n2154"},
		{tag: "v0.3.0", assetURL: "u-3-0", notes: "n30"},
		{tag: "v0.2.153", assetURL: "u-2-153", notes: "n2153"},
	}

	// current = v0.2.154 → v0.2 线最新(v0.2.155, >current) + v0.3(高线)
	got := groupLines(releases, "v0.2.154")
	if len(got) != 2 {
		t.Fatalf("current v0.2.154: got %d lines, want 2: %+v", len(got), got)
	}
	// 按 minor 降序(高线在前)
	if got[0].Minor != "v0.3" || got[0].Latest != "v0.3.0" {
		t.Errorf("line[0] = %+v, want v0.3 → v0.3.0", got[0])
	}
	if got[1].Minor != "v0.2" || got[1].Latest != "v0.2.155" || got[1].AssetURL != "u-2-155" {
		t.Errorf("line[1] = %+v, want v0.2 → v0.2.155 (u-2-155)", got[1])
	}

	// current = v0.3.0 → 只有 v0.3 线,其最新 v0.3.0 == current 故不显示
	got = groupLines(releases, "v0.3.0")
	if len(got) != 0 {
		t.Fatalf("current v0.3.0 (already highest+latest): got %d lines, want 0: %+v", len(got), got)
	}

	// current = dev → 显示所有线最新
	got = groupLines(releases, "dev")
	if len(got) != 2 {
		t.Fatalf("current dev: got %d lines, want 2: %+v", len(got), got)
	}
	if got[0].Minor != "v0.3" || got[1].Minor != "v0.2" {
		t.Errorf("dev lines order = %q,%q want v0.3,v0.2", got[0].Minor, got[1].Minor)
	}
}

func TestDownloadVersion_SetsAssetForTag(t *testing.T) {
	asset, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	listJSON := `[
	  {"tag_name":"v0.3.0","prerelease":false,"draft":false,
	   "assets":[{"name":"` + asset + `","browser_download_url":"https://x/v0.3.0","size":10}]},
	  {"tag_name":"v0.2.155","prerelease":false,"draft":false,
	   "assets":[{"name":"` + asset + `","browser_download_url":"https://x/v0.2.155","size":20},
	             {"name":"SHA256SUMS","browser_download_url":"https://x/sums-2-155","size":1},
	             {"name":"SHA256SUMS.sig","browser_download_url":"https://x/sig-2-155","size":1}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(listJSON))
	}))
	defer srv.Close()
	u := newUpdater(updaterConfig{current: "v0.2.154", repo: "attson/atterm",
		releasesURL: srv.URL + "/releases", client: srv.Client(), now: time.Now})

	if err := u.prepareVersion(context.Background(), "v0.2.155"); err != nil {
		t.Fatalf("prepareVersion: %v", err)
	}
	if u.state.AssetURL != "https://x/v0.2.155" {
		t.Fatalf("AssetURL = %q, want https://x/v0.2.155", u.state.AssetURL)
	}
	if u.checksumURL != "https://x/sums-2-155" || u.checksumSigURL != "https://x/sig-2-155" {
		t.Fatalf("checksum URLs not set: %q / %q", u.checksumURL, u.checksumSigURL)
	}
}
