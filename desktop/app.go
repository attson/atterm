package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Endpoint is what the frontend uses to open a WebSocket to the in-process relay.
type Endpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// NewSessionReq is the body of NewSession.
type NewSessionReq struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Cols    uint16   `json:"cols,omitempty"`
	Rows    uint16   `json:"rows,omitempty"`
}

// NewSessionResp is returned by NewSession.
type NewSessionResp struct {
	SessionID string `json:"session_id"`
}

// HostInfo describes this machine. The frontend uses HostID to dedupe
// remote-relay session listings (sessions whose host_id matches us are
// just mirrors of our own and are reachable through the local mini-relay).
type HostInfo struct {
	HostID string `json:"host_id"`
	Host   string `json:"host"`
	User   string `json:"user"`
}

// RelayConfig is the user-editable view of the persisted uplink configuration.
// Connected reflects whether the uplink goroutine is currently running; it is
// read-only from the frontend's perspective.
// Paused reflects whether the user has toggled the uplink off without clearing
// the URL/token (the "pause without erasing config" state).
type RelayConfig struct {
	URL                string `json:"url"`
	Token              string `json:"token"`
	AllowInsecureRelay bool   `json:"allow_insecure_relay"`
	RemotePermission   string `json:"remote_permission"`
	Connected          bool   `json:"connected"`
	Paused             bool   `json:"paused"`
}

type LoggingConfig struct {
	Enabled       bool   `json:"enabled"`
	Path          string `json:"path"`
	EffectivePath string `json:"effective_path"`
	DevDualOutput bool   `json:"dev_dual_output"`
}

type LogPreview struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

// App is the Wails-bound application surface.
type App struct {
	ctx      context.Context
	host     *relayHost
	cfgStore *configStore
	logger   *loggingManager

	mu           sync.Mutex
	uplink       *uplink
	uplinkCancel context.CancelFunc

	updater *Updater

	// quitApproved gates OnBeforeClose: once the frontend confirms a quit
	// via ConfirmQuit(), subsequent close attempts proceed without the
	// before-close prompt round trip.
	quitApproved atomic.Bool

	pluginFS *PluginFS
}

// NewApp creates a new App application struct.
func NewApp(cfgStore *configStore, logger *loggingManager) *App {
	a := &App{cfgStore: cfgStore, logger: logger, pluginFS: NewPluginFS()}
	a.updater = newUpdater(updaterConfig{
		current:         Version,
		repo:            "attson/atterm",
		verifyPublicKey: parseUpdateVerifyPublicKey(UpdateVerifyPublicKey),
	})
	return a
}

// startup is called when the Wails runtime is ready. Boot the in-process
// relay, load persisted config, and apply it (which may start the uplink).
// ATTERM_RELAY_URL/TOKEN env vars are honored only when no config file
// exists yet — they seed the first run.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.pluginFS.setupWatcher(ctx)
	h, err := startRelayHost()
	if err != nil {
		log.Fatalf("desktop: start relay host: %v", err)
	}
	a.host = h
	if a.cfgStore == nil {
		a.cfgStore = loadConfig()
	}
	a.host.setConfigStore(a.cfgStore)

	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" {
		if env := strings.TrimSpace(os.Getenv("ATTERM_RELAY_URL")); env != "" {
			cfg.RelayURL = env
			cfg.RelayToken = strings.TrimSpace(os.Getenv("ATTERM_RELAY_TOKEN"))
		}
	}
	if a.logger == nil {
		a.logger, err = newDesktopLoggingManager(cfg, Version)
		if err != nil {
			log.Fatalf("desktop: init logging: %v", err)
		}
	}
	a.applyRelayConfig(cfg)

	// Auto-update background loop, gated on the persisted preference.
	// New installs default to enabled (AutoCheckUpdatesOrDefault returns true).
	if a.updater != nil && cfg.AutoCheckUpdatesOrDefault() {
		a.updater.Start(ctx)
	}
}

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
	if a.logger != nil {
		_ = a.logger.Close()
		a.logger = nil
	}
	if a.host != nil {
		a.host.Stop()
		a.host = nil
	}
}

// applyRelayConfig (re)starts the uplink to match the given config. URL == ""
// means "no uplink" — any running one is cancelled. Caller need not hold a.mu.
func (a *App) applyRelayConfig(cfg appConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.uplinkCancel != nil {
		a.uplinkCancel()
		a.uplinkCancel = nil
		a.uplink = nil
	}
	if cfg.RelayURL == "" || cfg.RelayPaused {
		reason := "no URL"
		if cfg.RelayPaused {
			reason = "paused by user"
		}
		log.Printf("desktop: uplink disabled (%s)", reason)
		return
	}
	if err := validateRelayEndpoint(cfg.RelayURL, cfg.AllowInsecureRelay); err != nil {
		log.Printf("desktop: uplink disabled: %v", err)
		return
	}
	uplinkCtx, cancel := context.WithCancel(a.ctx)
	a.uplinkCancel = cancel
	a.uplink = newUplink(cfg.RelayURL, cfg.RelayToken, cfg.RemotePermissionOrDefault(), a.host)
	go a.uplink.Run(uplinkCtx)
	log.Printf("desktop: uplink configured for %s", cfg.RelayURL)
}

// GetHostInfo returns this machine's identity. Used for deduping remote
// session lists.
func (a *App) GetHostInfo() HostInfo {
	if a.host == nil {
		return HostInfo{}
	}
	id, h, u := a.host.HostMeta()
	return HostInfo{HostID: id, Host: h, User: u}
}

// GetEndpoint returns the local relay endpoint and a token. The frontend uses
// this to open a WebSocket to the in-process relay.
func (a *App) GetEndpoint() Endpoint {
	if a.host == nil {
		return Endpoint{}
	}
	return Endpoint{URL: "ws://" + a.host.addr, Token: a.host.token}
}

// GetRelayConfig returns the currently-persisted relay URL/token plus whether
// the uplink is currently running.
func (a *App) GetRelayConfig() RelayConfig {
	cfg := appConfig{}
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	a.mu.Lock()
	connected := a.uplink != nil
	a.mu.Unlock()
	return RelayConfig{
		URL:                cfg.RelayURL,
		Token:              cfg.RelayToken,
		AllowInsecureRelay: cfg.AllowInsecureRelay,
		RemotePermission:   cfg.RemotePermissionOrDefault(),
		Connected:          connected,
		Paused:             cfg.RelayPaused,
	}
}

// SetRelayConfig persists a new relay URL/token and (re)starts the uplink. To
// disable the uplink entirely, pass URL="".
func (a *App) SetRelayConfig(req RelayConfig) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	cfg.RelayURL = strings.TrimSpace(req.URL)
	cfg.RelayToken = strings.TrimSpace(req.Token)
	cfg.AllowInsecureRelay = req.AllowInsecureRelay
	switch req.RemotePermission {
	case proto.RemotePermissionView, proto.RemotePermissionControl, proto.RemotePermissionFull:
		cfg.RemotePermission = req.RemotePermission
	case "":
		cfg.RemotePermission = proto.RemotePermissionFull
	default:
		return fmt.Errorf("bad remote permission: %s", req.RemotePermission)
	}
	if err := validateRelayEndpoint(cfg.RelayURL, cfg.AllowInsecureRelay); err != nil {
		return err
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.applyRelayConfig(cfg)
	return nil
}

// SetUplinkPaused toggles the user-controlled pause flag without touching the
// relay URL, token, insecure flag, or remote permission. This fixes the
// "disconnect erases config" UX bug: the URL and token survive across
// pause/unpause cycles, and the relay reconnects immediately on unpause.
func (a *App) SetUplinkPaused(paused bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	cfg.RelayPaused = paused
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.applyRelayConfig(cfg)
	return nil
}

func (a *App) GetLoggingConfig() LoggingConfig {
	cfg := appConfig{}
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	effectivePath := cfg.LogFilePathOrDefault()
	if a.logger != nil {
		effectivePath = a.logger.EffectivePath(cfg.LogFilePath)
	}
	return LoggingConfig{
		Enabled:       cfg.LogToFileEnabledOrDefault(),
		Path:          cfg.LogFilePath,
		EffectivePath: effectivePath,
		DevDualOutput: isDevBuild(Version),
	}
}

func (a *App) SetLoggingConfig(req LoggingConfig) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	path := strings.TrimSpace(req.Path)
	if path != "" && !filepath.IsAbs(path) {
		return fmt.Errorf("log path must be absolute")
	}

	prevCfg := a.cfgStore.Get()
	prevState := loggingConfigState{
		enabled: prevCfg.LogToFileEnabledOrDefault(),
		path:    prevCfg.LogFilePath,
	}
	nextCfg := prevCfg
	nextCfg.LogFilePath = path
	nextCfg.LogToFileEnabled = &req.Enabled

	if a.logger != nil {
		if err := a.logger.Apply(loggingConfigState{
			enabled: req.Enabled,
			path:    path,
		}); err != nil {
			return err
		}
	}
	if err := a.cfgStore.Set(nextCfg); err != nil {
		if a.logger != nil {
			_ = a.logger.Apply(prevState)
		}
		return err
	}
	return nil
}

func (a *App) PickLogFilePath() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("runtime context not ready")
	}
	current := a.GetLoggingConfig().EffectivePath
	return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:            "Choose desktop log file",
		DefaultDirectory: filepath.Dir(current),
		DefaultFilename:  filepath.Base(current),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Log Files (*.log)", Pattern: "*.log"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
		CanCreateDirectories: true,
		ShowHiddenFiles:      true,
	})
}

func (a *App) GetLogPreview() (LogPreview, error) {
	if a.cfgStore == nil {
		return LogPreview{}, fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	path := cfg.LogFilePathOrDefault()
	if a.logger != nil {
		path = a.logger.EffectivePath(cfg.LogFilePath)
	}
	preview, err := readLogPreview(path, defaultLogPreviewBytes)
	if err != nil {
		return LogPreview{}, err
	}
	return LogPreview{
		Path:      preview.Path,
		Exists:    preview.Exists,
		Truncated: preview.Truncated,
		Content:   preview.Content,
	}, nil
}

func (a *App) GetClipboardPastePayload() (ClipboardPastePayload, error) {
	if a.ctx == nil {
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: "runtime context not ready",
		}, nil
	}
	return readClipboardPastePayloadFromRuntime(a.ctx), nil
}

// GetTerminalTheme returns the user's global terminal theme preference.
func (a *App) GetTerminalTheme() string {
	if a.cfgStore == nil {
		return terminalThemeClassic
	}
	return a.cfgStore.Get().TerminalThemeOrDefault()
}

// SetTerminalTheme persists the user's global terminal theme preference.
func (a *App) SetTerminalTheme(theme string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	theme = strings.TrimSpace(theme)
	if !isSupportedTerminalTheme(theme) {
		return fmt.Errorf("bad terminal theme: %s", theme)
	}
	cfg := a.cfgStore.Get()
	cfg.TerminalTheme = theme
	return a.cfgStore.Set(cfg)
}

// NewSession spawns a local PTY child and adopts it as a relay session.
// Returns the session id, which the frontend uses to ATTACH via WS.
func (a *App) NewSession(req NewSessionReq) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	sid, err := a.host.NewSession(a.ctx, req)
	if err != nil {
		return NewSessionResp{}, err
	}
	return NewSessionResp{SessionID: sid.String()}, nil
}

// CloseSession terminates the PTY backing a session. The frontend uses this
// when the user clicks a tab's close button.
func (a *App) CloseSession(sessionID string) error {
	if a.host == nil {
		return fmt.Errorf("relay host not ready")
	}
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("bad session id: %w", err)
	}
	return a.host.CloseSession(id)
}

// ListShells returns absolute paths of candidate shells available on this
// machine, in priority order: $SHELL first, then well-known shells.
func (a *App) ListShells() []string {
	candidates := []string{"bash", "zsh", "fish", "sh"}
	if runtime.GOOS == "windows" {
		candidates = []string{"powershell.exe", "cmd.exe"}
	}
	var out []string
	seen := map[string]bool{}
	if envShell := os.Getenv("SHELL"); envShell != "" {
		out = append(out, envShell)
		seen[filepath.Base(envShell)] = true
	}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			out = append(out, path)
			seen[c] = true
		}
	}
	return out
}

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

// InstallUpdate spawns the install helper detached and quits the app.
// The helper waits for our PID to exit then replaces the install and
// relaunches.
func (a *App) InstallUpdate() error {
	if a.updater == nil {
		return fmt.Errorf("updater not initialized")
	}
	if err := a.updater.InstallAndQuit(); err != nil {
		return err
	}
	// Quit ourselves so the helper's wait-for-PID-exit loop unblocks.
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

// beforeClose is wired to wails options.OnBeforeClose. If a previous
// ConfirmQuit() call set quitApproved, the close proceeds. Otherwise it
// emits a before-close event to the frontend and tells Wails to abort
// this close. The emit function is a parameter so tests can verify the
// gating without bringing up a Wails runtime.
func (a *App) beforeClose(ctx context.Context, emit func()) bool {
	if a.quitApproved.Load() {
		return false
	}
	emit()
	return true
}

// ConfirmQuit is called from the frontend after the user confirms the
// quit dialog. Sets the gating flag and asks Wails to quit; the next
// OnBeforeClose call returns false and the framework proceeds.
func (a *App) ConfirmQuit() {
	a.quitApproved.Store(true)
	if a.ctx == nil {
		return
	}
	wailsruntime.Quit(a.ctx)
}

// GetNotificationsEnabled returns the current persisted preference.
// Defaults to true for fresh installs.
func (a *App) GetNotificationsEnabled() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().NotificationsEnabledOrDefault()
}

// SetNotificationsEnabled persists the user's toggle.
func (a *App) SetNotificationsEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.NotificationsEnabled = &enabled
	return a.cfgStore.Set(cfg)
}

// ShowNotification is called from the frontend when a terminal bell fires
// and the window is unfocused. Routes through the platform-native
// notification system. Failures are logged, never propagated.
func (a *App) ShowNotification(title, body string) error {
	if a.ctx == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	return showNotification(
		ctx,
		func() bool {
			if a.cfgStore == nil {
				return true
			}
			return a.cfgStore.Get().NotificationsEnabledOrDefault()
		},
		runNativeNotify,
		title,
		body,
	)
}

// GetShellIntegrationEnabled returns the current persisted preference for
// OSC 133 shell-hook injection. Defaults to true for fresh installs.
func (a *App) GetShellIntegrationEnabled() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().ShellIntegrationEnabledOrDefault()
}

// SetShellIntegrationEnabled persists the user's toggle. Already-running
// sessions are unaffected; only newly spawned shells use the new value.
func (a *App) SetShellIntegrationEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.ShellIntegrationEnabled = &enabled
	return a.cfgStore.Set(cfg)
}

// GetCommandNotifyThresholdSeconds returns the current persisted command-
// finished notification threshold. Clamped to [1, 600] at read time;
// defaults to 10 for fresh installs.
func (a *App) GetCommandNotifyThresholdSeconds() int {
	if a.cfgStore == nil {
		return 10
	}
	return a.cfgStore.Get().CommandNotifyThresholdSecondsOrDefault()
}

// SetCommandNotifyThresholdSeconds persists the user's threshold. The
// stored value is clamped on read, so out-of-range writes (e.g. from a
// stale UI) are tolerated.
func (a *App) SetCommandNotifyThresholdSeconds(seconds int) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.CommandNotifyThresholdSeconds = &seconds
	return a.cfgStore.Set(cfg)
}

// BroadcastCommandFinished is invoked by the desktop frontend when an OSC
// 133 command-finished event passes the local notification gate. Sends a
// TypeCommandEvent frame to the configured remote relay via the uplink so
// the relay can fan out Web Push notifications to subscribed browsers.
// Failures (no uplink, no remote relay, invalid uuid) are silent — local
// OS notification has already fired.
func (a *App) BroadcastCommandFinished(sessionID string, exitCode, elapsedMS int, label string) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	a.mu.Lock()
	u := a.uplink
	a.mu.Unlock()
	if u == nil {
		return
	}
	u.SendCommandEvent(sid, exitCode, elapsedMS, label)
}

// RelayMe is the response body from the relay's /api/me endpoint.
// Email is never logged or persisted (SEC-1).
type RelayMe struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// FetchRelayMe queries the configured relay's /api/me endpoint using the
// stored API token and returns the user's identity. The desktop UI calls
// this after receiving a relay:auth-info event to display the email in the
// status row. Email is held in-memory only and is never written to disk.
func (a *App) FetchRelayMe() (RelayMe, error) {
	if a.cfgStore == nil {
		return RelayMe{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelayToken == "" {
		return RelayMe{}, fmt.Errorf("no relay configured")
	}
	// Convert WS scheme to HTTP so we can use net/http.
	baseHTTP := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	req, err := http.NewRequest("GET", baseHTTP+"/api/me", nil)
	if err != nil {
		return RelayMe{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelayToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RelayMe{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RelayMe{}, fmt.Errorf("relay /api/me returned status %d", resp.StatusCode)
	}
	var out RelayMe
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return RelayMe{}, err
	}
	return out, nil
}
