package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	Checking    bool   `json:"checking"`
	LastCheckAt int64  `json:"last_check_at"`
	Downloading bool   `json:"downloading"`
	DownloadPct int    `json:"download_pct"`
	Ready       bool   `json:"ready"`
	// DownloadedExists signals that the most recent DownloadVersion /
	// StartDownload call short-circuited to Ready because a validated
	// archive was already on disk. The frontend watches false→true to
	// decide whether to prompt "redownload?" Cleared at Download's HTTP
	// entry and by ForceRedownload. Never touched by Cancel.
	DownloadedExists bool   `json:"downloaded_exists"`
	Error            string `json:"error"`
	AssetURL         string `json:"asset_url"`
	AssetSize        int64  `json:"asset_size"`
	DownloadDir      string `json:"download_dir"`
	DownloadPath     string `json:"download_path"`

	Lines []VersionLine `json:"lines"`
}

// updaterConfig is the constructor-time bag for Updater. Test code
// substitutes the http.Client and now() to drive the cache + network paths
// without hitting GitHub or wall-clock time.
type updaterConfig struct {
	current          string
	repo             string // e.g. "attson/atterm"
	releaseURL       string // override of https://api.github.com/repos/<repo>/releases/latest, for tests
	releasesURL      string // override of https://api.github.com/repos/<repo>/releases, for tests
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
	// cancelDownload interrupts an in-flight Download. Set at Download's
	// HTTP entry, cleared on Download return, called by Cancel(). nil when
	// no download is running.
	cancelDownload context.CancelFunc

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
	// refreshLines issues its own HTTP request; call it here while NOT holding
	// u.mu so the slow network round-trip never blocks other goroutines. It
	// returns nil on any failure (graceful degradation).
	lines := u.refreshLines(checkCtx)
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
	// lines was fetched above without holding the lock; assign under the lock.
	u.state.Lines = lines
	u.applyReleaseLocked(rel)
	return nil
}

// applyReleaseLocked maps a single *githubRelease into state: clearing the
// previous asset/checksum URLs, then (for non-prereleases) recording
// Latest/Notes/Available, the SHA256SUMS verification URLs, and the
// platform asset URL/size. Pre-releases clear Latest/Available/Notes and
// return early. Used by both Check (latest release) and prepareVersion (a
// specific tag). Caller must hold u.mu.
func (u *Updater) applyReleaseLocked(rel *githubRelease) {
	u.checksumURL = ""
	u.checksumSigURL = ""
	u.state.AssetURL = ""
	u.state.AssetSize = 0

	if rel.Prerelease {
		// Don't expose pre-releases as "available" in v0.
		u.state.Latest = ""
		u.state.Available = false
		u.state.Notes = ""
		return
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

// tryClaimExistingArchive checks whether the target archive for latest
// already exists on disk and verifies OK. Returns (hit=true, nil) if
// state was updated to Ready and no HTTP download is needed. Returns
// (hit=false, nil) if there is nothing on disk or the file exists but
// fails verification (in which case the corrupted file is removed).
// Returns (hit=false, err) only for infrastructure errors (updatesDir
// lookup, platform asset name lookup).
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

// prepareVersion fetches the releases list, finds the given tag, and applies
// its asset/checksum/notes into state so a subsequent Download targets that
// exact version (the chosen line's latest). Used by DownloadVersion.
func (u *Updater) prepareVersion(ctx context.Context, tag string) error {
	rels, err := u.fetchReleases(ctx) // not under lock; issues HTTP
	if err != nil {
		return err
	}
	var found *githubRelease
	for i := range rels {
		if rels[i].TagName == tag {
			found = &rels[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("version %s not found in releases", tag)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.applyReleaseLocked(found)
	return nil
}

// DownloadVersion prepares the given tag (the chosen update line's latest)
// then starts the download for it instead of the default latest.
func (u *Updater) DownloadVersion(ctx context.Context, tag string) error {
	if err := u.prepareVersion(ctx, tag); err != nil {
		return err
	}
	return u.Download(ctx)
}

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

// installPathFromExecutable maps os.Executable() output to the path the
// install helper must replace. On macOS the running binary lives inside
// .app/Contents/MacOS/, but the helper replaces the .app bundle as a
// whole — walk back to it.
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
