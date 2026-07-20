package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/connhealth"
	"github.com/attson/atterm/internal/e2eeclient"
	internalfeishu "github.com/attson/atterm/internal/feishu"
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
	// previous crash. When non-empty (with AIKind), the Go side injects the
	// resume command (e.g. `claude --resume <id>`) straight into the PTY on the
	// restored shell's first OSC 133 prompt — see relay_host SetOnFirstPrompt.
	// It is NOT passed as an arg to the spawned shell.
	InitialAISessionID string `json:"initial_ai_session_id,omitempty"`

	// InitialAICommandLine is the original full command line the AI CLI was
	// launched with, round-tripped from the previous run's snapshot
	// (last_command_line). For claude its launch flags (e.g.
	// --permission-mode bypassPermissions, --model) are merged into the injected
	// `claude --resume <id>` so the recovered session keeps the user's original
	// settings — see computeResumeArgs / claudePreservedFlags. It is NOT passed
	// as an arg to the spawned shell.
	InitialAICommandLine string `json:"initial_ai_command_line,omitempty"`
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
	// RemoteProxyURL is the loopback ws:// base the frontend attaches remote
	// sessions through (see remoteProxy). Read-only; empty if the proxy is
	// unavailable. The WebView can't TLS-dial the relay directly on some
	// networks, so remote /client attaches tunnel through Go via this URL.
	RemoteProxyURL string `json:"remote_proxy_url"`
	Paused         bool   `json:"paused"`
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
	ctx         context.Context
	host        *relayHost
	remoteProxy *remoteProxy
	cfgStore    *configStore
	logger      *loggingManager

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

	// feishuMu guards feishuService / feishuMode, which are read from Wails
	// methods and rebuilt by reconcileFeishuMode when the relay login state
	// changes at runtime.
	feishuMu sync.RWMutex
	// feishuService is the top-level Feishu integration façade. Nil when
	// feishu startup fails (non-fatal).
	feishuService *feishu.Service
	// feishuMode is "local" or "relay".
	feishuMode string
	// feishuHookSrv is the long-lived hook listener, started once and kept
	// across mode switches so the ATTERM_HOOK_ENDPOINT baked into open PTYs
	// stays valid; only its dispatcher is swapped on rebuild.
	feishuHookSrv *feishu.HookServer
	// feishuHookEndpoint is the stable hook URL written to the endpoint file.
	feishuHookEndpoint string
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

	// Loopback proxy for remote-session attaches: the WebView can't open a TLS
	// WebSocket to the relay on networks that fingerprint-filter its handshake,
	// so the frontend tunnels /client through Go (whose TLS passes). Non-fatal:
	// remote viewing still works via ListRemoteSessions if this fails to bind.
	if rp, err := startRemoteProxy(a.cfgStore); err != nil {
		log.Printf("desktop: start remote proxy: %v", err)
	} else {
		a.remoteProxy = rp
	}

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
		if key, err := loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID); err != nil {
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
	if a.remoteProxy != nil {
		a.remoteProxy.Stop()
		a.remoteProxy = nil
	}
	if err := feishu.DeleteEndpointFile(); err != nil {
		log.Printf("desktop: delete feishu endpoint file: %v", err)
	}
}

// applyRelayConfig reconciles everything that depends on the relay login state:
// the uplink and the Feishu integration mode. Caller need not hold a.mu.
func (a *App) applyRelayConfig(cfg appConfig) {
	a.applyRelayUplink(cfg)
	// Feishu mode follows the relay login state: relay when logged in, local
	// otherwise. Done outside a.mu (reconcile uses its own lock and may touch
	// the long-conn). No-op until startFeishu has run.
	a.reconcileFeishuMode(a.ctx, cfg)
}

// applyRelayUplink (re)starts the uplink to match the given config. URL == ""
// means "no uplink" — any running one is cancelled.
func (a *App) applyRelayUplink(cfg appConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.uplinkCancel != nil {
		a.uplinkCancel()
		a.uplinkCancel = nil
		a.uplink = nil
	}
	if cfg.RelayURL == "" || cfg.RelayPaused || cfg.RelaySessionToken == "" {
		reason := "no URL"
		if cfg.RelayPaused {
			reason = "paused by user"
		} else if cfg.RelayURL != "" && cfg.RelaySessionToken == "" {
			// A URL persisted without a token is a remembered draft (e.g. the
			// user saved settings after a failed login). There is nothing to
			// authenticate with, so don't spin a doomed reconnect loop.
			reason = "no session token"
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
	dialURL := uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	a.uplink = newUplink(dialURL, cfg.RelaySessionToken, cfg.RemotePermissionOrDefault(), a.host, a.recordRelayError, a.agentSealAccountKey, cfg.AllowInsecureRelay)
	go a.uplink.Run(uplinkCtx)
	log.Printf("desktop: uplink configured for %s", dialURL)
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
		RemoteProxyURL:     a.remoteProxy.wsURL(),
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
	// Persist the email when provided so a failed-login "remember my inputs"
	// save keeps it (LoginRemoteRelay still writes it on success). Empty leaves
	// the cached value untouched.
	if req.LastEmail != "" {
		cfg.RelayLastEmail = req.LastEmail
	}
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

// ClearRelayConfig removes every persisted relay identifier from this
// desktop: the 9 Relay* fields on appConfig, the OS-keychain password slot
// (origin+email), and the OS-keychain account_key slot (origin+userID).
// The in-memory account_key is zeroed too, so this desktop stops sealing /
// decrypting frames for the just-forgotten identity. The uplink is stopped
// as part of applyRelayConfig (empty URL takes the "no uplink" branch).
//
// Local terminal sessions, pairing peer records, and every non-relay
// setting are left untouched.
func (a *App) ClearRelayConfig() error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	oldURL := cfg.RelayURL
	oldEmail := cfg.RelayLastEmail

	// Clear the E2EE account_key BEFORE zeroing cfg. setAccountKey(nil)
	// routes through persistAccountKey which reads cfg.RelayURL /
	// cfg.RelaySessionUserID; if we cleared cfg first, persistAccountKey
	// early-returns on the empty URL and the keychain slot is orphaned.
	a.setAccountKey(nil)

	cfg.RelayURL = ""
	cfg.RelaySessionToken = ""
	cfg.RelaySessionExpiresAt = 0
	cfg.RelayLastEmail = ""
	cfg.RelaySessionUserID = ""
	cfg.AllowInsecureRelay = false
	cfg.DisableE2EE = false
	cfg.RemotePermission = ""
	cfg.RelayPaused = false

	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}

	// Best-effort keychain delete for the password slot. clearRelayPasswordFor
	// swallows ErrNotFound; other errors are logged and swallowed because the
	// persisted config is already gone — "cleared with a stray keychain
	// entry" is strictly better than "aborted midway".
	if err := clearRelayPasswordFor(oldURL, oldEmail); err != nil {
		log.Printf("desktop: clear relay password keychain slot: %v", err)
	}

	a.applyRelayConfig(cfg)

	// emitE2EEModeChanged pushes e2ee-mode-changed unconditionally; the
	// existing helper does not skip when the value is already false, which
	// is what we want after a clear (the Settings checkbox needs the sync).
	a.emitE2EEModeChanged(false)

	if a.ctx != nil && a.eventsEmitter != nil {
		a.eventsEmitter(a.ctx, "relay:auth-info", map[string]any{"user_id": ""})
		a.eventsEmitter(a.ctx, "relay-config-changed")
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
	// Set the in-memory key now so the uplink (started by SetRelayConfig below)
	// seals its first announce. Persistence is deferred until the new relay URL
	// and user id are committed to config — see persistAccountKey below.
	a.setAccountKeyInMemory(res.AccountKey)
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
		cfg.RelayRealmID = res.RealmID
		cfg.RelayHomeInstanceURL = res.HomeInstanceURL
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	// Persist the account_key now that the realm id + user id are committed,
	// so the next launch's loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID)
	// finds it. Done after the config write — persisting earlier wrote under the
	// stale/empty user id and lost the key on relaunch.
	a.persistAccountKey(res.AccountKey)
	// Persist the password so SettingsRelay can prefill the password
	// field on subsequent launches. Failure is logged but does not fail
	// the login: the user already has a valid session token and account_key.
	// See docs/superpowers/specs/2026-06-23-desktop-relay-password-persistence-design.md.
	if err := saveRelayPassword(wsURL, email, password); err != nil {
		log.Printf("desktop: save relay password: %v", err)
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
	// In-memory now (uplink seals first announce); persist after URL + user id
	// are committed — see persistAccountKey below and LoginRemoteRelay.
	a.setAccountKeyInMemory(res.AccountKey)
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
		cfg.RelayRealmID = res.RealmID
		// Register never assigns a home instance (the relay sets home on login only);
		// clear any stale home from a prior account so the uplink falls back to RelayURL.
		cfg.RelayHomeInstanceURL = ""
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	a.persistAccountKey(res.AccountKey)
	// Persist the password so SettingsRelay can prefill the password
	// field on subsequent launches. Failure is logged but does not fail
	// the registration: the user already has a valid session token and account_key.
	// See docs/superpowers/specs/2026-06-23-desktop-relay-password-persistence-design.md.
	if err := saveRelayPassword(wsURL, email, password); err != nil {
		log.Printf("desktop: save relay password: %v", err)
	}
	return nil
}

// setAccountKey stores key as the current in-memory account_key AND persists
// it under the currently-configured (relay URL, user id). Used by callers
// whose config already reflects the right identity (e.g. logout clearing with
// nil). Login/register instead split the two steps — setAccountKeyInMemory
// early (so the uplink seals on its first announce) then persistAccountKey
// after the new URL + user id are committed — because persisting against the
// stale identity would write the keychain entry under the wrong (or empty)
// account name and lose it on the next launch.
func (a *App) setAccountKey(key []byte) {
	a.setAccountKeyInMemory(key)
	a.persistAccountKey(key)
}

// setAccountKeyInMemory updates the in-memory account_key and notifies the
// frontend WITHOUT touching the keychain. Concurrent callers see the most
// recent value via accountKeySnapshot.
func (a *App) setAccountKeyInMemory(key []byte) {
	a.accountKeyMu.Lock()
	if len(key) == 0 {
		a.accountKey = nil
	} else {
		a.accountKey = append([]byte(nil), key...)
	}
	a.accountKeyMu.Unlock()
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
	if err := saveAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID, key); err != nil {
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

// LoadSavedRelayPassword reads the password persisted by the most recent
// successful LoginRemoteRelay / RegisterRemoteRelay for the relay currently
// in the persisted config. Returns "" (no error) when nothing is stored,
// when RelayURL or RelayLastEmail is empty, or when the keychain entry is
// absent. Keychain errors other than "not found" are logged and surfaced
// as "" so the UI just shows an empty password field.
//
// Bound to the frontend's SettingsRelay onMounted prefill.
func (a *App) LoadSavedRelayPassword() (string, error) {
	if a.cfgStore == nil {
		return "", nil
	}
	cfg := a.cfgStore.Get()
	pw, err := loadRelayPassword(cfg.RelayURL, cfg.RelayLastEmail)
	if err != nil {
		log.Printf("desktop: load saved relay password: %v", err)
		return "", nil
	}
	return pw, nil
}

// RememberRelayPassword writes password into the safekeyring slot for the
// (RelayURL, RelayLastEmail) currently in cfgStore — used by the Settings
// form's "remember inputs on failed connect" path so the user does not
// have to retype the password after a probe failure / login failure /
// network blip. Empty password is intentionally treated as a no-op rather
// than a delete here, so a failure path with an empty password field
// cannot wipe an existing stored value.
//
// Best-effort: errors are logged and not surfaced to the UI (the real
// failure the user cares about is the underlying connect error). Caller
// is expected to have already called SetRelayConfig with the new URL +
// email so the slot key reflects the latest intent.
func (a *App) RememberRelayPassword(password string) error {
	if a.cfgStore == nil {
		return nil
	}
	if password == "" {
		return nil
	}
	cfg := a.cfgStore.Get()
	if err := saveRelayPassword(cfg.RelayURL, cfg.RelayLastEmail, password); err != nil {
		log.Printf("desktop: remember relay password: %v", err)
	}
	return nil
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

// GetPasteboardFileURLs returns the absolute file system paths of any file
// URL items currently on the system pasteboard (e.g. Cmd+C in Finder).
// Empty when the pasteboard carries no file URLs, or on non-darwin builds
// where no NSPasteboard-equivalent bridge is wired up yet. The frontend
// treats an empty slice as "no source path — fall back to PASTE_FILE upload".
func (a *App) GetPasteboardFileURLs() []string {
	return readPasteboardFileURLs()
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

// GetPinnedSessionIds returns the persisted list of session_ids the user
// has pinned to the top of the session bar. Always returns a non-nil
// slice so the frontend can rely on Array semantics.
func (a *App) GetPinnedSessionIds() []string {
	if a.cfgStore == nil {
		return []string{}
	}
	ids := a.cfgStore.Get().PinnedSessionIDs
	if ids == nil {
		return []string{}
	}
	return ids
}

// SetPinnedSessionIds persists the list, deduping and dropping empty
// entries while preserving first-occurrence order.
func (a *App) SetPinnedSessionIds(ids []string) error {
	if a.cfgStore == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	cfg := a.cfgStore.Get()
	cfg.PinnedSessionIDs = out
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
	baseHTTP := relayHTTPBase(cfg.RelayURL)
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

// ListRemoteSessions fetches the relay's owner-filtered session list over the
// Go HTTP client (relayHTTPClient) instead of a direct webview WebSocket.
//
// Some networks fingerprint-filter the desktop WebView's TLS handshake to the
// relay: WKWebView's fetch() and `new WebSocket()` to wss://relay are RST
// before the ServerHello ("An SSL error has occurred"), while Go's TLS — the
// same stack the uplink connects with successfully — passes. Routing the list
// through Go is therefore the only reliable path for the desktop client.
//
// Returns the raw /api/sessions JSON body so the frontend parses the exact
// same SessionInfo[] shape it would get over the WS LIST_RESP stream.
func (a *App) ListRemoteSessions() (string, error) {
	if a.cfgStore == nil {
		return "", fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return "", fmt.Errorf("no relay configured")
	}
	req, err := http.NewRequest("GET", relayHTTPBase(cfg.RelayURL)+"/api/sessions", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 10*time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("relay /api/sessions returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
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

// DownloadVersion downloads a specific version (the chosen update line's
// latest tag) instead of the default latest.
func (a *App) DownloadVersion(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.DownloadVersion(a.ctx, tag)
}

// CancelDownload interrupts an in-flight update download (if any) and
// clears the download state so the UI reverts to the pre-download
// primary button. Bound to Settings → Updates "Cancel (N%)" button.
func (a *App) CancelDownload() {
	if a.updater == nil {
		return
	}
	a.updater.Cancel()
}

// ForceRedownload deletes any existing archive for tag and downloads
// fresh. Bound to Settings → Updates "Redownload" button and the
// "redownload?" confirm prompt.
func (a *App) ForceRedownload(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.ForceRedownload(a.ctx, tag)
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

// GetAINotificationsOnly returns the current persisted preference.
// Defaults to true for fresh installs.
func (a *App) GetAINotificationsOnly() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().AINotificationsOnlyOrDefault()
}

// SetAINotificationsOnly persists the user's toggle.
func (a *App) SetAINotificationsOnly(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.AINotificationsOnly = &enabled
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("ai_notifications_only")
	return nil
}

// GetFeishuModePref returns the persisted Feishu mode preference
// ("auto" | "local" | "relay"). Empty / unknown values resolve to "auto".
func (a *App) GetFeishuModePref() string {
	if a.cfgStore == nil {
		return "auto"
	}
	return a.cfgStore.Get().FeishuModePrefOrDefault()
}

// SetFeishuModePref persists the new preference and triggers a hot
// reconcile of the running Feishu service. Validates against the three
// known values; rejects anything else without mutating state.
func (a *App) SetFeishuModePref(pref string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	switch pref {
	case "auto", "local", "relay":
	default:
		return fmt.Errorf("invalid feishu mode preference %q (want auto|local|relay)", pref)
	}
	cfg := a.cfgStore.Get()
	cfg.FeishuModePref = pref
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("feishu_mode_pref")
	a.reconcileFeishuMode(a.ctx, cfg)
	return nil
}

// GetFeishuEffectiveMode returns the currently-running Feishu mode
// ("local" | "relay"), or "" before startFeishu has run. Independent
// of the persisted preference — reflects the actual swapped state.
func (a *App) GetFeishuEffectiveMode() string {
	a.feishuMu.RLock()
	defer a.feishuMu.RUnlock()
	return a.feishuMode
}

// FeishuRemoteTerminalSettings is returned by GetFeishuRemoteTerminalSettings.
type FeishuRemoteTerminalSettings struct {
	Enabled    bool   `json:"enabled"`
	AutoAttach string `json:"auto_attach"`
}

// GetFeishuRemoteTerminalSettings returns the current binding's remote
// terminal settings. Returns defaults (false, "ai") when the relay host is
// unavailable or no binding exists.
//
// Wails-bound methods must not declare context.Context in their signature.
func (a *App) GetFeishuRemoteTerminalSettings() (FeishuRemoteTerminalSettings, error) {
	defaults := FeishuRemoteTerminalSettings{Enabled: false, AutoAttach: "ai"}
	if a.ctx == nil {
		return defaults, nil
	}
	// Local mode: read the keychain blob.
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(a.ctx)
		if err != nil {
			return defaults, nil // no blob yet → defaults
		}
		autoAttach := v.SessionAutoAttach
		if autoAttach == "" {
			autoAttach = "ai"
		}
		return FeishuRemoteTerminalSettings{
			Enabled:    v.RemoteTerminalEnabled,
			AutoAttach: autoAttach,
		}, nil
	}
	// Relay mode: read the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return defaults, nil
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err != nil {
		// Binding not yet created → return defaults, not an error.
		return defaults, nil
	}
	autoAttach := b.SessionAutoAttach
	if autoAttach == "" {
		autoAttach = "ai"
	}
	return FeishuRemoteTerminalSettings{
		Enabled:    b.RemoteTerminalEnabled,
		AutoAttach: autoAttach,
	}, nil
}

// SetFeishuRemoteTerminalSettings updates the remote terminal toggle and
// autoAttach mode for the current user's Feishu binding. If the enabled flag
// flipped, OnRemoteTerminalToggle is called to tear down (or arm) active
// subscribers.
//
// Wails-bound methods must not declare context.Context in their signature.
func (a *App) SetFeishuRemoteTerminalSettings(enabled bool, autoAttach string) error {
	if a.ctx == nil {
		return fmt.Errorf("app not ready")
	}
	// Local mode: write the keychain blob; the toggle side effect still runs
	// against the in-memory subscriber map below.
	if ls := a.localBindingStore(); ls != nil {
		prevEnabled := false
		if v, err := ls.Get(a.ctx); err == nil {
			prevEnabled = v.RemoteTerminalEnabled
		}
		if err := ls.SetRemoteTerminalSettings(a.ctx, enabled, autoAttach); err != nil {
			return err
		}
		if a.host != nil && prevEnabled != enabled {
			a.host.OnRemoteTerminalToggle(enabled)
		}
		return nil
	}
	// Relay mode: write the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return fmt.Errorf("relay host unavailable")
	}
	prev, _ := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err := a.host.sqliteStore.SetRemoteTerminalSettings(a.ctx, a.host.adminUserID, enabled, autoAttach); err != nil {
		return err
	}
	if prev != nil && prev.RemoteTerminalEnabled != enabled {
		a.host.OnRemoteTerminalToggle(enabled)
	}
	return nil
}

// GetPtyInputDebugEnabled reports whether PTY input debug logging is on.
func (a *App) GetPtyInputDebugEnabled() bool {
	if a.cfgStore == nil {
		return false
	}
	return a.cfgStore.Get().PtyInputDebugEnabledOrDefault()
}

// SetPtyInputDebugEnabled persists the PTY input debug logging toggle.
// No markPrefDirtyAndPush: the setting is read live per-write in
// desktopPtyHost.Write and has no reactive frontend consumer to push to.
func (a *App) SetPtyInputDebugEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.PtyInputDebugEnabled = &enabled
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
	baseHTTP := relayHTTPBase(cfg.RelayURL)
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

// ListRelaySessions returns every active session for the currently
// logged-in relay account. Bound to Settings → Signed-in Devices tab.
func (a *App) ListRelaySessions() ([]RelaySessionRow, error) {
	if a.cfgStore == nil {
		return nil, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	return a.meSessionsGET(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}

// RevokeRelaySession revokes one session by id_hash. The current
// session cannot be revoked through this method (the relay endpoint
// itself refuses to revoke the caller's own session, so no extra
// guard is needed here).
func (a *App) RevokeRelaySession(idHash string) error {
	idHash = strings.TrimSpace(idHash)
	if idHash == "" {
		return fmt.Errorf("id_hash is empty")
	}
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return fmt.Errorf("not authenticated")
	}
	return a.meSessionDELETE(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, idHash, cfg.AllowInsecureRelay)
}

// SignOutOtherRelaySessions revokes every session except the current
// one. Returns the number of sessions revoked.
func (a *App) SignOutOtherRelaySessions() (SignOutOthersResult, error) {
	if a.cfgStore == nil {
		return SignOutOthersResult{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return SignOutOthersResult{}, fmt.Errorf("not authenticated")
	}
	return a.meSessionsSignOutOthers(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
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
	baseHTTP := relayHTTPBase(cfg.RelayURL)
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
		case "ai_notifications_only":
			return c.AINotificationsOnly != nil
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
	if appdir.IsDev() {
		log.Printf("recovery: save %s", summarizeRecoverySnapshot(snap))
	}
	return a.recoveryStore.Save(snap)
}

// summarizeRecoverySnapshot renders a one-line view of what's being persisted
// — per pane: workload type, the atterm session id, and the captured AI
// session id (or "-"). Used by the dev save log so recovery state can be
// confirmed live (tail the log) without restarting.
func summarizeRecoverySnapshot(snap RecoverySnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tabs=%d", len(snap.Tabs))
	for _, t := range snap.Tabs {
		for _, p := range t.Panes {
			ai := "-"
			if p.AI != nil && p.AI.SessionID != "" {
				ai = p.AI.SessionID
			}
			fmt.Fprintf(&b, " [type=%s ai=%s cwd=%s]", p.SessionType, ai, p.LastCwd)
		}
	}
	return b.String()
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

// relayHTTPBase rewrites a stored relay WebSocket URL (wss://, ws://) to the
// HTTP scheme its REST endpoints are served over. http.Client rejects "wss"/
// "ws" with "unsupported protocol scheme", so every HTTP call to the relay must
// go through this first. A URL already using http(s):// is returned unchanged.
func relayHTTPBase(relayURL string) string {
	return strings.Replace(strings.Replace(relayURL, "wss://", "https://", 1), "ws://", "http://", 1)
}

// feishuServiceConfig builds the ServiceConfig for the current relay login
// state and returns it alongside the resolved mode ("relay" / "local"). Shared
// by startFeishu (first init) and reconcileFeishuMode (runtime switch).
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
	loggedIn := cfg.RelayURL != "" && cfg.RelaySessionToken != "" && !cfg.RelayPaused

	effective := "local"
	switch cfg.FeishuModePrefOrDefault() {
	case "local":
		effective = "local"
	case "relay":
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
			log.Printf("desktop: feishu mode=relay requested but not effectively logged in (RelayPaused=%v); falling back to local", cfg.RelayPaused)
		}
	default: // "auto"
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
		}
	}

	if effective == "relay" {
		// The stored relay URL is a WebSocket URL (wss://). The Feishu relay
		// store/token source make plain HTTP REST calls, and http.Client rejects
		// "wss"/"ws" ("unsupported protocol scheme"), so rewrite the scheme.
		return feishu.ServiceConfig{
			Mode:     feishu.ModeRelay,
			RelayURL: relayHTTPBase(cfg.RelayURL),
			// Reads cfgStore on each call so it stays current after token refresh.
			RelayToken: func() string {
				if a.cfgStore == nil {
					return ""
				}
				return a.cfgStore.Get().RelaySessionToken
			},
			// Same client the rest of the app uses for the relay: pins ALPN to
			// http/1.1 and trusts a self-signed relay when the user opted in.
			RelayHTTPClient: relayHTTPClient(cfg.AllowInsecureRelay, 10*time.Second),
			Sessions:        a.host,
		}, "relay"
	}
	return feishu.ServiceConfig{Mode: feishu.ModeLocal, Sessions: a.host}, "local"
}

// startFeishu constructs feishu.Service, starts the HookServer, writes the
// endpoint file, and wires up the relayHost. Called once from startup(); the
// runtime mode switch is handled by reconcileFeishuMode, which reuses the
// HookServer this starts.
func (a *App) startFeishu(ctx context.Context, cfg appConfig) {
	svcCfg, mode := a.feishuServiceConfig(cfg)

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
		a.host.SetFeishuDispatcher(svc.Dispatcher())
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
		// Wire the inbound router so LongConn card actions and reply messages
		// are routed through the CardIndex + FeishuSubscriber registry.
		router := internalfeishu.NewRouter(a.host.feishuCards, func(sessionID string) internalfeishu.Subscriber {
			a.host.feishuSubsMu.Lock()
			defer a.host.feishuSubsMu.Unlock()
			fs, ok := a.host.feishuSubs[sessionID]
			if !ok || fs == nil {
				return nil
			}
			return fs
		})
		svc.SetRouter(router)
	}

	if mode == "local" {
		if err := svc.EnsureLongConn(ctx); err != nil {
			// Not fatal — credentials may not be set yet.
			log.Printf("desktop: feishu long-conn: %v", err)
		}
	}

	a.feishuMu.Lock()
	a.feishuService = svc
	a.feishuMode = mode
	a.feishuHookSrv = svc.HookServer()
	a.feishuHookEndpoint = hookEndpoint
	a.feishuMu.Unlock()
	log.Printf("desktop: feishu service started (mode=%s endpoint=%s)", mode, hookEndpoint)
}

// reconcileFeishuMode rebuilds the Feishu service when the relay login state no
// longer matches the running mode (relay login/logout at runtime). The
// long-lived HookServer/listener is kept — only the dispatcher, store, token
// source and long-conn are swapped — so the ATTERM_HOOK_ENDPOINT already baked
// into open PTYs stays valid. No-op before first init or when already correct.
func (a *App) reconcileFeishuMode(ctx context.Context, cfg appConfig) {
	a.feishuMu.Lock()
	defer a.feishuMu.Unlock()
	if a.feishuService == nil || a.feishuHookSrv == nil {
		return // first init not done yet; startFeishu handles it
	}
	svcCfg, desired := a.feishuServiceConfig(cfg)
	if desired == a.feishuMode {
		return
	}
	newSvc, err := feishu.NewService(svcCfg)
	if err != nil {
		log.Printf("desktop: feishu reload (%s→%s): %v", a.feishuMode, desired, err)
		return
	}
	// Stop the outgoing service's long-conn (relay mode has none).
	if err := a.feishuService.CloseLongConn(ctx); err != nil {
		log.Printf("desktop: feishu close long-conn: %v", err)
	}
	// Repoint the persistent hook server + host at the new dispatcher. The
	// endpoint/port is unchanged, so already-spawned PTYs keep working.
	a.feishuHookSrv.SetDispatcher(newSvc.Dispatcher())
	if a.host != nil {
		a.host.SetFeishuDispatcher(newSvc.Dispatcher())
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
		// Rebuild the inbound router for the new service so LongConn events
		// are routed through the same CardIndex + FeishuSubscriber registry.
		router := internalfeishu.NewRouter(a.host.feishuCards, func(sessionID string) internalfeishu.Subscriber {
			a.host.feishuSubsMu.Lock()
			defer a.host.feishuSubsMu.Unlock()
			fs, ok := a.host.feishuSubs[sessionID]
			if !ok || fs == nil {
				return nil
			}
			return fs
		})
		newSvc.SetRouter(router)
	}
	a.feishuService = newSvc
	a.feishuMode = desired
	if desired == "local" {
		if err := newSvc.EnsureLongConn(ctx); err != nil {
			log.Printf("desktop: feishu long-conn after reload: %v", err)
		}
	}
	log.Printf("desktop: feishu mode reconciled → %s (endpoint unchanged %s)", desired, a.feishuHookEndpoint)
}

// FeishuStatusResp is returned by GetFeishuStatus.
//
// Enabled / RelayDisabled / Error are mutually exclusive views the UI renders
// differently:
//   - Enabled=true: integration is active (see Bound/Disabled for detail).
//   - RelayDisabled=true: relay mode, but the relay admin turned Feishu off.
//   - Error!="": the status fetch failed (network/keychain/etc.) — state unknown.
//   - all zero: integration is not running on this client.
type FeishuStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`
	Bound    bool   `json:"bound"`
	OpenID   string `json:"open_id"`
	Disabled bool   `json:"disabled"`
	// RelayDisabled is set when the relay responded that Feishu is disabled
	// server-side (HTTP 503). Distinct from "not configured".
	RelayDisabled bool `json:"relay_disabled,omitempty"`
	// Error carries a human-readable reason the status could not be fetched.
	// When set, the UI must NOT claim the integration is disabled — the real
	// state is unknown. Empty on success.
	Error string `json:"error,omitempty"`
	// Configured reports that app credentials are stored (regardless of bind
	// state). The UI uses it to render a "configured" view instead of an empty
	// form — credentials persist but are never echoed back, so without this the
	// form looks blank on reopen and reads as "not saved".
	Configured bool `json:"configured,omitempty"`
	// AppID echoes the stored (non-secret) App ID so the UI can show which app
	// is configured. Available in local mode; empty in relay mode (the relay
	// does not echo it back).
	AppID string `json:"app_id,omitempty"`
	// AppIDHash is sha256(AppID) — the suffix of the event callback URL.
	AppIDHash string `json:"app_id_hash,omitempty"`
	// CallbackURL is the relay event endpoint to paste into the Feishu console.
	// Set only in relay mode; empty in local mode (long-conn, no public URL).
	CallbackURL string `json:"callback_url,omitempty"`
}

// GetFeishuStatus returns the current Feishu integration state.
//
// Wails-bound methods must not declare context.Context in their signature —
// the generator surfaces it as a required JS argument, so the frontend call
// fails with "received 0 arguments, expected 1". Internal callers (long-conn,
// store, dispatcher) get the lifecycle context via a.ctx.
func (a *App) GetFeishuStatus() (FeishuStatusResp, error) {
	svc, mode := a.currentFeishu()
	if svc == nil {
		return FeishuStatusResp{Enabled: false}, nil
	}
	v, err := svc.Store().Get(a.ctx)
	if errors.Is(err, feishu.ErrLocalBindingNotFound) {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    mode,
			Bound:   false,
		}, nil
	}
	if errors.Is(err, feishu.ErrRelayFeishuDisabled) {
		// The relay reachable but the admin turned Feishu off server-side.
		return FeishuStatusResp{Mode: mode, RelayDisabled: true}, nil
	}
	if err != nil {
		// Status couldn't be fetched (network, keychain, relay error). Report
		// it as a non-nil Error rather than returning a Go error: the latter
		// surfaces as a rejected Promise that the UI silently swallowed, which
		// is exactly what made a transient failure look like "not enabled".
		return FeishuStatusResp{Mode: mode, Error: err.Error()}, nil
	}
	// Local mode keeps full credentials in the keychain; a blob with an OpenID
	// but no AppSecret (e.g. a stale bind left over after switching modes) is
	// effectively unconfigured — the long-conn and token mint both need the
	// secret. Treat it as "not configured" so the UI shows the credentials
	// form instead of a misleading "bound" view that can't actually send.
	// Relay mode never echoes the secret back, so this check is local-only.
	if mode == "local" && v.AppSecret == "" {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    mode,
			Bound:   false,
		}, nil
	}
	return FeishuStatusResp{
		Enabled:     true,
		Mode:        mode,
		Bound:       v.OpenID != "",
		OpenID:      v.OpenID,
		Disabled:    v.DisabledAt != 0,
		Configured:  true,
		AppID:       v.AppID,
		AppIDHash:   v.AppIDHash,
		CallbackURL: v.CallbackURL,
	}, nil
}

// currentFeishu returns the live service + mode under the read lock. Both may
// be swapped at runtime by reconcileFeishuMode on a relay login/logout.
func (a *App) currentFeishu() (*feishu.Service, string) {
	a.feishuMu.RLock()
	defer a.feishuMu.RUnlock()
	return a.feishuService, a.feishuMode
}

// localBindingStore returns the keychain-backed store when Feishu is running in
// local mode, or nil otherwise. Used to route remote-terminal settings to the
// keychain (relay mode keeps them in the embedded sqlite store).
func (a *App) localBindingStore() *feishu.LocalKeychainBindingStore {
	svc, mode := a.currentFeishu()
	if svc == nil || mode != "local" {
		return nil
	}
	ls, _ := svc.Store().(*feishu.LocalKeychainBindingStore)
	return ls
}

// feishuRemoteTermState reads the remote-terminal gate state for the live mode:
// the keychain blob in local mode, the embedded sqlite binding in relay mode.
// Returns ok=false when no binding exists yet or the store is unavailable.
func (a *App) feishuRemoteTermState(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool) {
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(ctx)
		if err != nil {
			return false, "", "", false
		}
		aa := v.SessionAutoAttach
		if aa == "" {
			aa = "ai"
		}
		return v.RemoteTerminalEnabled, v.OpenID, aa, true
	}
	if a.host == nil || a.host.sqliteStore == nil {
		return false, "", "", false
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(ctx, a.host.adminUserID)
	if err != nil {
		return false, "", "", false
	}
	aa := b.SessionAutoAttach
	if aa == "" {
		aa = "ai"
	}
	return b.RemoteTerminalEnabled, b.OpenID, aa, true
}

// SetFeishuCredentials saves app credentials and (re)starts the long-conn.
func (a *App) SetFeishuCredentials(c feishu.Credentials) error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	if err := svc.Store().SetCredentials(a.ctx, c); err != nil {
		return err
	}
	return svc.EnsureLongConn(a.ctx)
}

// BeginFeishuPair issues a short-code that the user sends to the bot via
// private chat to complete the bind flow. In relay mode the code is issued by
// the relay; in local mode it is generated in-process.
func (a *App) BeginFeishuPair() (string, error) {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return "", errors.New("feishu disabled")
	}
	return svc.BeginPair(a.ctx)
}

// DeleteFeishuBinding removes the bound OpenID from the store.
func (a *App) DeleteFeishuBinding() error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	return svc.Store().Delete(a.ctx)
}

// SendFeishuTestCard renders and sends one notification card to the bound
// OpenID through the live token + IM path, so the user can verify delivery from
// Settings without waiting for a real trigger. scenario is one of the
// feishu.TestCard* values ("command_success", "command_failure",
// "command_sealed", "waiting_input"). Any failure (not configured, disabled,
// unbound, or send error) is returned verbatim for the UI to display.
func (a *App) SendFeishuTestCard(scenario string) error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	return svc.SendTestCard(a.ctx, feishu.TestCardScenario(scenario))
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
