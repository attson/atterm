package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/desktop/hookinstall"
	"github.com/attson/atterm/internal/connhealth"
	"github.com/attson/atterm/internal/e2eeclient"
	"github.com/attson/atterm/internal/prefssync"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Endpoint is what the frontend uses to open a WebSocket to the in-process
// relay. SessionToken is the bearer token returned by the desktop's local
// bootstrap admin login; it is sent in Authorization headers / WS
// subprotocols the same way it is for any remote relay.
type Endpoint struct {
	URL          string `json:"url"`
	SessionToken string `json:"session_token"`
}

// NewSessionReq is the body of NewSession.
type NewSessionReq struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Cols    uint16   `json:"cols,omitempty"`
	Rows    uint16   `json:"rows,omitempty"`

	// AIKind is set by the frontend after calling its own classifyAIKind()
	// on the user-typed command. Allowed values match the keys of
	// aiSniffers ("claude" | "codex" | "aider"). Empty disables AI behavior
	// (sniffer doesn't start, no resume metadata). Names here are kept in
	// sync with internal/session/ClassifyCommand.
	AIKind string `json:"ai_kind,omitempty"`

	// InitialAISessionID is the AI-side session id we captured before a
	// previous crash. When non-empty, the frontend is responsible for
	// PTY-writing the resume command after first prompt-ready; the Go side
	// just round-trips this value through PaneSnapshot bookkeeping. We do
	// NOT pass it as an arg to the spawned process.
	InitialAISessionID string `json:"initial_ai_session_id,omitempty"`
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
	URL   string `json:"url"`
	Token string `json:"token"`
	// SessionExpiresAt is the Unix-seconds expiry of Token when it was
	// minted as a relay session token (e.g. via /api/pair/consume). 0
	// means "unknown / not a session token" — the frontend then falls
	// back to treating Token as an opaque long-lived credential.
	SessionExpiresAt   int64 `json:"session_expires_at"`
	AllowInsecureRelay bool  `json:"allow_insecure_relay"`
	// DisableE2EE mirrors appConfig.DisableE2EE — see that field for the
	// threat-model write-up. Surfaced through GetRelayConfig /
	// SetRelayDisableE2EE so the Settings UI can flip it and render the
	// titlebar Plaintext chip.
	DisableE2EE      bool   `json:"disable_e2ee"`
	RemotePermission string `json:"remote_permission"`
	// LastEmail is the email cached from the most recent successful
	// LoginRemoteRelay call. Read-only from the frontend: GetRelayConfig
	// populates it; SetRelayConfig ignores it. Only LoginRemoteRelay
	// writes appConfig.RelayLastEmail.
	LastEmail string `json:"last_email"`
	Connected bool   `json:"connected"`
	Paused    bool   `json:"paused"`
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

	// recent relay errors — bounded ring, newest-first.
	relayErrMu  sync.Mutex
	relayErrors []RelayErrorEntry

	// writeFile is os.WriteFile in production; tests substitute a stub.
	writeFile writeFileFunc

	prefsSync *prefssync.Engine

	// accountKey is the user's E2EE account_key (32 bytes) unlocked by
	// the most recent successful LoginRemoteRelay / RegisterRemoteRelay.
	// In-memory only in v1 — lost on app restart, requires re-login. A
	// future milestone will persist it via OS keychain. Protected by
	// accountKeyMu because the uplink reads it while the foreground
	// thread might rewrap during password change.
	accountKeyMu sync.Mutex
	accountKey   []byte

	// eventsEmitter is the Wails EventsEmit function used to push events
	// to the frontend. Defaults to wailsruntime.EventsEmit in NewApp;
	// tests substitute a no-op so they don't crash on wailsruntime's
	// strict context check. Same pattern as uplink.eventsEmit.
	eventsEmitter func(ctx context.Context, name string, data ...interface{})

	// recoveryStore persists tab/pane layout to disk so a relaunch can show
	// the recovery dialog. Wired in startup; nil in tests that don't need it.
	recoveryStore *RecoveryStore

	// lastSnapshot caches the latest payload the frontend pushed via
	// SaveRecoverySnapshot. MarkCleanShutdown re-Saves this exact value with
	// CleanShutdown=true so a clean exit is distinguishable from a crash.
	// Guarded by mu (re-using the existing uplink mutex; both are touched
	// only on relatively cold paths).
	lastSnapshot RecoverySnapshot

	// feishuService is the top-level Feishu integration façade. Nil when
	// feishu startup fails (non-fatal).
	feishuService *feishu.Service
	// feishuMode is "local" or "relay", set at startup alongside feishuService.
	feishuMode string
}

// NewApp creates a new App application struct.
func NewApp(cfgStore *configStore, logger *loggingManager) *App {
	a := &App{
		cfgStore:      cfgStore,
		logger:        logger,
		pluginFS:      NewPluginFS(),
		eventsEmitter: wailsruntime.EventsEmit,
	}
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
	// cfgStore must be ready before startRelayHost — the relay host's
	// bootstrap admin password lives in appConfig, so it has to be loaded
	// (and possibly written on first run) before we open the userstore.
	if a.cfgStore == nil {
		a.cfgStore = loadConfig()
	}
	h, err := startRelayHost(a.cfgStore)
	if err != nil {
		log.Fatalf("desktop: start relay host: %v", err)
	}
	a.host = h

	if rs, err := NewRecoveryStore(a.host.hostID); err == nil {
		a.recoveryStore = rs
	} else {
		log.Printf("recovery store unavailable: %v", err)
	}
	a.host.aiSidCallback = func(localSessionID uuid.UUID, kind, aiSid string) {
		a.eventsEmitter(a.ctx, "recovery:ai-sid", map[string]string{
			"session_id":    localSessionID.String(),
			"kind":          kind,
			"ai_session_id": aiSid,
		})
	}

	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" {
		if env := strings.TrimSpace(os.Getenv("ATTERM_RELAY_URL")); env != "" {
			cfg.RelayURL = env
			cfg.RelaySessionToken = strings.TrimSpace(os.Getenv("ATTERM_RELAY_TOKEN"))
		}
	}
	if a.logger == nil {
		a.logger, err = newDesktopLoggingManager(cfg, Version)
		if err != nil {
			log.Fatalf("desktop: init logging: %v", err)
		}
	}
	// Restore the E2EE account_key from the OS keychain if a previous
	// login persisted one. Failures are logged but never fatal — a
	// missing or corrupted entry just means the user has to log in
	// again. This MUST run before applyRelayConfig so the uplink picks
	// up the unlocked key on the same boot.
	if cfg.RelayURL != "" && cfg.RelaySessionUserID != "" {
		if key, err := loadAccountKey(cfg.RelayURL, cfg.RelaySessionUserID); err != nil {
			log.Printf("desktop: load persisted account_key: %v", err)
		} else if len(key) > 0 {
			a.accountKeyMu.Lock()
			a.accountKey = key
			a.accountKeyMu.Unlock()
			log.Printf("desktop: account_key restored from keychain (user=%s)", cfg.RelaySessionUserID)
		}
	}
	a.applyRelayConfig(cfg)
	// Auto-install ~/.claude/settings.json hook entries + materialize
	// atterm-hook binary, so a fresh install gets Feishu notifications
	// without manual settings.json editing. Failure is non-fatal — the
	// Settings · Feishu panel will surface the LastError.
	if cfg.HookAutoInstallEnabledOrDefault() {
		if err := hookinstall.Install(ctx); err != nil {
			log.Printf("hookinstall: install: %v", err)
		}
	}
	if a.updater != nil {
		a.updater.SetGHProxyURL(cfg.UpdateGHProxyURL)
	}

	adapter := newAppConfigAdapter(a.cfgStore)
	relayClient := newHTTPRelayClient(a.cfgStore)
	a.prefsSync = prefssync.NewEngine(adapter, relayClient)

	// Trigger an initial PULL in the background if already logged in.
	if cfg := a.cfgStore.Get(); cfg.RelaySessionToken != "" {
		go func() {
			if err := a.prefsSync.Pull(a.ctx); err == nil {
				wailsruntime.EventsEmit(a.ctx, "prefs:changed")
			}
		}()
	}

	// Auto-update background loop, gated on the persisted preference.
	// New installs default to enabled (AutoCheckUpdatesOrDefault returns true).
	if a.updater != nil && cfg.AutoCheckUpdatesOrDefault() {
		a.updater.Start(ctx)
	}

	// Feishu integration: choose mode based on relay login state.
	a.startFeishu(ctx, cfg)
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
	if err := feishu.DeleteEndpointFile(); err != nil {
		log.Printf("desktop: delete feishu endpoint file: %v", err)
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
	a.uplink = newUplink(cfg.RelayURL, cfg.RelaySessionToken, cfg.RemotePermissionOrDefault(), a.host, a.recordRelayError, a.agentSealAccountKey, cfg.AllowInsecureRelay)
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

// GetEndpoint returns the local relay endpoint and a session token. The
// frontend uses this to open a WebSocket to the in-process relay; the
// session token is bound to the desktop-local bootstrap admin user.
func (a *App) GetEndpoint() Endpoint {
	if a.host == nil {
		return Endpoint{}
	}
	return Endpoint{URL: "ws://" + a.host.addr, SessionToken: a.host.sessionToken}
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
		Token:              cfg.RelaySessionToken,
		SessionExpiresAt:   cfg.RelaySessionExpiresAt,
		AllowInsecureRelay: cfg.AllowInsecureRelay,
		DisableE2EE:        cfg.DisableE2EE,
		RemotePermission:   cfg.RemotePermissionOrDefault(),
		LastEmail:          cfg.RelayLastEmail,
		Connected:          connected,
		Paused:             cfg.RelayPaused,
	}
}

// SetRelayDisableE2EE flips the per-desktop "stop sealing outbound
// session content" toggle and persists it. The change takes effect on
// the next agent frame — agentSealAccountKey is a closure that reads
// cfg on every call, so no uplink restart is needed.
//
// Emits an "e2ee-mode-changed" Wails event so the frontend (TitleBar
// chip, Settings checkbox) can refresh without polling.
//
// Intended for testing / regression of the unsealed fallback path.
// Off by default; the Settings UI presents this with a ⚠ warning so
// users don't leave it on by mistake.
func (a *App) SetRelayDisableE2EE(disabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.DisableE2EE == disabled {
		return nil
	}
	cfg.DisableE2EE = disabled
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.emitE2EEModeChanged(disabled)
	return nil
}

// emitE2EEModeChanged notifies the frontend of a toggle flip. Split
// into its own method so tests can swap in a capture-style emitter
// via App.eventsEmitter (set in NewApp; see M5-meta-wails wiring).
func (a *App) emitE2EEModeChanged(disabled bool) {
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "e2ee-mode-changed", map[string]any{
		"disabled": disabled,
	})
}

// SetRelayConfig persists a new relay URL/token and (re)starts the uplink. To
// disable the uplink entirely, pass URL="".
func (a *App) SetRelayConfig(req RelayConfig) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	cfg.RelayURL = strings.TrimSpace(req.URL)
	cfg.RelaySessionToken = strings.TrimSpace(req.Token)
	cfg.RelaySessionExpiresAt = req.SessionExpiresAt
	cfg.AllowInsecureRelay = req.AllowInsecureRelay
	priorDisableE2EE := cfg.DisableE2EE
	cfg.DisableE2EE = req.DisableE2EE
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
	if priorDisableE2EE != cfg.DisableE2EE {
		a.emitE2EEModeChanged(cfg.DisableE2EE)
	}
	return nil
}

// LoginRemoteRelay calls POST /api/auth/login on the given relay URL with the
// supplied credentials, parses the returned {session_token, expires_at, user}
// envelope, and persists (relayURL, session_token) to local config via
// SetRelayConfig. Bound to the frontend's "Connect to remote relay" form.
//
// The user-facing input is the HTTP(S) URL of the relay (the same URL their
// browser hits). We POST to that URL directly and normalize the scheme to
// ws:// or wss:// before persistence — the uplink and validateRelayEndpoint
// both expect the WebSocket form. HTTP API calls translate back on the fly
// (see MarkSessionsSeen et al.).
//
// allowInsecure mirrors the "enable insecure mode" toggle on the form. It
// applies to the SetRelayConfig call that persists the new session token so
// the validator sees the user's latest intent, not the previously persisted
// flag — without this, toggling the checkbox in the UI and clicking save
// rejects ws:// targets even though the user just opted in.
func (a *App) LoginRemoteRelay(relayURL, email, password string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, wsURL, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c := &e2eeclient.Client{BaseURL: httpURL, HTTPClient: relayHTTPClient(allowInsecure, 0)}
	res, err := c.Login(ctx, email, password)
	if err != nil {
		return fmt.Errorf("relay OPAQUE login: %w", err)
	}
	a.setAccountKey(res.AccountKey)
	// RemotePermission is preserved from the persisted config (the login form
	// doesn't surface it). AllowInsecureRelay comes from the call argument so
	// the validator inside SetRelayConfig sees the form's current checkbox
	// state rather than the previously persisted flag — necessary for
	// ws:// targets the user is just now opting into. Session expiry is no
	// longer returned by the OPAQUE login response (it lives entirely on the
	// relay side); the frontend will rely on 401-on-expiry instead.
	prev := a.GetRelayConfig()
	if err := a.SetRelayConfig(RelayConfig{
		URL:                wsURL,
		Token:              res.SessionToken,
		AllowInsecureRelay: allowInsecure,
		RemotePermission:   prev.RemotePermission,
	}); err != nil {
		return err
	}
	// Persist the email and user id separately — RelayConfig.LastEmail is
	// read-only from the frontend's perspective (SetRelayConfig intentionally
	// ignores it), so LoginRemoteRelay writes the cfgStore directly.
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.RelayLastEmail = email
		cfg.RelaySessionUserID = res.UserID
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	if a.prefsSync != nil {
		go func() {
			if err := a.prefsSync.Pull(a.ctx); err != nil {
				return
			}
			cfg := a.cfgStore.Get()
			userID := cfg.RelaySessionUserID
			if userID == "" || cfg.PrefsSeedMarkerFor(userID) {
				wailsruntime.EventsEmit(a.ctx, "prefs:changed")
				return
			}
			a.prefsSync.SeedFromLocal(isPrefCustomized(cfg), time.Now().UnixMilli())
			_ = a.prefsSync.Push(a.ctx)

			cfg2 := a.cfgStore.Get()
			if cfg2.PrefsSeedMarkers == nil {
				cfg2.PrefsSeedMarkers = map[string]bool{}
			}
			cfg2.PrefsSeedMarkers[userID] = true
			_ = a.cfgStore.Set(cfg2)
			wailsruntime.EventsEmit(a.ctx, "prefs:changed")
		}()
	}
	return nil
}

// RegisterRemoteRelay creates a fresh OPAQUE-authenticated account on the
// remote relay (POST /api/auth/register/init + /finalize via SDK), mints a
// session token, persists URL+token+email locally, and stores the freshly
// generated account_key in memory. claimToken is optional — supply the
// plaintext token printed by `atterm-relay` bootstrap to also promote the
// new user to admin.
//
// On success behaves identically to LoginRemoteRelay (same SetRelayConfig
// + prefsSync seed path); on failure the call returns the underlying
// SDK error verbatim so the frontend can surface a meaningful message.
func (a *App) RegisterRemoteRelay(relayURL, email, password, claimToken string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, wsURL, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c := &e2eeclient.Client{BaseURL: httpURL, HTTPClient: relayHTTPClient(allowInsecure, 0)}
	res, err := c.Register(ctx, email, password, claimToken)
	if err != nil {
		return fmt.Errorf("relay OPAQUE register: %w", err)
	}
	a.setAccountKey(res.AccountKey)
	prev := a.GetRelayConfig()
	if err := a.SetRelayConfig(RelayConfig{
		URL:                wsURL,
		Token:              res.SessionToken,
		AllowInsecureRelay: allowInsecure,
		RemotePermission:   prev.RemotePermission,
	}); err != nil {
		return err
	}
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.RelayLastEmail = email
		cfg.RelaySessionUserID = res.UserID
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	return nil
}

// setAccountKey stores key as the current in-memory account_key. Concurrent
// callers see the most recent successful value via accountKeySnapshot.
func (a *App) setAccountKey(key []byte) {
	a.accountKeyMu.Lock()
	if len(key) == 0 {
		a.accountKey = nil
	} else {
		a.accountKey = append([]byte(nil), key...)
	}
	a.accountKeyMu.Unlock()
	a.persistAccountKey(key)
	a.emitAccountKeyChanged()
}

// emitAccountKeyChanged notifies the frontend so the platform-layer
// cache (wails.ts setAccountKeyProvider) refreshes. Routed through
// the injectable a.eventsEmitter so unit tests that wire a plain
// context.Background() do not crash on wailsruntime's strict context
// check.
func (a *App) emitAccountKeyChanged() {
	if a.ctx == nil {
		return
	}
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "account-key:changed")
}

// persistAccountKey writes (or clears) the account_key for the currently
// configured (relay URL, user ID). Failures are logged but never
// returned — losing the persistence is a UX regression (requires
// re-login on app restart) but not a correctness one (in-memory key is
// still set / cleared). Callers MUST hold accountKeyMu released so the
// keychain syscall doesn't lengthen the critical section.
func (a *App) persistAccountKey(key []byte) {
	if a.cfgStore == nil {
		return
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionUserID == "" {
		return
	}
	if err := saveAccountKey(cfg.RelayURL, cfg.RelaySessionUserID, key); err != nil {
		log.Printf("desktop: persist account_key failed: %v", err)
	}
}

// accountKeySnapshot returns a defensive copy of the current account_key
// (or nil if unlocked). The uplink consumes this to derive per-session
// frame keys once frame-level encryption ships in M2.
func (a *App) accountKeySnapshot() []byte {
	a.accountKeyMu.Lock()
	defer a.accountKeyMu.Unlock()
	if len(a.accountKey) == 0 {
		return nil
	}
	return append([]byte(nil), a.accountKey...)
}

// agentSealAccountKey is the closure handed to newUplink. It returns
// the live account_key for seal operations EXCEPT when the user has
// flipped the per-desktop DisableE2EE toggle — in that case it returns
// nil, which is the existing "no key = no encryption" code path. Every
// seal site in uplink.go / uplink_seal_*.go falls through to plaintext
// automatically without any additional branching. Hot-toggleable: the
// closure consults the latest cfg on every call, so flipping the flag
// in Settings takes effect on the next frame without restart.
//
// Distinct from accountKeySnapshot, which is also used by the JS-side
// GetAccountKey binding for decrypting frames originating from OTHER
// desktops. The toggle only suppresses THIS desktop's sealing; cross-
// desktop decrypt keeps working so a paused-encryption desktop still
// reads its other devices' sealed sessions correctly.
func (a *App) agentSealAccountKey() []byte {
	if a.cfgStore != nil && a.cfgStore.Get().DisableE2EE {
		return nil
	}
	return a.accountKeySnapshot()
}

// HasAccountKey reports whether an account_key is currently unlocked in
// memory. The frontend uses this to decide whether to surface a "unlock"
// prompt vs assume the user just needs to re-authenticate.
func (a *App) HasAccountKey() bool {
	return len(a.accountKeySnapshot()) > 0
}

// GetAccountKey returns the unlocked E2EE account_key as a standard
// base64 string, or the empty string when no key is available (user
// not logged in, bootstrap-admin path, etc.).
//
// Threat model: this binding sits entirely inside the desktop's own
// process boundary — the JS side runs in the same OS user's Wails
// host. Exposing the key to JS lets the connection layer decrypt
// MetaPayload.Sealed / SessionInfo.Sealed in the WebSocket hot path
// without an async round-trip per frame. The same key would have
// been derivable by anything running in this process anyway (via
// Wails' own ipc binding mechanism), so the surface area does not
// change.
//
// Note: do NOT log the return value. Do NOT persist it. The Go side
// already has a Keychain copy under M1f; this binding is for the JS
// runtime to cache once at platform-init and discard on logout.
func (a *App) GetAccountKey() string {
	key := a.accountKeySnapshot()
	if len(key) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(key)
}

// ProbeRelayVersion does a lightweight GET <relayURL>/api/version to verify
// the URL points at an atterm relay. Returns nil if the response is 200 and
// the JSON body has a non-empty "version" field. Otherwise returns an error
// the frontend surfaces as "无法连接到 relay" inline beneath the URL field.
//
// /api/version is auth-less per the session-token spec, so no credentials
// are sent. 5-second timeout keeps the UI from blocking on a stalled
// connection — the user can re-click "保存并连接" if the relay just woke up.
func (a *App) ProbeRelayVersion(relayURL string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, _, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay url: %w", err)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/api/version", nil)
	if err != nil {
		return err
	}
	client := relayHTTPClient(allowInsecure, 5*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("not an atterm relay (decode): %w", err)
	}
	if out.Version == "" {
		return fmt.Errorf("not an atterm relay (no version field)")
	}
	return nil
}

// relayLoginEndpoints normalizes a user-entered relay URL into the (http(s),
// ws(s)) pair we need. Accepts http://, https://, ws://, wss:// — anything
// else is rejected so the caller sees a clear error before the POST.
func relayLoginEndpoints(raw string) (httpURL, wsURL string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil || u.Host == "" {
		return "", "", fmt.Errorf("invalid relay url %q", raw)
	}
	switch u.Scheme {
	case "http", "ws":
		httpURL = "http://" + u.Host + u.Path
		wsURL = "ws://" + u.Host + u.Path
	case "https", "wss":
		httpURL = "https://" + u.Host + u.Path
		wsURL = "wss://" + u.Host + u.Path
	default:
		return "", "", fmt.Errorf("relay url scheme must be http(s) or ws(s), got %q", u.Scheme)
	}
	return strings.TrimRight(httpURL, "/"), strings.TrimRight(wsURL, "/"), nil
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

// GetUplinkHealth returns a snapshot of the desktop uplink's connection
// health (RTT, reconnect history, byte rates, seq gaps). Surfaced to the
// frontend ConnHealthPill / ConnHealthDrawer. Returns a "closed" snapshot
// when no uplink exists (i.e. relay not configured or uplink paused).
func (a *App) GetUplinkHealth() connhealth.Snapshot {
	if a.uplink == nil {
		return connhealth.Snapshot{
			State:      connhealth.StateClosed,
			RTTSamples: []connhealth.RTTSample{},
			Reconnect:  connhealth.ReconnectStats{History: []connhealth.ReconnectEvent{}},
		}
	}
	return a.uplink.Health()
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

// GetLocalePreference returns the user's persisted UI language preference.
func (a *App) GetLocalePreference() string {
	if a == nil || a.cfgStore == nil {
		return localePreferenceSystem
	}
	return a.cfgStore.Get().LocalePreferenceOrDefault()
}

// SetLocalePreference persists the user's UI language preference.
func (a *App) SetLocalePreference(preference string) error {
	if a == nil || a.cfgStore == nil {
		return errors.New("app not initialized")
	}
	cfg := a.cfgStore.Get()
	switch preference {
	case localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified:
		cfg.LocalePreference = preference
	default:
		return errors.New("unsupported locale preference")
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("locale_preference")
	return nil
}

func (a *App) GetDefaultShell() string {
	if a.cfgStore == nil {
		return defaultShellAuto
	}
	return a.cfgStore.Get().DefaultShellOrDefault()
}

func (a *App) SetDefaultShell(shell string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	shell = strings.TrimSpace(shell)
	if shell == "" || strings.EqualFold(shell, defaultShellAuto) {
		shell = defaultShellAuto
	} else if _, err := exec.LookPath(shell); err != nil {
		return fmt.Errorf("default shell not found: %s", shell)
	}
	cfg := a.cfgStore.Get()
	cfg.DefaultShell = shell
	return a.cfgStore.Set(cfg)
}

// GetTaskPreset returns the user's persisted task state display preset.
func (a *App) GetTaskPreset() string {
	if a.cfgStore == nil {
		return taskPresetDefault
	}
	return a.cfgStore.Get().TaskPresetOrDefault()
}

// SetTaskPreset persists the user's task state display preset.
func (a *App) SetTaskPreset(preset string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	preset = strings.TrimSpace(preset)
	if !isSupportedTaskPreset(preset) {
		return fmt.Errorf("unknown task preset %q", preset)
	}
	cfg := a.cfgStore.Get()
	cfg.TaskPreset = preset
	return a.cfgStore.Set(cfg)
}

// GetTaskGroupBy returns "host" or "state". Default "host".
func (a *App) GetTaskGroupBy() string {
	if a.cfgStore == nil {
		return taskGroupByDefault
	}
	return a.cfgStore.Get().TaskGroupByOrDefault()
}

// SetTaskGroupBy persists the user's session-bar grouping choice. Rejects
// unknown values so a typo can't leave the sidebar without a renderer.
func (a *App) SetTaskGroupBy(groupBy string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	groupBy = strings.TrimSpace(groupBy)
	if !isSupportedTaskGroupBy(groupBy) {
		return fmt.Errorf("unknown task group-by %q", groupBy)
	}
	cfg := a.cfgStore.Get()
	cfg.TaskGroupBy = groupBy
	return a.cfgStore.Set(cfg)
}

// GetUserHomeDir returns the OS-reported user home directory, so the
// frontend can collapse paths starting with HOME to "~" for display.
// Returns empty string on failure (the frontend already treats empty
// HOME as "don't substitute").
func (a *App) GetUserHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// GetTaskSidebarCollapsed returns whether the task sidebar panel is collapsed.
func (a *App) GetTaskSidebarCollapsed() bool {
	if a.cfgStore == nil {
		return false
	}
	return a.cfgStore.Get().TaskSidebarCollapsed
}

// SetTaskSidebarCollapsed persists the task sidebar collapsed state.
func (a *App) SetTaskSidebarCollapsed(collapsed bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.TaskSidebarCollapsed = collapsed
	return a.cfgStore.Set(cfg)
}

const (
	minTaskSidebarWidth = 180
	maxTaskSidebarWidth = 480
)

// GetTaskSidebarWidth returns the persisted task sidebar pixel width,
// clamped to a sane range. Returns the default (240) when no value is
// stored or when the stored value is out of range.
func (a *App) GetTaskSidebarWidth() int {
	if a.cfgStore == nil {
		return defaultTaskSidebarWidth
	}
	w := a.cfgStore.Get().TaskSidebarWidthOrDefault()
	if w < minTaskSidebarWidth || w > maxTaskSidebarWidth {
		return defaultTaskSidebarWidth
	}
	return w
}

// SetTaskSidebarWidth clamps px to [minTaskSidebarWidth, maxTaskSidebarWidth]
// and persists. Out-of-range values are silently corrected so a frontend
// bug that emits 1500 still leaves the user in a usable state.
func (a *App) SetTaskSidebarWidth(px int) error {
	if a.cfgStore == nil {
		return nil
	}
	if px < minTaskSidebarWidth {
		px = minTaskSidebarWidth
	}
	if px > maxTaskSidebarWidth {
		px = maxTaskSidebarWidth
	}
	cfg := a.cfgStore.Get()
	cfg.TaskSidebarWidth = px
	return a.cfgStore.Set(cfg)
}

// MarkSessionsSeen marks sessions as seen on the relay. If all is true, all
// sessions are marked seen regardless of the ids slice.
func (a *App) MarkSessionsSeen(ids []string, all bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return fmt.Errorf("no relay configured")
	}
	baseHTTP := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	body, err := json.Marshal(map[string]any{
		"session_ids": ids,
		"all":         all,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", baseHTTP+"/api/sessions/seen", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("relay /api/sessions/seen returned status %d", resp.StatusCode)
	}
	return nil
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
		candidates = windowsShellCandidates()
	}
	var out []string
	seen := map[string]bool{}
	addShell := func(shell string) {
		if shell == "" {
			return
		}
		path, err := exec.LookPath(shell)
		if err != nil {
			return
		}
		key := strings.ToLower(filepath.Base(path))
		if seen[key] {
			return
		}
		out = append(out, path)
		seen[key] = true
	}
	if a.cfgStore != nil {
		if configured := a.cfgStore.Get().DefaultShellOrDefault(); configured != defaultShellAuto {
			addShell(configured)
		}
	}
	if runtime.GOOS != "windows" {
		envShell := os.Getenv("SHELL")
		addShell(envShell)
	}
	for _, c := range candidates {
		addShell(c)
	}
	return out
}

func windowsShellCandidates() []string {
	candidates := []string{
		"powershell.exe",
		"cmd.exe",
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		candidates = append(candidates,
			filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
			filepath.Join(root, "System32", "cmd.exe"),
		)
	}
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		candidates = append(candidates, comspec)
	}
	return candidates
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

// GetUpdateGHProxyURL reports the optional GitHub release download proxy.
func (a *App) GetUpdateGHProxyURL() string {
	if a.cfgStore == nil {
		return ""
	}
	return a.cfgStore.Get().UpdateGHProxyURL
}

// SetUpdateGHProxyURL persists the optional GitHub release download proxy.
// Empty disables proxying. Non-empty values must be absolute http(s) URLs.
func (a *App) SetUpdateGHProxyURL(proxyURL string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	normalized, err := normalizeUpdateGHProxyURL(proxyURL)
	if err != nil {
		return err
	}
	cfg := a.cfgStore.Get()
	cfg.UpdateGHProxyURL = normalized
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if a.updater != nil {
		a.updater.SetGHProxyURL(normalized)
	}
	return nil
}

func normalizeUpdateGHProxyURL(proxyURL string) (string, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return "", nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("GitHub proxy URL must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("GitHub proxy URL must use http or https")
	}
	return proxyURL, nil
}

// beforeClose is wired to wails options.OnBeforeClose. If a previous
// ConfirmQuit() call set quitApproved, the close proceeds. Otherwise it
// emits a before-close event to the frontend and tells Wails to abort
// this close. The emit function is a parameter so tests can verify the
// gating without bringing up a Wails runtime.
func (a *App) beforeClose(ctx context.Context, emit func()) bool {
	if a.quitApproved.Load() {
		// Last chance to mark the recovery snapshot as a clean exit so
		// the next launch shows "last clean exit" instead of "ended
		// unexpectedly". Best-effort: a failure here doesn't block the
		// close — the user already approved.
		if err := a.MarkCleanShutdown(); err != nil {
			log.Printf("recovery: MarkCleanShutdown on close: %v", err)
		}
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

// GetWebglRendererEnabled returns the user's preference for the xterm WebGL
// renderer. Linux defaults to false (NVIDIA + X11 + WebKitGTK paints the
// cursor / last cell on a delayed schedule that surfaces as typing lag —
// #48); macOS/Windows default to true so the #33 ghosting fix stays active.
func (a *App) GetWebglRendererEnabled() bool {
	if a.cfgStore == nil {
		return defaultWebglRendererEnabledFor(runtime.GOOS)
	}
	return a.cfgStore.Get().WebglRendererEnabledOrDefault()
}

// SetWebglRendererEnabled persists the user's WebGL toggle. The setting takes
// effect for new terminal sessions; already-open xterms keep their current
// renderer until they are recreated.
func (a *App) SetWebglRendererEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.WebglRendererEnabled = &enabled
	return a.cfgStore.Set(cfg)
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
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("notifications_enabled")
	return nil
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
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("shell_integration_enabled")
	return nil
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
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("command_notify_threshold_seconds")
	return nil
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
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return RelayMe{}, fmt.Errorf("no relay configured")
	}
	// Convert WS scheme to HTTP so we can use net/http.
	baseHTTP := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	req, err := http.NewRequest("GET", baseHTTP+"/api/me", nil)
	if err != nil {
		return RelayMe{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 0).Do(req)
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

// PairingTokenResponse is what the renderer receives when generating a QR code.
// Mirrors the relay's /api/pair/create response body.
type PairingTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	QRURL     string `json:"qr_url"`
}

// CreatePairingToken asks the configured relay to mint a 5-minute single-use
// pairing token for the desktop's current user and returns the response,
// including the qr_url to encode into a QR code.
func (a *App) CreatePairingToken() (PairingTokenResponse, error) {
	if a.cfgStore == nil {
		return PairingTokenResponse{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return PairingTokenResponse{}, fmt.Errorf("no relay configured")
	}
	baseHTTP := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	req, err := http.NewRequest("POST", baseHTTP+"/api/pair/create", strings.NewReader("{}"))
	if err != nil {
		return PairingTokenResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 0).Do(req)
	if err != nil {
		return PairingTokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PairingTokenResponse{}, fmt.Errorf("relay /api/pair/create returned status %d", resp.StatusCode)
	}
	var out PairingTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PairingTokenResponse{}, err
	}
	return out, nil
}

// recordRelayError appends an error entry to the recent-errors ring buffer.
// Nil errors are dropped. Messages are passed through redactErrorLine so
// tokens / Authorization / Cookie values are masked. Newest-first ordering;
// when the buffer is full the oldest entry falls off.
func (a *App) recordRelayError(err error) {
	if err == nil {
		return
	}
	entry := RelayErrorEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Message:   redactErrorLine(err.Error()),
	}
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	a.relayErrors = append([]RelayErrorEntry{entry}, a.relayErrors...)
	if len(a.relayErrors) > maxRelayErrors {
		a.relayErrors = a.relayErrors[:maxRelayErrors]
	}
}

// markPrefDirtyAndPush stamps the meta for key with the current ms,
// then triggers a background PUSH. Errors are swallowed by design (sync
// is best-effort; user UI already reflects the change).
func (a *App) markPrefDirtyAndPush(key string) {
	if a.prefsSync == nil {
		return
	}
	a.prefsSync.MarkDirty(key, time.Now().UnixMilli())
	go func() {
		if err := a.prefsSync.Push(a.ctx); err == nil {
			wailsruntime.EventsEmit(a.ctx, "prefs:changed")
		}
	}()
}

// snapshotRelayErrors returns a copy of the recent-errors ring buffer.
// Callers receive a fresh slice safe to mutate; the underlying buffer is
// unaffected.
func (a *App) snapshotRelayErrors() []RelayErrorEntry {
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	out := make([]RelayErrorEntry, len(a.relayErrors))
	copy(out, a.relayErrors)
	return out
}

// writeFileFunc is the function `ExportDiagnostics` uses to persist content.
// Held as a field on App so tests can substitute a capturing stub instead of
// touching disk. Defaults to os.WriteFile in production.
type writeFileFunc func(path string, data []byte, perm fs.FileMode) error

// GetDiagnostics is the Wails-exposed binding that returns the current
// diagnostics payload. userAgent should be the renderer's navigator.userAgent.
func (a *App) GetDiagnostics(userAgent string) DiagnosticsPayload {
	return collectDiagnostics(a, userAgent)
}

// ExportDiagnostics opens a native save dialog (default filename
// "atterm-diagnostics-<ts>.txt") and writes content to the chosen path.
// Returns "" when the user cancelled. Returns ("", err) only on actual
// I/O failure after the user picked a path.
func (a *App) ExportDiagnostics(content string) (string, error) {
	defaultName := "atterm-diagnostics-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ".txt"
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export diagnostics",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Text Files (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	wf := a.writeFile
	if wf == nil {
		wf = os.WriteFile
	}
	if err := wf(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// isPrefCustomized returns true when the given synced key's value in
// the loaded config differs from the desktop's hard-coded default.
func isPrefCustomized(c appConfig) func(string) bool {
	return func(key string) bool {
		switch key {
		case "locale_preference":
			return c.LocalePreference != "" && c.LocalePreference != localePreferenceSystem
		case "quick_templates":
			return len(c.QuickTemplates) > 0
		case "notifications_enabled":
			return c.NotificationsEnabled != nil
		case "command_notify_threshold_seconds":
			return c.CommandNotifyThresholdSeconds != nil
		case "shell_integration_enabled":
			return c.ShellIntegrationEnabled != nil
		}
		return false
	}
}

// LoadRecoverySnapshot returns the most recent snapshot, or a zero value
// when there's nothing to recover. Side effect: rewrites the on-disk file
// with CleanShutdown=false so a crash during the recovery dialog is caught
// next launch.
//
// All return paths normalize Tabs to a non-nil slice — Go's `nil` slice
// marshals to JSON `null`, and the frontend's check
// `recoverySnap.tabs.length > 0` throws on `null` (uncaught, because the
// check sits outside the boot try/catch). Keeping Tabs as `[]` here makes
// that wire shape impossible to regress.
func (a *App) LoadRecoverySnapshot() (RecoverySnapshot, error) {
	if a.recoveryStore == nil {
		return RecoverySnapshot{Tabs: []TabSnapshot{}}, nil
	}
	snap, err := a.recoveryStore.Load()
	if err != nil {
		return RecoverySnapshot{Tabs: []TabSnapshot{}}, err
	}
	if snap.Tabs == nil {
		snap.Tabs = []TabSnapshot{}
	}
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	return snap, nil
}

// SaveRecoverySnapshot accepts a JSON-encoded RecoverySnapshot from the
// frontend (debounce-driven). Validates by unmarshalling into the typed
// struct so malformed payloads fail loudly. Server-side overrides
// Version/HostID/SavedAtUnix so the frontend can't lie about them.
func (a *App) SaveRecoverySnapshot(payload string) error {
	if a.recoveryStore == nil {
		return nil
	}
	var snap RecoverySnapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return fmt.Errorf("decode recovery snapshot: %w", err)
	}
	snap.Version = recoverySnapshotVersion
	snap.HostID = a.host.hostID
	snap.SavedAtUnix = a.recoveryStore.nowUnix()
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	return a.recoveryStore.Save(snap)
}

// DiscardRecoverySnapshot removes recovery.json. Used by the dialog's
// "discard" / close-X paths.
func (a *App) DiscardRecoverySnapshot() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	a.lastSnapshot = RecoverySnapshot{}
	a.mu.Unlock()
	return a.recoveryStore.Discard()
}

// MarkCleanShutdown is called from OnBeforeClose right before the wails
// runtime tears the window down. It rewrites the latest snapshot with
// CleanShutdown=true so the next launch's dialog can render "last clean
// exit" copy. No-op when nothing has been saved this session.
func (a *App) MarkCleanShutdown() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	snap := a.lastSnapshot
	a.mu.Unlock()
	return a.recoveryStore.MarkCleanShutdown(snap)
}

// GetRecoveryDialogEnabled mirrors appConfig.RecoveryDialogEnabledOrDefault
// for the frontend Settings → General toggle.
func (a *App) GetRecoveryDialogEnabled() bool {
	return a.cfgStore.Get().RecoveryDialogEnabledOrDefault()
}

// SetRecoveryDialogEnabled persists the user's choice. true re-enables the
// startup recovery dialog; false skips it.
func (a *App) SetRecoveryDialogEnabled(enabled bool) error {
	cfg := a.cfgStore.Get()
	cfg.RecoveryDialogEnabled = &enabled
	return a.cfgStore.Set(cfg)
}

// startFeishu constructs feishu.Service, starts the HookServer, writes the
// endpoint file, and wires up the relayHost. Called once from startup().
func (a *App) startFeishu(ctx context.Context, cfg appConfig) {
	isRelayMode := cfg.RelayURL != "" && cfg.RelaySessionToken != ""

	var svcCfg feishu.ServiceConfig
	if isRelayMode {
		a.feishuMode = "relay"
		relayURL := cfg.RelayURL
		// Capture token func at startup; reads cfgStore on each call so it
		// stays current after token refresh.
		svcCfg = feishu.ServiceConfig{
			Mode:     feishu.ModeRelay,
			RelayURL: relayURL,
			RelayToken: func() string {
				if a.cfgStore == nil {
					return ""
				}
				return a.cfgStore.Get().RelaySessionToken
			},
			Sessions: a.host,
		}
	} else {
		a.feishuMode = "local"
		svcCfg = feishu.ServiceConfig{
			Mode:     feishu.ModeLocal,
			Sessions: a.host,
		}
	}

	svc, err := feishu.NewService(svcCfg)
	if err != nil {
		log.Printf("desktop: feishu service init: %v", err)
		return
	}

	addr, _, err := svc.HookServer().Start()
	if err != nil {
		log.Printf("desktop: feishu hook server start: %v", err)
		return
	}
	hookEndpoint := "http://" + addr + "/atterm-hook/notify"

	if err := feishu.WriteEndpointFile(hookEndpoint); err != nil {
		log.Printf("desktop: write feishu endpoint file: %v", err)
	}

	svc.HookServer().SetSuspectCallback(func() {
		// A misrouted POST may indicate stale install; nudge the
		// debounced auto-repair on next UI poll.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
	})

	if a.host != nil {
		a.host.FeishuHookEndpoint = hookEndpoint
		a.host.FeishuDispatcher = svc.Dispatcher()
	}

	if !isRelayMode {
		if err := svc.EnsureLongConn(ctx); err != nil {
			// Not fatal — credentials may not be set yet.
			log.Printf("desktop: feishu long-conn: %v", err)
		}
	}

	a.feishuService = svc
	log.Printf("desktop: feishu service started (mode=%s endpoint=%s)", a.feishuMode, hookEndpoint)
}

// FeishuStatusResp is returned by GetFeishuStatus.
type FeishuStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`
	Bound    bool   `json:"bound"`
	OpenID   string `json:"open_id"`
	Disabled bool   `json:"disabled"`
}

// GetFeishuStatus returns the current Feishu integration state.
//
// Wails-bound methods must not declare context.Context in their signature —
// the generator surfaces it as a required JS argument, so the frontend call
// fails with "received 0 arguments, expected 1". Internal callers (long-conn,
// store, dispatcher) get the lifecycle context via a.ctx.
func (a *App) GetFeishuStatus() (FeishuStatusResp, error) {
	if a.feishuService == nil {
		return FeishuStatusResp{Enabled: false}, nil
	}
	v, err := a.feishuService.Store().Get(a.ctx)
	if errors.Is(err, feishu.ErrLocalBindingNotFound) {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    a.feishuMode,
			Bound:   false,
		}, nil
	}
	if err != nil {
		return FeishuStatusResp{}, err
	}
	return FeishuStatusResp{
		Enabled:  true,
		Mode:     a.feishuMode,
		Bound:    v.OpenID != "",
		OpenID:   v.OpenID,
		Disabled: v.DisabledAt != 0,
	}, nil
}

// SetFeishuCredentials saves app credentials and (re)starts the long-conn.
func (a *App) SetFeishuCredentials(c feishu.Credentials) error {
	if a.feishuService == nil {
		return errors.New("feishu disabled")
	}
	if err := a.feishuService.Store().SetCredentials(a.ctx, c); err != nil {
		return err
	}
	return a.feishuService.EnsureLongConn(a.ctx)
}

// BeginFeishuPair issues a short-code that the user sends to the bot via
// private chat to complete the bind flow. In relay mode the code is issued by
// the relay; in local mode it is generated in-process.
func (a *App) BeginFeishuPair() (string, error) {
	if a.feishuService == nil {
		return "", errors.New("feishu disabled")
	}
	return a.feishuService.BeginPair(a.ctx)
}

// DeleteFeishuBinding removes the bound OpenID from the store.
func (a *App) DeleteFeishuBinding() error {
	if a.feishuService == nil {
		return errors.New("feishu disabled")
	}
	return a.feishuService.Store().Delete(a.ctx)
}

// hookInstallLastAttempt tracks when we last auto-repaired so the UI
// poll doesn't trigger a Check→Install loop while the underlying issue
// is permanent (e.g. read-only mount).
var (
	hookInstallLastAttempt   time.Time
	hookInstallLastAttemptMu sync.Mutex
)

const hookInstallRepairDebounce = 5 * time.Second

// GetHookInstallState returns the current health snapshot. When the
// surface is unhealthy and we haven't tried in the last 5 seconds,
// we kick a silent Install before returning the post-repair state.
func (a *App) GetHookInstallState() hookinstall.State {
	enabled := true
	if a.cfgStore != nil {
		enabled = a.cfgStore.Get().HookAutoInstallEnabledOrDefault()
	}
	s := hookinstall.Check(a.ctx, enabled)
	if !s.Healthy() && enabled && allowHookInstallRepair() {
		if err := hookinstall.Install(a.ctx); err != nil {
			log.Printf("hookinstall: auto-repair: %v", err)
		}
		s = hookinstall.Check(a.ctx, enabled)
	}
	return s
}

// SetHookInstallEnabled persists the toggle and either installs or
// uninstalls. Errors are returned to the frontend so the Retry button
// can surface them.
func (a *App) SetHookInstallEnabled(on bool) error {
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.HookAutoInstallEnabled = &on
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	if on {
		// Reset debounce so a manual toggle ALWAYS retries.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
		return hookinstall.Install(a.ctx)
	}
	return hookinstall.Uninstall(a.ctx)
}

func allowHookInstallRepair() bool {
	hookInstallLastAttemptMu.Lock()
	defer hookInstallLastAttemptMu.Unlock()
	if time.Since(hookInstallLastAttempt) < hookInstallRepairDebounce {
		return false
	}
	hookInstallLastAttempt = time.Now()
	return true
}
