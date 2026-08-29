package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/desktop/hookinstall"
	"github.com/attson/atterm/internal/connhealth"
	"github.com/attson/atterm/internal/logging"
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

	// ProfileID explicitly selects a SessionProfile for this session,
	// overriding the configured default profile (see relayHost.NewSession's
	// precedence: explicit > default > default_shell/HOME). Empty means "no
	// explicit choice" — fall through to the default profile if one is set.
	ProfileID string `json:"profile_id,omitempty"`

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

// SSHConnectReq describes one SSH connection request from the frontend.
// Credentials are used for this connection only and never persisted (slice 1).
type SSHConnectReq struct {
	Host       string `json:"host"`
	Port       string `json:"port,omitempty"`
	User       string `json:"user"`
	AuthKind   string `json:"auth_kind"` // "password" | "privateKey"
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"` // PEM content (pasted or file-read)
	Passphrase string `json:"passphrase,omitempty"`
	Cols       uint16 `json:"cols,omitempty"`
	Rows       uint16 `json:"rows,omitempty"`
	// AcceptedHostKeyHost and AcceptedHostKeyFingerprint are set on a retry
	// after the user confirmed an unknown host fingerprint in the TOFU dialog:
	// they echo back the Host and Fingerprint of the *HostKeyUnknownError that
	// produced the dialog, so the acceptance names exactly one key on one
	// machine.
	//
	// They replace an "accept the next unknown key" bool, and once a connection
	// can run through a jump-host chain (roadmap item 27) that difference is a
	// security one rather than a stylistic one. KnownHostsCallback does not
	// merely let an accepted key through — it *appends it to known_hosts* — so a
	// bool would record keys for machines the user was never shown, and the next
	// connection to a substituted hop would prompt for nothing at all. See
	// acceptedHostKey in ssh_jump.go.
	AcceptedHostKeyHost        string `json:"accepted_host_key_host,omitempty"`
	AcceptedHostKeyFingerprint string `json:"accepted_host_key_fingerprint,omitempty"`
	// SSHHostID is set internally by NewSshSessionByID to the saved SSHHost.ID
	// so the adopted session carries it (for recovery reconnect), and so the
	// connection can find the host's ProxyJump chain. Empty for ad-hoc
	// connections. Not part of the frontend-facing request shape.
	SSHHostID string `json:"-"`
}

// acceptedHostKey is the (host, fingerprint) pair the user agreed to, in the
// form the chain builder and the host-key callbacks match on. A request that
// has not been through a TOFU dialog yields the zero value, which accepts
// nothing.
func (r SSHConnectReq) acceptedHostKey() acceptedHostKey {
	return acceptedHostKey{Host: r.AcceptedHostKeyHost, Fingerprint: r.AcceptedHostKeyFingerprint}
}

// errCodeHostKeyUnknown is the error string carried by HostKeyUnknownError so
// the frontend can recognize the TOFU case and re-issue with the accepted key.
const errCodeHostKeyUnknown = "ssh_host_key_unknown"

// errCredentialMissing is returned by NewSshSessionByID when the host has no
// stored credential; the frontend prompts the user to supply one.
const errCredentialMissing = "ssh_credential_missing"

// errKeyMissing is returned when a host references a key ID that no longer
// exists in the vault.
const errKeyMissing = "ssh_key_missing"

// HostKeyUnknownError carries the fingerprint so the frontend can show the
// TOFU dialog and retry with Host + Fingerprint echoed back in
// SSHConnectReq.AcceptedHostKeyHost / AcceptedHostKeyFingerprint.
type HostKeyUnknownError struct {
	Fingerprint string
	Host        string

	// HopIndex and HopName say *which machine* this fingerprint belongs to
	// when the connection runs through a jump-host chain (roadmap item 27).
	//
	// Without them the dialog shows an unfamiliar fingerprint for a
	// connection the user started by naming one host, and there is no way to
	// tell the destination from the second bastion on the way to it. Someone
	// accepting a key without knowing whose it is has turned TOFU into a
	// formality, which is exactly what a substituted bastion relies on.
	//
	// HopIndex is 1-based along the chain in dial order, with the target
	// last; 0 means the connection is direct and there is no chain to
	// disambiguate, so the dialog reads as it always has. HopName is the
	// hop's alias (or hostname when it has no alias).
	HopIndex int
	HopName  string
}

func (e *HostKeyUnknownError) Error() string { return errCodeHostKeyUnknown }

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
	// Level is the minimum severity written to the file. Distinct from the
	// viewer's level filter, which only decides what is rendered from a file
	// that has already been written.
	Level string `json:"level"`
}

type LogPreview struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

type StartupError struct {
	Fatal   bool   `json:"fatal"`
	Message string `json:"message"`
	LogPath string `json:"log_path"`
}

// App is the Wails-bound application surface.
type App struct {
	ctx         context.Context
	host        *relayHost
	remoteProxy *remoteProxy
	cfgStore    *configStore
	logger      *loggingManager

	mu               sync.Mutex
	uplink           *uplink
	uplinkCancel     context.CancelFunc
	prefsWatchCancel context.CancelFunc

	updater *Updater

	// quitApproved gates OnBeforeClose: once the frontend confirms a quit
	// via ConfirmQuit(), subsequent close attempts proceed without the
	// before-close prompt round trip.
	quitApproved atomic.Bool

	pluginFS *PluginFS

	// widget supervises the companion window ("桌面挂件" / Desk Widget) — a second process of
	// this executable launched with --widget. Always non-nil; whether a process
	// is actually running is widgetProcess's own state.
	widget *widgetProcess

	// recent relay errors — bounded ring, newest-first.
	relayErrMu  sync.Mutex
	relayErrors []RelayErrorEntry

	// writeFile is os.WriteFile in production; tests substitute a stub.
	writeFile writeFileFunc

	// saveDialog is wailsruntime.SaveFileDialog in production; ExportConfig's
	// tests substitute a stub so they never need a bound Wails frontend (see
	// saveDialogFunc in config_export.go).
	saveDialog saveDialogFunc

	// clearSSHCredential removes a host's stored credential. Injectable for
	// the same reason writeFile and saveDialog are: the real implementation
	// talks to the OS keychain, which a test cannot make fail on demand —
	// and the behaviour worth pinning is precisely what happens WHEN it
	// fails. Defaults to sshCredentialSlot(id).Clear() in NewApp.
	clearSSHCredential func(id string) error

	// sshKnownHostsPath overrides the known_hosts file used by NewSshSession
	// (tests set a temp path). Empty → ~/.ssh/known_hosts.
	sshKnownHostsPath string

	// tunnels owns the running SSH port forwards (see ssh_tunnels.go). It is
	// deliberately independent of host/relayHost: a tunnel is not a session,
	// so it never enters the registry or a subscriber count. Zero value is
	// usable; it holds its own lock.
	tunnels tunnelManager

	// servicePreviews owns loopback listeners opened by the desktop Preview
	// bridge. It is independent of PTY/session lifecycle and is stopped on
	// relay reconfiguration or app shutdown.
	servicePreviews servicePreviewManager

	// sftp holds the file explorer's SSH data source (see sftp_source.go).
	// Built lazily by sftpBrowser() because App is constructed in a dozen
	// shapes and none of them should have to remember this one.
	sftpMu sync.Mutex
	sftp   *sftpBrowser

	// prefsSync is layered behind the prefsSyncEngine interface (defined in
	// prefs_sync_loop.go) rather than typed as *prefssync.Engine directly, so
	// tests can substitute a fake without internal/prefssync growing any
	// desktop-only test scaffolding.
	prefsSync prefsSyncEngine

	// prefsSyncCh and prefsSyncTaskCh feed the serial sync loop in
	// prefs_sync_loop.go. That file is the only place allowed to call a
	// method on prefsSync — see its file-level comment for why.
	prefsSyncCh     chan syncRequest
	prefsSyncTaskCh chan func(prefsSyncEngine)
	// prefsSyncLoopDone is closed when runPrefsSyncLoop returns. Test-only
	// signal — production code never reads it, see startPrefsSyncLoop.
	prefsSyncLoopDone chan struct{}

	// syncStatusMu guards the fields below it, which back GetSyncStatus /
	// SyncNow (see prefs_sync_loop.go). Read from any goroutine (a Wails
	// call from the frontend); written only by the serial sync loop
	// goroutine, via recordSyncOutcome / setSyncBusy — never touched
	// directly outside those two so there is exactly one writer.
	syncStatusMu sync.Mutex
	// syncBusy is true for the duration of one runSyncRequest/runSyncTask
	// call on the loop goroutine -- surfaced as SyncStatus.State ==
	// "syncing" (when a relay is actually configured; see syncOffline).
	syncBusy bool
	// syncLastSyncedAt is the ms timestamp of the most recent Pull or Push
	// that returned no error. Zero means never.
	syncLastSyncedAt int64
	// syncLastError is the Error() text of the most recent failed Pull or
	// Push. Cleared (set back to "") the next time either succeeds -- see
	// recordSyncOutcome. This is deliberately not sticky: the whole point
	// of this field, per markPrefDirtyAndPush's comment about
	// ssh_hosts_encrypted staying silently broken for months, is that a
	// failure must surface, and a success must be able to say so too.
	syncLastError string
	// syncLastEmitted is the last full SyncStatus value actually sent over
	// the "sync:status" event, so emitSyncStatusIfChanged only fires on an
	// observable change to *any* field -- not just State. Deduping on State
	// alone let three edits made back-to-back while offline collapse into a
	// single emitted event still carrying the PendingKeys count from before
	// any of them landed: State stays "offline" throughout, but PendingKeys
	// (and, for other transitions, LastSyncedAt/LastError) genuinely changes
	// and the frontend needs to see that. SyncStatus's fields are all plain
	// comparable types (string/int64/int), so the whole struct can be
	// compared with == rather than a field-by-field diff.
	syncLastEmitted SyncStatus

	// accountKey is the user's E2EE account_key (32 bytes) unlocked by
	// the most recent successful LoginRemoteRelay / RegisterRemoteRelay.
	// In-memory only in v1 — lost on app restart, requires re-login. A
	// future milestone will persist it via OS keychain. Protected by
	// accountKeyMu because the uplink reads it while the foreground
	// thread might rewrap during password change.
	accountKeyMu sync.Mutex
	accountKey   []byte

	startupFatalMu sync.RWMutex
	startupFatal   StartupError

	// eventsEmitter is the Wails EventsEmit function used to push events
	// to the frontend. Defaults to wailsruntime.EventsEmit in NewApp;
	// tests substitute a no-op so they don't crash on wailsruntime's
	// strict context check. Same pattern as uplink.eventsEmit.
	eventsEmitter func(ctx context.Context, name string, data ...interface{})

	// snippetDialer opens the connection a batch snippet run executes a
	// command over (see snippet_run.go). Defaults to dialSnippetConn in
	// NewApp; tests substitute a fake so the batch-run tests don't need a
	// live SSH server, the same injectability pattern eventsEmitter uses and
	// for the same reason.
	snippetDialer func(context.Context, SSHHost) (snippetConn, error)

	// snippetRunsMu guards snippetRuns. Its own lock rather than mu: a batch
	// run's lifetime has nothing to do with the uplink/prefs state mu already
	// serializes, and sharing it would block one on the other for no reason.
	snippetRunsMu sync.Mutex
	snippetRuns   map[string]*snippetRun

	// windowActivator raises and focuses the main window for user-driven
	// actions arriving outside the main webview (for example the widget child
	// process). Injectable so event-routing tests do not need a live Wails UI.
	windowActivator func(ctx context.Context)

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
		cfgStore:        cfgStore,
		logger:          logger,
		pluginFS:        NewPluginFS(),
		eventsEmitter:   wailsruntime.EventsEmit,
		windowActivator: activateMainWindow,
	}
	a.snippetDialer = a.dialSnippetConn
	a.updater = newUpdater(updaterConfig{
		current:         Version,
		repo:            "attson/atterm",
		verifyPublicKey: parseUpdateVerifyPublicKey(UpdateVerifyPublicKey),
	})
	a.widget = newWidgetProcess(a.handleWidgetEvent)
	a.observeConfigStore()
	return a
}

// observeConfigStore hangs the config-driven reconciles off the store's
// post-commit observer.
//
// Today that is exactly one thing: running port-forward tunnels have to follow
// the saved rules, because deleting a rule (in the drawer, by deleting the
// host, or on another device that syncs the deletion here) has to stop the
// tunnel — nothing else can, since the tunnels tab renders from the saved
// rules and an orphan therefore has no row. See tunnelManager.reconcile.
//
// It is wired here and in startup because a.cfgStore can be supplied either
// way; calling it twice just replaces the observer with an identical one.
func (a *App) observeConfigStore() {
	if a.cfgStore == nil {
		return
	}
	a.cfgStore.setOnCommit(func(c appConfig) { a.tunnels.reconcile(c.SSHHosts) })
}

// startup is called when the Wails runtime is ready. Boot the in-process
// relay, load persisted config, and apply it (which may start the uplink).
// ATTERM_RELAY_URL/TOKEN env vars are honored only when no config file
// exists yet — they seed the first run.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	wailsruntime.OnNotificationResponse(ctx, a.handleNotificationResponse)
	a.pluginFS.setupWatcher(ctx)
	// cfgStore must be ready before startRelayHost — the relay host's
	// bootstrap admin password lives in appConfig, so it has to be loaded
	// (and possibly written on first run) before we open the userstore.
	if a.cfgStore == nil {
		a.cfgStore = loadConfig()
	}
	a.observeConfigStore()
	h, err := startRelayHost(a.cfgStore)
	if err != nil {
		a.setStartupFatalError("start relay host", err)
		return
	}
	a.host = h
	// A session forked for a network-originated request (mobile
	// TypeSessionCreate) must survive an uplink reconnect the same way a
	// local tab already does — see relayHost.forkCtx's doc comment.
	h.SetForkContext(a.ctx)

	// Loopback proxy for remote-session attaches: the WebView can't open a TLS
	// WebSocket to the relay on networks that fingerprint-filter its handshake,
	// so the frontend tunnels /client through Go (whose TLS passes). Non-fatal:
	// remote viewing still works via ListRemoteSessions if this fails to bind.
	if rp, err := startRemoteProxy(a.cfgStore); err != nil {
		logWarn("remote-proxy", "start remote proxy: %v", err)
	} else {
		a.remoteProxy = rp
	}

	if rs, err := NewRecoveryStore(a.host.hostID); err == nil {
		a.recoveryStore = rs
	} else {
		logWarn("recovery", "store unavailable: %v", err)
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
			a.setStartupFatalError("init logging", err)
			return
		}
	}
	// Restore the E2EE account_key from the OS keychain if a previous
	// login persisted one. Failures are logged but never fatal — a
	// missing or corrupted entry just means the user has to log in
	// again. This MUST run before applyRelayConfig so the uplink picks
	// up the unlocked key on the same boot.
	if cfg.RelayURL != "" && cfg.RelaySessionUserID != "" {
		if key, err := loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID); err != nil {
			logWarn("keychain", "load persisted account_key: %v", err)
		} else if len(key) > 0 {
			a.accountKeyMu.Lock()
			a.accountKey = key
			a.accountKeyMu.Unlock()
			logInfo("keychain", "account_key restored from keychain (user=%s)", cfg.RelaySessionUserID)
		}
	}
	a.applyRelayConfig(cfg)
	// Auto-install ~/.claude/settings.json hook entries + materialize
	// atterm-hook binary, so a fresh install gets Feishu notifications
	// without manual settings.json editing. Failure is non-fatal — the
	// Settings · Feishu panel will surface the LastError.
	if cfg.HookAutoInstallEnabledOrDefault() {
		if err := hookinstall.Install(ctx); err != nil {
			logWarn("hookinstall", "install: %v", err)
		}
	}
	if a.updater != nil {
		a.updater.SetGHProxyURL(cfg.UpdateGHProxyURL)
	}

	adapter := newAppConfigAdapter(a.cfgStore, a.accountKeyForSync)
	relayClient := newHTTPRelayClient(a.cfgStore)
	a.prefsSync = prefssync.NewEngine(adapter, relayClient)
	a.startPrefsSyncLoop()

	// Trigger an initial pull if already logged in. Goes through the serial
	// loop (see prefs_sync_loop.go) like every other prefsSync call.
	if cfg := a.cfgStore.Get(); cfg.RelaySessionToken != "" {
		a.enqueueSync(syncRequest{pull: true})
	}

	// Start the preference watch NOW, not in the applyRelayConfig above.
	//
	// applyRelayConfig -> applyRelayPrefsWatch returns early when
	// a.prefsSync == nil (prefs_watch.go), and a.prefsSync is not constructed
	// until sixteen lines further down this function — so on a cold boot the
	// watch never started at all. It only came up on a LATER applyRelayConfig:
	// a relay reconfigure or a login. Until the user happened to do one of
	// those, a preference changed on another machine never arrived, which is
	// indistinguishable from "sync is broken".
	//
	// Re-running it here is safe and not a double-start: applyRelayPrefsWatch
	// cancels any existing watch before deciding whether to start one, so the
	// earlier no-op call and this one collapse to a single live watch.
	a.applyRelayPrefsWatch(a.cfgStore.Get())

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
	a.servicePreviews.stopAll()
	a.mu.Lock()
	if a.uplinkCancel != nil {
		a.uplinkCancel()
		a.uplinkCancel = nil
	}
	if a.prefsWatchCancel != nil {
		a.prefsWatchCancel()
		a.prefsWatchCancel = nil
	}
	a.mu.Unlock()
	// Kill the companion window before anything else tears down: it is an
	// always-on-top window the user cannot close by itself, so leaving it
	// behind would strand a floating card on their desktop.
	if a.widget != nil {
		a.widget.Stop()
	}
	if a.updater != nil {
		a.updater.Stop()
	}
	// End file browsing before the tunnels: an SFTP session holds a reference
	// on the same per-host connection a tunnel does, so releasing it first is
	// what lets stopAll actually close the login rather than leave it held by
	// a panel nobody is looking at any more.
	a.sftpMu.Lock()
	browser := a.sftp
	a.sftpMu.Unlock()
	if browser != nil {
		browser.closeAll()
	}
	// Close forwarded local ports; a listener outliving the window would keep
	// the port busy for whatever the user starts next.
	a.tunnels.stopAll()
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
		logWarn("feishu", "delete feishu endpoint file: %v", err)
	}
}

// applyRelayConfig reconciles everything that depends on the relay login state:
// the uplink and the Feishu integration mode. Caller need not hold a.mu.
func (a *App) applyRelayConfig(cfg appConfig) {
	a.servicePreviews.stopAll()
	a.applyRelayUplink(cfg)
	a.applyRelayPrefsWatch(cfg)
	// Feishu mode follows the relay login state: relay when logged in, local
	// otherwise. Done outside a.mu (reconcile uses its own lock and may touch
	// the long-conn). No-op until startFeishu has run.
	a.reconcileFeishuMode(a.ctx, cfg)
	// Every caller of this function has just changed something that can flip
	// sync between offline and online — pause, unpause, log in, log out, or
	// a different relay. None of them enqueue any sync work, so the sync
	// loop's own emitters never run and the Settings indicator would keep
	// showing whatever it last saw. See syncOfflineChanged: it also drops
	// the recorded error, which otherwise reappears red and stale the moment
	// the config is online again with nothing yet synced.
	a.syncOfflineChanged()
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
	if a.prefsWatchCancel != nil {
		a.prefsWatchCancel()
		a.prefsWatchCancel = nil
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
		logInfo("uplink", "disabled (%s)", reason)
		return
	}
	if err := validateRelayEndpoint(cfg.RelayURL, cfg.AllowInsecureRelay); err != nil {
		logWarn("uplink", "disabled: %v", err)
		return
	}
	uplinkCtx, cancel := context.WithCancel(a.ctx)
	a.uplinkCancel = cancel
	dialURL := uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	a.uplink = newUplink(dialURL, cfg.RelaySessionToken, cfg.RemotePermissionOrDefault(), a.host, a.recordRelayError, a.agentSealAccountKey, cfg.AllowInsecureRelay)
	go a.uplink.Run(uplinkCtx)
	logInfo("uplink", "configured for %s", dialURL)
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

func (a *App) setStartupFatalError(stage string, err error) {
	if err == nil {
		return
	}
	msg := strings.TrimSpace(stage)
	if msg != "" {
		msg += ": "
	}
	msg += err.Error()
	payload := StartupError{
		Fatal:   true,
		Message: msg,
		LogPath: a.startupLogPath(),
	}
	a.startupFatalMu.Lock()
	a.startupFatal = payload
	a.startupFatalMu.Unlock()
	logError("app", "startup fatal: %s", msg)
}

func (a *App) startupLogPath() string {
	cfg := appConfig{}
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	if a.logger != nil {
		return a.logger.EffectivePath(cfg.LogFilePath)
	}
	return cfg.LogFilePathOrDefault()
}

func (a *App) GetStartupError() StartupError {
	a.startupFatalMu.RLock()
	payload := a.startupFatal
	a.startupFatalMu.RUnlock()
	if payload.Fatal && payload.LogPath == "" {
		payload.LogPath = a.startupLogPath()
	}
	return payload
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
		logWarn("keychain", "clear relay password keychain slot: %v", err)
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
		Level:         cfg.LogLevelOrDefault().String(),
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

	// An empty level means "leave it alone", so callers that only touch the
	// path or the on/off switch cannot silently reset a level the user chose.
	level := prevCfg.LogLevelOrDefault()
	if trimmed := strings.TrimSpace(req.Level); trimmed != "" {
		parsed, ok := logging.ParseLevel(trimmed)
		if !ok {
			return fmt.Errorf("unknown log level %q", req.Level)
		}
		level = parsed
	}

	prevState := loggingConfigState{
		enabled: prevCfg.LogToFileEnabledOrDefault(),
		path:    prevCfg.LogFilePath,
		level:   prevCfg.LogLevelOrDefault(),
	}
	nextCfg := prevCfg
	nextCfg.LogFilePath = path
	nextCfg.LogToFileEnabled = &req.Enabled
	nextCfg.LogLevel = level.String()

	if a.logger != nil {
		if err := a.logger.Apply(loggingConfigState{
			enabled: req.Enabled,
			path:    path,
			level:   level,
		}); err != nil {
			return err
		}
	} else {
		logging.SetLevel(level)
	}
	if err := a.cfgStore.Set(nextCfg); err != nil {
		if a.logger != nil {
			_ = a.logger.Apply(prevState)
		} else {
			logging.SetLevel(prevState.level)
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
	if urls := readPasteboardFileURLs(); urls != nil {
		return urls
	}
	// Not `return readPasteboardFileURLs()`: the darwin bridge returns nil for
	// an empty pasteboard and the non-darwin stub always does, and a nil slice
	// crosses the wails bridge as JSON `null`, not `[]` — which is what the
	// comment above promises and what every caller destructures.
	return []string{}
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
	theme = strings.TrimSpace(theme)
	if !isSupportedTerminalTheme(theme) {
		return fmt.Errorf("bad terminal theme: %s", theme)
	}
	return a.updatePref("terminal_theme", func(cfg *appConfig) error {
		cfg.TerminalTheme = theme
		return nil
	})
}

// GetTerminalFontHead returns the user's chosen leading monospace family,
// or "" for the built-in chain. Only the head is user-controlled — the
// CJK-aware tail is composed on the frontend (redline #13).
func (a *App) GetTerminalFontHead() string {
	if a == nil || a.cfgStore == nil {
		return ""
	}
	return a.cfgStore.Get().TerminalFontHeadOrDefault()
}

func (a *App) SetTerminalFontHead(head string) error {
	return a.updatePref("terminal_font_head", func(cfg *appConfig) error {
		cfg.TerminalFontHead = strings.TrimSpace(head)
		return nil
	})
}

func (a *App) GetTerminalFontSize() int {
	if a == nil || a.cfgStore == nil {
		return terminalFontSizeDefault
	}
	return a.cfgStore.Get().TerminalFontSizeOrDefault()
}

func (a *App) SetTerminalFontSize(px int) error {
	if px < terminalFontSizeMin || px > terminalFontSizeMax {
		return fmt.Errorf("font size out of range: %d", px)
	}
	return a.updatePref("terminal_font_size", func(cfg *appConfig) error {
		cfg.TerminalFontSize = px
		return nil
	})
}

func (a *App) GetTerminalLineHeight() float64 {
	if a == nil || a.cfgStore == nil {
		return terminalLineHeightDefault
	}
	return a.cfgStore.Get().TerminalLineHeightOrDefault()
}

func (a *App) SetTerminalLineHeight(v float64) error {
	if v < terminalLineHeightMin || v > terminalLineHeightMax {
		return fmt.Errorf("line height out of range: %v", v)
	}
	return a.updatePref("terminal_line_height", func(cfg *appConfig) error {
		cfg.TerminalLineHeight = v
		return nil
	})
}

func (a *App) GetTerminalCursorStyle() string {
	if a == nil || a.cfgStore == nil {
		return terminalCursorStyleDefault
	}
	return a.cfgStore.Get().TerminalCursorStyleOrDefault()
}

func (a *App) SetTerminalCursorStyle(style string) error {
	style = strings.TrimSpace(style)
	if !isSupportedCursorStyle(style) {
		return fmt.Errorf("bad cursor style: %s", style)
	}
	return a.updatePref("terminal_cursor_style", func(cfg *appConfig) error {
		cfg.TerminalCursorStyle = style
		return nil
	})
}

func (a *App) GetTerminalCursorBlink() bool {
	if a == nil || a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().TerminalCursorBlinkOrDefault()
}

func (a *App) SetTerminalCursorBlink(on bool) error {
	return a.updatePref("terminal_cursor_blink", func(cfg *appConfig) error {
		cfg.TerminalCursorBlink = &on
		return nil
	})
}

func (a *App) GetTerminalScrollback() int {
	if a == nil || a.cfgStore == nil {
		return terminalScrollbackDefault
	}
	return a.cfgStore.Get().TerminalScrollbackOrDefault()
}

func (a *App) SetTerminalScrollback(lines int) error {
	if lines <= 0 || lines > terminalScrollbackMax {
		return fmt.Errorf("scrollback out of range: %d", lines)
	}
	return a.updatePref("terminal_scrollback", func(cfg *appConfig) error {
		cfg.TerminalScrollback = lines
		return nil
	})
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
	return a.updatePref("locale_preference", func(cfg *appConfig) error {
		switch preference {
		case localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified:
			cfg.LocalePreference = preference
			return nil
		default:
			return errors.New("unsupported locale preference")
		}
	})
}

func (a *App) GetDefaultShell() string {
	if a.cfgStore == nil {
		return defaultShellAuto
	}
	return a.cfgStore.Get().DefaultShellOrDefault()
}

// validateDefaultShell normalizes and validates a candidate default-shell
// value: "" and any case-insensitive spelling of "auto" normalize to
// defaultShellAuto and always pass; anything else must resolve via
// exec.LookPath on THIS machine. Factored out of SetDefaultShell so
// PreviewConfigImport (config_import.go) can run the exact same check
// against an inbound file before promising a "replace" that
// ApplyConfigImport — which is expected to call SetDefaultShell — would
// then refuse. Two copies of this rule would drift the moment either one
// changed what counts as a valid shell.
func validateDefaultShell(shell string) (string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" || strings.EqualFold(shell, defaultShellAuto) {
		return defaultShellAuto, nil
	}
	if _, err := exec.LookPath(shell); err != nil {
		return "", fmt.Errorf("default shell not found: %s", shell)
	}
	return shell, nil
}

func (a *App) SetDefaultShell(shell string) error {
	shell, err := validateDefaultShell(shell)
	if err != nil {
		return err
	}
	return a.updatePref("default_shell", func(cfg *appConfig) error {
		cfg.DefaultShell = shell
		return nil
	})
}

// GetShortcutBindings returns the user's action-id → binding overrides.
func (a *App) GetShortcutBindings() map[string]string {
	if a == nil || a.cfgStore == nil {
		return map[string]string{}
	}
	b := a.cfgStore.Get().ShortcutBindings
	out := make(map[string]string, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// SetShortcutBindings replaces the override map wholesale.
//
// Validates before touching the store — the same rule ValidatePluginConfig
// applies to Plugins.Shortcuts.Bindings (empty action id, malformed binding
// string), reusing isValidShortcutBinding rather than a second copy of the
// predicate. This key now syncs, so an unvalidated write here wouldn't just
// break the local machine: it would propagate a malformed binding to every
// other device the user owns via prefsSync, where it flows straight into
// buildRoutingTable on the frontend. A rejected call must leave the
// previously stored map untouched, so validation runs against the incoming
// argument before updatePref's mutate callback ever assigns it — mutate
// returning an error means updatePref never calls cfgStore.Set, so the
// config on disk (and in memory) is exactly what it was before this call.
// validateShortcutBindings reports the first rule SetShortcutBindings would
// reject — an empty action id, or a binding string isValidShortcutBinding
// rejects — checked in sorted action-id order so the reported violation is
// deterministic rather than whatever order Go's map iteration happens to
// produce. Factored out for the same reason validateDefaultShell is: shared
// by SetShortcutBindings (which rejects the whole incoming map on any single
// violation) and PreviewConfigImport, which must report that same
// all-or-nothing refusal instead of promising a "replace" apply would
// actually error on.
func validateShortcutBindings(bindings map[string]string) error {
	ids := make([]string, 0, len(bindings))
	for actionID := range bindings {
		ids = append(ids, actionID)
	}
	sort.Strings(ids)
	for _, actionID := range ids {
		if actionID == "" {
			return errors.New("shortcutBindings: action id must be non-empty")
		}
		if !isValidShortcutBinding(bindings[actionID]) {
			return fmt.Errorf("shortcutBindings[%q]: malformed binding %q", actionID, bindings[actionID])
		}
	}
	return nil
}

func (a *App) SetShortcutBindings(bindings map[string]string) error {
	if err := validateShortcutBindings(bindings); err != nil {
		return err
	}
	return a.updatePref("shortcut_bindings", func(cfg *appConfig) error {
		cfg.ShortcutBindings = bindings
		return nil
	})
}

// GetProfiles returns the user's saved session-launch profiles.
// cfgStore.Get() already deep-copies each profile's Env map (see
// detachMaps in config.go), so the returned slice is safe for the caller to
// mutate without affecting the store.
func (a *App) GetProfiles() []SessionProfile {
	if a == nil || a.cfgStore == nil {
		return []SessionProfile{}
	}
	if p := a.cfgStore.Get().Profiles; p != nil {
		return p
	}
	return []SessionProfile{}
}

// SetProfiles replaces the profile list wholesale. Validates before touching
// the store, same discipline as SetShortcutBindings: a rejected call must
// leave the previously stored profiles untouched.
func (a *App) SetProfiles(profiles []SessionProfile) error {
	seen := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		// Compare trimmed ids, matching filterValidProfiles' inbound check
		// (profiles.go). Unreachable in practice with generated UUIDs — the
		// only caller (SettingsProfiles.vue) never emits whitespace-padded
		// ids — but the two validation boundaries should agree on what an id
		// is rather than silently diverge on an edge case neither exercises.
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return errors.New("profiles: id must be non-empty")
		}
		if seen[id] {
			return fmt.Errorf("profiles: duplicate id %q", id)
		}
		seen[id] = true
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("profiles[%q]: name must be non-empty", id)
		}
	}
	return a.updatePref("profiles_encrypted", func(cfg *appConfig) error {
		cfg.Profiles = profiles
		// Structural guarantee, not a caller's responsibility: if this write
		// removes the profile DefaultProfileID currently names, the default
		// must fall back to "" rather than persist a dangling reference.
		// resolveDefaultProfileID already exists for the inbound-sync half
		// of this same invariant (openProfiles' merge path); reuse it here
		// so a local SetProfiles call (the UI's delete path already
		// compensates, but nothing else does — see SettingsProfiles.vue's
		// deleteProfile) can't strand it either.
		cfg.DefaultProfileID = resolveDefaultProfileID(cfg.DefaultProfileID, profiles)
		return nil
	})
}

// GetDefaultProfileID returns the profile used for new tabs/splits when none
// is explicitly picked. Empty means "no default".
func (a *App) GetDefaultProfileID() string {
	if a == nil || a.cfgStore == nil {
		return ""
	}
	return a.cfgStore.Get().DefaultProfileID
}

// SetDefaultProfileID persists the default profile choice. Marked dirty
// under the same "profiles_encrypted" key as SetProfiles — the default
// selection is part of the same user-facing "profiles" preference, wired
// into the actual sync payload by Task 3.
func (a *App) SetDefaultProfileID(id string) error {
	return a.updatePref("profiles_encrypted", func(cfg *appConfig) error {
		cfg.DefaultProfileID = id
		return nil
	})
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

// GetAppVersion returns the app version injected at build time via
// -ldflags -X main.Version=<tag>. Empty / "dev" for unbuilt dev runs.
func (a *App) GetAppVersion() string {
	return Version
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
	return a.updatePref("pinned_session_ids", func(cfg *appConfig) error {
		cfg.PinnedSessionIDs = out
		return nil
	})
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
	var out []string
	seen := map[string]bool{}
	shellPriorityOrder(a.cfgStore, func(shell string) bool {
		if shell == "" {
			return true
		}
		path, err := exec.LookPath(shell)
		if err != nil {
			return true
		}
		key := strings.ToLower(filepath.Base(path))
		if seen[key] {
			return true
		}
		out = append(out, path)
		seen[key] = true
		return true
	})
	if out == nil {
		// A machine where none of the candidates resolved. Returning the nil
		// slice would reach the frontend as JSON `null` rather than `[]`.
		return []string{}
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
			logWarn("recovery", "MarkCleanShutdown on close: %v", err)
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
	return a.updatePref("notifications_enabled", func(cfg *appConfig) error {
		cfg.NotificationsEnabled = &enabled
		return nil
	})
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
	return a.updatePref("ai_notifications_only", func(cfg *appConfig) error {
		cfg.AINotificationsOnly = &enabled
		return nil
	})
}

// GetFeishuModePref returns the persisted Feishu mode preference

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
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.PtyInputDebugEnabled = &enabled
		return nil
	})
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
	return a.updatePref("shell_integration_enabled", func(cfg *appConfig) error {
		cfg.ShellIntegrationEnabled = &enabled
		return nil
	})
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
	return a.updatePref("command_notify_threshold_seconds", func(cfg *appConfig) error {
		cfg.CommandNotifyThresholdSeconds = &seconds
		return nil
	})
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

// updatePref applies mutate to a snapshot of the config, persists it, and
// (when key is non-empty) marks the key dirty so prefsSync pushes it out.
// The mutate closure is where each Set* method's per-field validation and
// assignment lives — return an error to abort the update before the store
// is written. Pass key="" to skip the sync push for settings that have no
// reactive consumer (e.g. SetPtyInputDebugEnabled, where the value is read
// live per-write).
func (a *App) updatePref(key string, mutate func(*appConfig) error) error {
	if a == nil || a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	if err := mutate(&cfg); err != nil {
		return err
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if key != "" {
		a.markPrefDirtyAndPush(key)
	}
	return nil
}

// markPrefDirtyAndPush moved to prefs_sync_loop.go, alongside every other
// call into the prefs sync engine.

// accountKeyForSync returns a copy of the unlocked E2EE account key, or nil
// when E2EE is not active. Used by the prefssync adapter to seal/open the SSH
// host list — nil means "local only, never sync credentials to the relay".
func (a *App) accountKeyForSync() []byte {
	a.accountKeyMu.Lock()
	defer a.accountKeyMu.Unlock()
	if len(a.accountKey) == 0 {
		return nil
	}
	out := make([]byte, len(a.accountKey))
	copy(out, a.accountKey)
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
		case "pinned_session_ids":
			return len(c.PinnedSessionIDs) > 0
		// The nine L1 keys below: this switch is the only thing standing
		// between a user and losing their config on first login. Any key not
		// named here is silently treated as "never customized" and gets
		// overwritten by whatever SeedFromLocal finds on the relay (including
		// nothing at all). Each predicate answers "did the user explicitly
		// set this?", not "does this differ from the default?" — zero/empty
		// is the raw-field signal for "never set" (see ReadValue in
		// prefssync_adapter.go).
		case "terminal_theme":
			return c.TerminalTheme != ""
		case "terminal_font_head":
			return c.TerminalFontHead != ""
		case "terminal_font_size":
			return c.TerminalFontSize != 0
		case "terminal_line_height":
			return c.TerminalLineHeight != 0
		case "terminal_cursor_style":
			return c.TerminalCursorStyle != ""
		case "terminal_cursor_blink":
			return c.TerminalCursorBlink != nil
		case "terminal_scrollback":
			return c.TerminalScrollback != 0
		case "default_shell":
			return c.DefaultShell != ""
		case "shortcut_bindings":
			return len(c.ShortcutBindings) > 0
		case "profiles_encrypted":
			return len(c.Profiles) > 0
		case "ssh_hosts_encrypted":
			// Missing until now, while its sibling profiles_encrypted was
			// present — which is exactly the accident the comment above
			// describes. Without this case, a machine that already has SSH
			// hosts marks them "never customized" on first login, so
			// SeedFromLocal never flags them and they are never uploaded;
			// and because their local stamp stays 0, a relay already holding
			// an older set from another machine wins the comparison in Pull
			// and replaces them. Losing a host list on first login is not a
			// cosmetic omission.
			return len(c.SSHHosts) > 0 || len(c.SSHKeys) > 0
		}
		return false
	}
}
