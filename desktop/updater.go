package main

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
	cacheDir   string // overrides os.UserCacheDir(); for tests
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
