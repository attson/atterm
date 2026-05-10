package main

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

//go:embed scripts/install-darwin.sh
//go:embed scripts/install-linux.sh
//go:embed scripts/install-windows.ps1
var installScripts embed.FS

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
	current    string
	repo       string // e.g. "attson/atterm"
	releaseURL string // override of https://api.github.com/repos/<repo>/releases/latest, for tests
	client     *http.Client
	now        func() time.Time // optional; defaults to time.Now
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

// assetNameForPlatform returns the GitHub Release asset filename for the
// given runtime.GOOS/GOARCH pair. Unsupported pairs return an error so the
// UI can display "no build for $platform" instead of silently picking a
// wrong artifact.
func assetNameForPlatform(goos, goarch string) (string, error) {
	switch {
	case goos == "linux" && goarch == "amd64":
		return "atterm-desktop-linux-amd64.tar.gz", nil
	case goos == "darwin" && goarch == "arm64":
		return "atterm-desktop-darwin-arm64.zip", nil
	case goos == "windows" && goarch == "amd64":
		return "atterm-desktop-windows-amd64.zip", nil
	}
	return "", fmt.Errorf("no atterm build for %s/%s", goos, goarch)
}

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

// Check fetches the latest release. force=true bypasses the 1h response cache.
// In dev/empty builds it's a no-op that never touches the network.
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
	req.Header.Set("User-Agent", "atterm-desktop/"+u.cfg.current)
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
