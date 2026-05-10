package main

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

	mu         sync.Mutex
	state      UpdateState
	cachedAt   time.Time // when we last fetched the latest-release manifest
	cancelLoop context.CancelFunc
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
	// Detach: don't Wait(). The helper waits on our PID via kill -0 / Get-Process
	// loop and survives our exit because the relaunch path inside the helper
	// uses platform-native detachment (`open` on darwin, `setsid &` on linux,
	// `Start-Process` on windows).
	return nil
}

// buildHelperCommand returns the platform-appropriate exec.Cmd for invoking
// the install helper script.
func buildHelperCommand(helperPath, pid, src, dst string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin", "linux":
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
	return exec.Command("false")
}

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
