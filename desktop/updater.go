package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"golang.org/x/mod/semver"
)

//go:embed scripts/install-darwin.sh
//go:embed scripts/install-linux.sh
//go:embed scripts/install-windows.ps1
var installScripts embed.FS

// UpdateVerifyPublicKey is set by release builds to the base64-encoded
// Ed25519 public key that signs SHA256SUMS. Empty disables installation.
var UpdateVerifyPublicKey string

// UpdateState is the observable view of the auto-update subsystem.
// Mirrored as a TypeScript interface in desktop/frontend/src/lib/api.ts.
type UpdateState struct {
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	Available    bool   `json:"available"`
	Notes        string `json:"notes"`
	Checking     bool   `json:"checking"`
	LastCheckAt  int64  `json:"last_check_at"`
	Downloading  bool   `json:"downloading"`
	DownloadPct  int    `json:"download_pct"`
	Ready        bool   `json:"ready"`
	Error        string `json:"error"`
	AssetURL     string `json:"asset_url"`
	AssetSize    int64  `json:"asset_size"`
	DownloadDir  string `json:"download_dir"`
	DownloadPath string `json:"download_path"`
}

// updaterConfig is the constructor-time bag for Updater. Test code
// substitutes the http.Client and now() to drive the cache + network paths
// without hitting GitHub or wall-clock time.
type updaterConfig struct {
	current          string
	repo             string // e.g. "attson/atterm"
	releaseURL       string // override of https://api.github.com/repos/<repo>/releases/latest, for tests
	latestURL        string // override of https://github.com/<repo>/releases/latest, for tests
	cacheDir         string // overrides os.UserCacheDir(); for tests
	client           *http.Client
	now              func() time.Time // optional; defaults to time.Now
	verifyPublicKey  ed25519.PublicKey
	updateGHProxyURL string
}

// Updater owns state for the auto-update flow. All methods are goroutine-safe.
type Updater struct {
	cfg updaterConfig

	mu         sync.Mutex
	state      UpdateState
	cachedAt   time.Time // when we last fetched the latest-release manifest
	cancelLoop context.CancelFunc

	checksumURL    string
	checksumSigURL string
	fetchBytes     func(ctx context.Context, url string, maxBytes int64) ([]byte, error)
}

func newUpdater(cfg updaterConfig) *Updater {
	if cfg.client == nil {
		cfg.client = &http.Client{}
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

func (u *Updater) SetGHProxyURL(proxyURL string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.cfg.updateGHProxyURL = strings.TrimSpace(proxyURL)
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
		return "AT-Term-linux-amd64.tar.gz", nil
	case goos == "linux" && goarch == "arm64":
		return "AT-Term-linux-arm64.tar.gz", nil
	case goos == "darwin" && goarch == "arm64":
		return "AT-Term-darwin-arm64.zip", nil
	case goos == "darwin" && goarch == "amd64":
		return "AT-Term-darwin-amd64.zip", nil
	case goos == "windows" && goarch == "amd64":
		return "AT-Term-windows-amd64.zip", nil
	}
	return "", fmt.Errorf("no atterm build for %s/%s", goos, goarch)
}

const (
	releaseCacheTTL        = 1 * time.Hour
	updaterCheckTimeout    = 15 * time.Second
	updaterDownloadTimeout = 10 * time.Minute
)

// githubReleaseAPI returns the URL to fetch the latest release manifest.
func (u *Updater) githubReleaseAPI() string {
	if u.cfg.releaseURL != "" {
		return u.cfg.releaseURL
	}
	return "https://api.github.com/repos/" + u.cfg.repo + "/releases/latest"
}

// githubLatestURL returns the browser endpoint whose redirect target carries
// the latest tag without consuming GitHub's unauthenticated API quota.
func (u *Updater) githubLatestURL() string {
	if u.cfg.latestURL != "" {
		return u.cfg.latestURL
	}
	return "https://github.com/" + u.cfg.repo + "/releases/latest"
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

	checkCtx, cancelCheck := context.WithTimeout(ctx, updaterCheckTimeout)
	rel, err := u.fetchLatest(checkCtx)
	cancelCheck()

	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Checking = false
	u.state.LastCheckAt = u.cfg.now().Unix()
	if err != nil {
		u.state.Error = err.Error()
		return err
	}
	u.cachedAt = u.cfg.now()
	u.checksumURL = ""
	u.checksumSigURL = ""
	u.state.AssetURL = ""
	u.state.AssetSize = 0

	if rel.Prerelease {
		// Don't expose pre-releases as "available" in v0.
		u.state.Latest = ""
		u.state.Available = false
		u.state.Notes = ""
		return nil
	}

	u.state.Latest = rel.TagName
	u.state.Notes = rel.Body
	u.state.Available = semver.IsValid(rel.TagName) &&
		semver.IsValid(u.cfg.current) &&
		semver.Compare(u.cfg.current, rel.TagName) < 0

	for _, a := range rel.Assets {
		switch a.Name {
		case "SHA256SUMS":
			u.checksumURL = a.DownloadURL
		case "SHA256SUMS.sig":
			u.checksumSigURL = a.DownloadURL
		}
	}

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
	u.clearStaleReadyLocked()
	return nil
}

func (u *Updater) clearStaleReadyLocked() {
	if !u.state.Ready {
		return
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		u.state.Ready = false
		u.state.DownloadPct = 0
		u.state.DownloadPath = ""
		return
	}
	dir, err := u.updatesDir()
	if err != nil {
		u.state.Ready = false
		u.state.DownloadPct = 0
		u.state.DownloadPath = ""
		return
	}
	expected := filepath.Join(dir, u.state.Latest+"-"+name)
	if u.state.DownloadPath == expected {
		return
	}
	u.state.Ready = false
	u.state.DownloadPct = 0
	u.state.DownloadPath = ""
}

func (u *Updater) fetchLatest(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.githubReleaseAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == http.StatusForbidden || u.cfg.latestURL != "" {
			if rel, ferr := u.fetchLatestViaRedirect(ctx); ferr == nil {
				return rel, nil
			}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return nil, fmt.Errorf("github returned http %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("github returned http %d: %s", resp.StatusCode, detail)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (u *Updater) fetchLatestViaRedirect(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", u.githubLatestURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github latest redirect returned http %d", resp.StatusCode)
	}
	final := resp.Request.URL
	tag, err := tagFromReleaseURL(final)
	if err != nil {
		return nil, err
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return &githubRelease{
		TagName: tag,
		Assets: []githubAsset{
			{
				Name:        name,
				DownloadURL: assetDownloadURL(final, tag, name),
			},
			{
				Name:        "SHA256SUMS",
				DownloadURL: assetDownloadURL(final, tag, "SHA256SUMS"),
			},
			{
				Name:        "SHA256SUMS.sig",
				DownloadURL: assetDownloadURL(final, tag, "SHA256SUMS.sig"),
			},
		},
	}, nil
}

func tagFromReleaseURL(u *url.URL) (string, error) {
	const marker = "/releases/tag/"
	idx := strings.LastIndex(u.Path, marker)
	if idx < 0 {
		return "", fmt.Errorf("github latest redirect target %q has no release tag", u.String())
	}
	tag, err := url.PathUnescape(strings.Trim(u.Path[idx+len(marker):], "/"))
	if err != nil {
		return "", err
	}
	if tag == "" {
		return "", fmt.Errorf("github latest redirect target %q has empty release tag", u.String())
	}
	return tag, nil
}

func assetDownloadURL(final *url.URL, tag, name string) string {
	const marker = "/releases/tag/"
	idx := strings.LastIndex(final.Path, marker)
	prefix := ""
	if idx >= 0 {
		prefix = final.Path[:idx]
	}
	out := *final
	out.Path = prefix + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
	out.RawPath = ""
	out.RawQuery = ""
	out.Fragment = ""
	return out.String()
}

func proxiedGitHubReleaseURL(rawURL, proxyBase string) string {
	proxyBase = strings.TrimSpace(proxyBase)
	if proxyBase == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || strings.ToLower(u.Host) != "github.com" || !strings.Contains(u.Path, "/releases/download/") {
		return rawURL
	}
	return strings.TrimRight(proxyBase, "/") + "/" + rawURL
}

func (u *Updater) downloadURL(rawURL string) string {
	u.mu.Lock()
	proxyURL := u.cfg.updateGHProxyURL
	u.mu.Unlock()
	return proxiedGitHubReleaseURL(rawURL, proxyURL)
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
	dir := filepath.Join(base, appdir.Name(), "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Download fetches the asset URL recorded in state to the cache dir,
// streaming through a .partial file before atomic-renaming on success.
// Reports size-mismatch errors loudly so the UI can offer Retry.
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

	req, err := http.NewRequestWithContext(downloadCtx, "GET", u.downloadURL(url), nil)
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

	if err := u.verifyDownloadedArchive(downloadCtx, partial, name); err != nil {
		_ = os.Remove(partial)
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

func (u *Updater) verifyDownloadedArchive(ctx context.Context, path, assetName string) error {
	key := u.cfg.verifyPublicKey
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("update verification public key not configured")
	}
	u.mu.Lock()
	checksumURL := u.checksumURL
	checksumSigURL := u.checksumSigURL
	u.mu.Unlock()
	if checksumURL == "" || checksumSigURL == "" {
		return fmt.Errorf("release is missing SHA256SUMS verification assets")
	}
	fetch := u.fetchBytes
	if fetch == nil {
		fetch = u.fetchSmallFile
	}
	sums, err := fetch(ctx, checksumURL, 1024*1024)
	if err != nil {
		return fmt.Errorf("fetch SHA256SUMS: %w", err)
	}
	sig, err := fetch(ctx, checksumSigURL, 1024)
	if err != nil {
		return fmt.Errorf("fetch SHA256SUMS signature: %w", err)
	}
	if !ed25519.Verify(key, sums, sig) {
		return fmt.Errorf("SHA256SUMS signature verification failed")
	}
	want, err := checksumForAsset(sums, assetName)
	if err != nil {
		return err
	}
	got, err := sha256File(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("asset checksum mismatch: got %s want %s", got, want)
	}
	return nil
}

func (u *Updater) fetchSmallFile(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.downloadURL(rawURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func checksumForAsset(sums []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) != assetName {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != sha256.Size*2 {
			return "", fmt.Errorf("invalid checksum for %s", assetName)
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return "", fmt.Errorf("invalid checksum for %s: %w", assetName, err)
		}
		return sum, nil
	}
	return "", fmt.Errorf("checksum for %s not found in SHA256SUMS", assetName)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseUpdateVerifyPublicKey(encoded string) ed25519.PublicKey {
	if strings.TrimSpace(encoded) == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
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
