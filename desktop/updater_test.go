package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// silence-the-unused-import warnings for httptest in later tasks.
var _ = httptest.NewServer

func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "atterm-desktop-linux-amd64.tar.gz"},
		{"darwin", "arm64", "atterm-desktop-darwin-arm64.zip"},
		{"windows", "amd64", "atterm-desktop-windows-amd64.zip"},
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
				"name":                 "atterm-desktop-darwin-arm64.zip",
				"browser_download_url": "https://example.com/" + tag + "/darwin.zip",
				"size":                 int64(12345),
			},
			{
				"name":                 "atterm-desktop-linux-amd64.tar.gz",
				"browser_download_url": "https://example.com/" + tag + "/linux.tar.gz",
				"size":                 int64(54321),
			},
			{
				"name":                 "atterm-desktop-windows-amd64.zip",
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

// silence unused-import warnings
var _ = io.Discard

func TestInstallPathFromExecutable_Darwin(t *testing.T) {
	got := installPathFromExecutable("/Applications/atterm-desktop.app/Contents/MacOS/atterm-desktop", "darwin")
	want := "/Applications/atterm-desktop.app"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_DarwinDeepPath(t *testing.T) {
	got := installPathFromExecutable("/Users/x/Applications/atterm-desktop.app/Contents/MacOS/atterm-desktop", "darwin")
	want := "/Users/x/Applications/atterm-desktop.app"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_Linux(t *testing.T) {
	got := installPathFromExecutable("/home/x/.local/bin/atterm-desktop", "linux")
	want := "/home/x/.local/bin/atterm-desktop"
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}

func TestInstallPathFromExecutable_Windows(t *testing.T) {
	got := installPathFromExecutable(`C:\Users\x\atterm-desktop.exe`, "windows")
	want := `C:\Users\x\atterm-desktop.exe`
	if got != want {
		t.Errorf("install path = %q; want %q", got, want)
	}
}
