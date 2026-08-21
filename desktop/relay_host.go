package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/desktop/shellintegration"
	"github.com/attson/atterm/internal/appdir"
	internalfeishu "github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/ptyhost"
	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

// relayHost wires an internal/relay.Server to in-process PTYs spawned by the
// desktop app. It listens on 127.0.0.1:<random> and is the only WebSocket
// endpoint the desktop frontend talks to.
type relayHost struct {
	addr         string
	sessionToken string
	adminUserID  string // owner ULID for sessions adopted into the mini-relay
	server       *relay.Server
	httpSrv      *http.Server
	store        userstore.Store    // closed on Stop()
	sqliteStore  *userstore.DBStore // concrete type for Feishu binding lookup

	hostID string
	host   string
	user   string

	cfg *configStore

	// forkCtx is the app's own lifetime, not any one caller's. Set once by
	// app.go's startup() (SetForkContext) right after a.host is assigned;
	// nil in tests that build a relayHost directly, where
	// SessionForkContext falls back to context.Background(). It exists so a
	// session forked on behalf of a request that arrived over the network
	// (TypeSessionCreate — session_create_handler.go) can outlive the one
	// uplink connection that happened to carry that request: a local tab
	// already gets this by construction (App.NewSession passes a.ctx
	// directly), but the uplink's own connCtx is cancelled by runOnce on
	// every reconnect, which is a routine event for that link, not a reason
	// to kill a PTY the user is actively using.
	forkCtx context.Context

	mu       sync.Mutex
	sessions map[uuid.UUID]*activeSession
	changes  chan struct{} // capacity 1; signals "session set has changed"
	// uplinkSubs remembers, for each session id, the local Subscriber created
	// in SubscribeLocal. ClaimLocalDriver looks the sub up here when a remote
	// attacher (behind the uplink) sends CLAIM_DRIVER and we need to promote
	// that subscription to driver on the local session.
	uplinkSubs map[uuid.UUID]*session.Subscriber

	// startSniffFn launches an AI session-id resolution goroutine. Defaults
	// to startAIResolve in production; tests override with a stub.
	startSniffFn func(ctx context.Context, sess *session.Session, cwd, kind string, onCapture func(sid string))

	// aiSidCallback is set by app.go after startRelayHost returns; it
	// receives AI recovery projection updates and emits a Wails event. An empty
	// aiSid invalidates the previous generation before the new resolver has a
	// precise id. Nil when no app is wired (tests / standalone).
	aiSidCallback func(localSessionID uuid.UUID, kind, aiSid string)

	// FeishuHookEndpoint is set by app.go at startup once the HookServer
	// has bound a port. Empty when feishu is disabled or not yet started.
	FeishuHookEndpoint string

	// feishuDispatcher is set by app.go and swapped at runtime when the relay
	// login state changes (relay/local mode switch). Atomic because task-state
	// callbacks read it from session goroutines while app.go may replace it.
	// nil → no-op.
	feishuDispatcher atomic.Pointer[feishu.Dispatcher]

	// feishuRemoteTermState reports the remote-terminal gate state for the live
	// Feishu mode. Injected by app.go so the guard does not bind to a concrete
	// store. ok=false means "no binding / not ready" → skip auto-attach.
	feishuRemoteTermState func(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool)

	// feishuCards is the in-process registry of live anchor cards keyed by
	// atterm session ID string. Guarded indirectly by the CardIndex's own
	// RWMutex; initialized lazily but always non-nil after startRelayHost.
	feishuCards *internalfeishu.CardIndex

	// feishuSubsMu guards feishuSessions.
	feishuSubsMu sync.Mutex
	// feishuSessions is the per-session Feishu record store, keyed by session
	// ID string. Each entry may carry any subset of: an attached
	// FeishuSubscriber (sub), the live status/anchor state (anchor), and a
	// lazyAttachInFlight flag that collapses concurrent attach attempts.
	// Previously three parallel maps; consolidated so attach / detach /
	// concurrent-attach guards operate on a single map lookup.
	feishuSessions map[string]*feishuSession
}

// feishuSession holds the per-session Feishu state. A record may exist with
// only some fields populated: the anchor is set before the subscriber goes
// live (attach in progress), sub is set after AttachFeishuSubscriber
// returns, and lazyAttachInFlight is true only during the attach handshake
// so a burst of concurrent SetOnTaskStateChange / OnTurnMissingChunker
// events collapses into a single attach goroutine.
type feishuSession struct {
	sub                *internalfeishu.FeishuSubscriber
	anchor             *anchorRuntime
	lazyAttachInFlight bool
}

// anchorRuntime is the per-session live state the anchor card's status
// preamble depends on: task state, elapsed time, and the last inner body we
// flushed (so a status-only refresh can re-emit without losing the AI body).
type anchorRuntime struct {
	createdAt time.Time
	taskState atomic.Value // string (proto.TaskState*)
	lastInner atomic.Value // string
	render    func()       // re-build wrapper from current state + re-PATCH
}

// sessionPTY is the minimal contract activeSession needs from whatever backs
// a session — a local PTY (*ptyhost.Host) or an SSH remote shell
// (*sshclient.Session, via sshPtyHost). Both satisfy it. Resize's signature
// matches *ptyhost.Host so the local path is unchanged.
type sessionPTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
}

type activeSession struct {
	host     sessionPTY
	cleanup  func()
	restored bool // true when NewSession was invoked with AIKind set (recovery path)
}

// startRelayHost opens the mini-relay's userstore, bootstraps a desktop-local
// admin (creating the user on first launch, generating LocalAdminPassword if
// the persisted config doesn't have one yet), mints a session token for that
// user, and starts the loopback HTTP server gated by session-token auth.
//
// cfgStore must be non-nil — the bootstrap password lives in it. Tests can
// pass an empty &configStore{}; on first call we generate the password and
// persist it (which writes to UserConfigDir, isolated by the standard test
// HOME/XDG overrides).
func startRelayHost(cfgStore *configStore) (*relayHost, error) {
	if cfgStore == nil {
		return nil, fmt.Errorf("startRelayHost: cfgStore is nil")
	}
	ctx := context.Background()

	// Persist the bootstrap password the first time we run on this machine.
	// It is NOT cryptographically secret from the user (it lives in
	// ~/.config/atterm/config.json), but it is the only thing tying this
	// process to the on-disk users.db, so it must be stable across launches.
	cfg := cfgStore.Get()
	if cfg.LocalAdminPassword == "" {
		pw := randomPassword(32)
		if pw == "" {
			return nil, fmt.Errorf("generate local admin password: rand failed")
		}
		cfg.LocalAdminPassword = pw
		if err := cfgStore.Set(cfg); err != nil {
			return nil, fmt.Errorf("persist local admin password: %w", err)
		}
	}

	dbPath, err := localUserStorePath()
	if err != nil {
		return nil, fmt.Errorf("locate local userstore: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create userstore dir: %w", err)
	}
	store, err := userstore.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open userstore at %s: %w", dbPath, err)
	}

	tok, adminUser, err := bootstrapLocalAdmin(ctx, store, localAdminEmail, cfg.LocalAdminPassword)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap local admin: %w", err)
	}

	srv := relay.NewServer(relay.Config{
		Version:      Version,
		Debug:        relayDebugEnabled(),
		DebugPayload: relayDebugPayloadEnabled(),
		// Loopback-only listener already constrains who can reach us; allow
		// any origin so the webview's wails:// scheme and Vite dev's
		// http://localhost:* both pass the WS upgrade check.
		AllowedOrigins: nil, // nil enables InsecureSkipVerify in acceptOptions
		// Store + Resolver wire requireSession in the relay server — every
		// /api/* and WS request now validates the session token against the
		// local users.db. The desktop frontend bears tok via Bearer header /
		// atterm-token subprotocol like any other relay client.
		Store:    store,
		Resolver: relay.NewIdentityResolver(store),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	// CORS is handled inside internal/relay.Server.ServeHTTP — no wrapper here.
	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logError("relay-host", "local relay stopped serving: %v", err)
		}
	}()
	return &relayHost{
		addr:           ln.Addr().String(),
		sessionToken:   tok,
		adminUserID:    adminUser.ID,
		server:         srv,
		httpSrv:        httpSrv,
		store:          store,
		sqliteStore:    store,
		hostID:         appdir.HostID(),
		host:           hostnameOrUnknown(),
		user:           usernameOrUid(),
		cfg:            cfgStore,
		sessions:       make(map[uuid.UUID]*activeSession),
		changes:        make(chan struct{}, 1),
		uplinkSubs:     make(map[uuid.UUID]*session.Subscriber),
		startSniffFn:   startAIResolve,
		feishuCards:    internalfeishu.NewCardIndex(),
		feishuSessions: make(map[string]*feishuSession),
	}, nil
}

// localUserStorePath returns the absolute path to the desktop's local
// users.db. It lives next to config.json under UserConfigDir/atterm so
// the same XDG_CONFIG_HOME / HOME test overrides apply.
func localUserStorePath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "users.db"), nil
}

func relayDebugEnabled() bool {
	return envEnabled("ATTERM_RELAY_DEBUG") || relayDebugPayloadEnabled()
}

func relayDebugPayloadEnabled() bool {
	return envEnabled("ATTERM_RELAY_DEBUG_PAYLOAD") || envEnabled("ATTERM_RELAY_DEBUG_PAYLOADS")
}

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// notifyChange marks the session set dirty for any uplink watching it.
// Saturating semantics: if a notification is already pending, drop this one.
func (h *relayHost) notifyChange() {
	select {
	case h.changes <- struct{}{}:
	default:
	}
}

// WatchChanges returns a channel that receives whenever a session is added or
// removed. At most one pending notification is queued at any time.
func (h *relayHost) WatchChanges() <-chan struct{} { return h.changes }

// Snapshot returns a slice of SessionInfo for all currently-live sessions.
// Used by the uplink to build ANNOUNCE payloads. Uses ForEach to build the
// SessionInfo slice in a single pass (previously it copied *Session pointers
// via List() and then mapped to SessionInfo, allocating twice per announce).
func (h *relayHost) Snapshot() []proto.SessionInfo {
	var out []proto.SessionInfo
	h.server.Registry().ForEach(func(s *session.Session) bool {
		out = append(out, s.Info())
		return true
	})
	return out
}

// HostMeta returns the (host_id, host, user) triple identifying this machine.
func (h *relayHost) HostMeta() (hostID, host, user string) {
	return h.hostID, h.host, h.user
}

// SetForkContext installs the app-lifetime context that governs sessions
// forked on behalf of a network-originated request (see the forkCtx field
// doc comment). Called once, by app.go's startup(), after a.host is
// assigned and a.ctx is known.
//
// The write is unlocked, and that is safe only because of where it happens:
// startup() writes it before the uplink goroutine that reads it exists, so
// goroutine creation supplies the happens-before edge. A second caller, or
// a call after the uplink is running, turns this into a data race — add a
// mutex before adding one.
func (h *relayHost) SetForkContext(ctx context.Context) {
	h.forkCtx = ctx
}

// SessionForkContext returns the context that should own a session's OS
// process when the request that created it did not come from something
// physically at this keyboard — currently only TypeSessionCreate
// (session_create_handler.go). Falls back to context.Background() when
// SetForkContext was never called, which is every relayHost a test builds
// directly: those processes exit with the test binary regardless, so a
// context that never cancels is the correct (and only sensible) default
// there.
func (h *relayHost) SessionForkContext() context.Context {
	if h.forkCtx != nil {
		return h.forkCtx
	}
	return context.Background()
}

// SubscribeLocal returns a Subscriber for the local session with the given id.
// Used by the uplink when the remote relay asks it to start streaming.
func (h *relayHost) SubscribeLocal(id uuid.UUID, sinceSeq uint64) (*session.Subscriber, uint64, error) {
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return nil, 0, fmt.Errorf("no such local session %s", id)
	}
	uplinkSubClientID := "uplink:" + uuid.New().String()
	// Pseudo-name for the uplink subscriber; the real per-remote-client name
	// gets propagated via CLAIM_DRIVER end-to-end when a remote claims.
	uplinkSubClientName := "remote"
	// The uplink subscriber is a passive fan-out for remote viewers, not an
	// interactive client. It must never auto-promote to driver: a stream
	// restart (StreamStop→StreamRequest when an idle remote client reconnects)
	// recreates this sub, and auto-promotion would seize the driver role as the
	// placeholder "remote", spuriously kicking the real remote driver back to a
	// viewer ("remote has taken control" with nobody actually there). A remote
	// becomes driver only via an explicit end-to-end CLAIM_DRIVER → ClaimLocalDriver.
	sub, replayToSeq := sess.Subscribe(sinceSeq, uplinkSubClientID, uplinkSubClientName, session.WithoutAutoDrive())
	h.mu.Lock()
	h.uplinkSubs[id] = sub
	h.mu.Unlock()
	info := sess.Info()
	logDebug("uplink", "subscribe_local_ok session=%s since_seq=%d replay_to_seq=%d cols=%d rows=%d client_id=%q", id, sinceSeq, replayToSeq, info.Cols, info.Rows, uplinkSubClientID)
	return sub, replayToSeq, nil
}

// UnsubscribeLocal removes a previously-acquired subscriber.
func (h *relayHost) UnsubscribeLocal(id uuid.UUID, sub *session.Subscriber) {
	if sess, ok := h.server.Registry().Get(id); ok {
		sess.Unsubscribe(sub)
	}
	h.mu.Lock()
	if h.uplinkSubs[id] == sub {
		delete(h.uplinkSubs, id)
	}
	h.mu.Unlock()
}

// ClaimLocalDriver promotes the uplink's own local-session subscriber to
// driver for the given session, attributing the end-to-end client_id and
// client_name. Called by uplink when a remote subscriber on the public relay
// sends CLAIM_DRIVER.
func (h *relayHost) ClaimLocalDriver(id uuid.UUID, clientID, clientName string) error {
	h.mu.Lock()
	uplinkSub := h.uplinkSubs[id]
	h.mu.Unlock()
	if uplinkSub == nil {
		return fmt.Errorf("no uplink subscriber for session %s", id)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return fmt.Errorf("no local session %s", id)
	}
	sess.ClaimDriver(uplinkSub, clientID, clientName)
	return nil
}

func (h *relayHost) DriverClientID(id uuid.UUID) (string, bool) {
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return "", false
	}
	return sess.DriverClientID(), true
}

// SendLocalInbound forwards an IN/RESIZE frame from a remote attacher into the
// local session's inbound queue, where the AdoptSession goroutine routes it to
// the PTY.
func (h *relayHost) SendLocalInbound(id uuid.UUID, f proto.Frame) error {
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return fmt.Errorf("no such local session %s", id)
	}
	if !sess.SendInbound(f) {
		return fmt.Errorf("local inbound full")
	}
	return nil
}

// Inject writes text into a local session's PTY by sending it as a TypeIn
// frame down the same path remote-viewer keystrokes use.
func (h *relayHost) Inject(id uuid.UUID, text string) error {
	return h.SendLocalInbound(id, proto.Frame{
		Type:      proto.TypeIn,
		SessionID: id,
		Payload:   []byte(text),
	})
}

// RequestLocalRepaint nudges a full-screen terminal app to redraw after a
// remote attach receives only a truncated alternate-screen replay. Many TUIs
// repaint on SIGWINCH, which is the only reliable signal available outside
// the PTY byte stream.
func (h *relayHost) RequestLocalRepaint(id uuid.UUID) {
	h.mu.Lock()
	active := h.sessions[id]
	h.mu.Unlock()
	if active == nil || active.host == nil {
		logDebug("repaint", "skip session=%s reason=no_active_host", id)
		return
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		logDebug("repaint", "skip session=%s reason=no_session", id)
		return
	}
	info := sess.Info()
	if info.Cols < 2 || info.Rows < 2 {
		logDebug("repaint", "skip session=%s reason=invalid_size cols=%d rows=%d", id, info.Cols, info.Rows)
		return
	}
	logDebug("repaint", "nudge_start session=%s cols=%d rows=%d", id, info.Cols, info.Rows)
	go func(cols, rows uint16) {
		if err := active.host.Resize(cols, rows-1); err != nil {
			logWarn("repaint", "nudge_failed session=%s step=shrink cols=%d rows=%d error=%v", id, cols, rows-1, err)
			return
		}
		time.Sleep(25 * time.Millisecond)
		if err := active.host.Resize(cols, rows); err != nil {
			logWarn("repaint", "nudge_failed session=%s step=restore cols=%d rows=%d error=%v", id, cols, rows, err)
			return
		}
		logDebug("repaint", "nudge_ok session=%s cols=%d rows=%d", id, cols, rows)
	}(info.Cols, info.Rows)
}

// Exists reports whether a session with the given ID is currently active in
// this relay host. Satisfies feishu.SessionLookup.
func (h *relayHost) Exists(id uuid.UUID) bool {
	h.mu.Lock()
	_, ok := h.sessions[id]
	h.mu.Unlock()
	return ok
}

// CloseSession terminates the PTY for a session and synchronously evicts
// it from the local registry, so the uplink learns NOW (rather than after
// the eventual pty.Wait() in the watcher goroutine) and the upstream relay
// drops the mirror promptly. Without this, the close-to-uplink-ANNOUNCE
// delay was bounded by how long zsh took to notice EOF and exit — which
// for shells in the middle of a foreground command can be arbitrary.
func (h *relayHost) CloseSession(id uuid.UUID) error {
	h.mu.Lock()
	s, ok := h.sessions[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("no such session")
	}
	err := s.host.Close()
	// AdoptSession's cleanup is sync.Once-guarded; calling it here is safe
	// even if the watcher goroutine also reaches it later.
	s.cleanup()
	// Detach the Feishu subscriber for this session (idempotent; the PTY
	// watcher goroutine will also call this after pty.Wait() returns).
	h.detachFeishuSubscriber(id)
	h.notifyChange()
	return err
}

// Stop tears down all live PTYs, shuts down the HTTP server, and closes the
// backing userstore. Idempotent.
func (h *relayHost) Stop() {
	h.mu.Lock()
	sessions := h.sessions
	h.sessions = nil
	h.mu.Unlock()
	for _, s := range sessions {
		s.cleanup()
		_ = s.host.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = h.httpSrv.Shutdown(ctx)
	if h.store != nil {
		_ = h.store.Close()
		h.store = nil
	}
}

// resolveSessionProfile implements design §4's precedence for choosing a
// profile at session-creation time: an explicitly chosen profile id wins,
// then the configured default, then no profile at all (nil) — the caller
// then falls back to whatever req.Command/req.Cwd already carry, i.e.
// today's default_shell + HOME behavior, unchanged.
//
// Returns nil rather than an error when the resolved id (explicit or
// default) does not match any profile: a stale or deleted profile id must
// never block session creation, it just means "no profile applies".
func resolveSessionProfile(profiles []SessionProfile, defaultProfileID, explicitProfileID string) *SessionProfile {
	id := explicitProfileID
	if id == "" {
		id = defaultProfileID
	}
	if id == "" {
		return nil
	}
	for i := range profiles {
		if profiles[i].ID == id {
			p := profiles[i]
			return &p
		}
	}
	return nil
}

// applyProfileEnv merges a profile's environment variables onto the base
// terminal env. Profile entries win over the base environment (design §4
// precedence) — with one deliberate exception: TERM. relay_host builds its
// base env via terminalEnvForXterm, which sets TERM=xterm-256color to match
// the xterm.js renderer atterm actually talks to; a profile env that
// happened to carry TERM (e.g. copied from a dotfile) would silently break
// rendering if it were allowed to win (design §6.3). Every other key is the
// user's to override, TERM is not.
func applyProfileEnv(env []string, profileEnv map[string]string) []string {
	for k, v := range profileEnv {
		if k == envKeyTerm {
			continue
		}
		env = setEnv(env, k, v)
	}
	return env
}

// NewSession spawns a PTY for the given command and adopts it as a session.
func (h *relayHost) NewSession(ctx context.Context, req NewSessionReq) (uuid.UUID, error) {
	var profiles []SessionProfile
	var defaultProfileID string
	if h.cfg != nil {
		c := h.cfg.Get()
		profiles = c.Profiles
		defaultProfileID = c.DefaultProfileID
	}
	profile := resolveSessionProfile(profiles, defaultProfileID, req.ProfileID)
	profileID := ""
	if profile != nil {
		profileID = profile.ID
	}

	// Precedence (design §4): the resolved profile's Shell/Cwd win when set;
	// an empty Shell/Cwd on the profile means "fall back to what the caller
	// already resolved" (req.Command from default_shell, req.Cwd/HOME) —
	// exactly today's behavior when no profile applies at all.
	//
	// Shell and Cwd get deliberately asymmetric treatment for a synced
	// profile whose Shell/Cwd don't exist on this machine (final-review
	// ruling, Important 3):
	//
	//   - Shell gets a fallback. A shell that cannot start means no session
	//     at all, so degrading to the caller's already-resolved default
	//     shell (req.Command) is strictly better than failing outright — and
	//     a bad *default* profile would otherwise fail every new tab and
	//     split. Mirrors DefaultShellOrDefault's treatment of a synced
	//     absolute default_shell (config.go): stat only absolute paths (a
	//     bare name is PATH-resolved at spawn time, so statting it here would
	//     reject perfectly good configs) and fall back with a logWarn.
	//   - Cwd does NOT get a fallback. A wrong cwd still opens a working
	//     session — just in the wrong directory, which the user may not
	//     notice and may run commands in unintentionally. Failing loudly
	//     (see the ptyhost.Open wrap below, which names the profile) is
	//     better than silently landing somewhere the user didn't choose.
	command := req.Command
	if profile != nil && profile.Shell != "" {
		shell := profile.Shell
		if filepath.IsAbs(shell) {
			if _, err := os.Stat(shell); err != nil {
				logWarn("session", "profile %q (%s) shell %q is not present on this machine; falling back to the default shell", profile.Name, profile.ID, shell)
				shell = ""
			}
		}
		if shell != "" {
			command = shell
		}
	}
	if command == "" {
		return uuid.Nil, fmt.Errorf("empty command")
	}
	cwd := req.Cwd
	if profile != nil && profile.Cwd != "" {
		cwd = profile.Cwd
	}
	if cwd == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = home
		}
	}
	cols, rows := req.Cols, req.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	argv := append([]string{command}, defaultShellArgs(command, req.Args)...)
	logInfo("session", "command=%q args=%v cwd=%q aiKind=%q profileID=%q -> argv=%v",
		command, req.Args, cwd, req.AIKind, profileID, argv)
	env := terminalEnvForXterm(os.Environ())
	if profile != nil {
		env = applyProfileEnv(env, profile.Env)
	}

	enabled := true
	if h.cfg != nil {
		enabled = h.cfg.Get().ShellIntegrationEnabledOrDefault()
	}
	sid := uuid.New() // generated here so the plan can scope temp files by id
	plan := shellintegration.Prepare(command, enabled, sid.String())
	argv, env = mergeShellIntegrationPlan(argv, env, plan)
	env = appendFeishuHookEnv(env, sid.String(), h.FeishuHookEndpoint)
	if plan.Shell != "" {
		logInfo("shell-integration", "enabled session=%s shell=%s", sid, plan.Shell)
	}

	pty, err := ptyhost.Open(ctx, ptyhost.Config{
		Argv: argv,
		Env:  env,
		Cwd:  cwd,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		if plan.Cleanup != nil {
			plan.Cleanup()
		}
		// Name the profile when one applied — most likely to fire here is a
		// synced cwd that doesn't exist on this machine (Shell already has a
		// fallback above; Cwd deliberately does not). Without this, the user
		// gets a bare "chdir: no such file or directory" with nothing tying
		// it to a profile they configured on another machine.
		if profile != nil {
			return uuid.Nil, fmt.Errorf("open pty for profile %q (%s): %w", profile.Name, profile.ID, err)
		}
		return uuid.Nil, fmt.Errorf("open pty: %w", err)
	}

	id := sid
	info := proto.SessionInfo{
		Command:   strings.Join(argv, " "),
		Cwd:       cwd,
		Title:     command,
		Cols:      cols,
		Rows:      rows,
		HostID:    h.hostID,
		Host:      h.host,
		User:      h.user,
		StartedAt: time.Now().Unix(),
	}

	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty, cfg: h.cfg}, h.adminUserID)

	// resolveCtx bounds every AI id resolver to the PTY lifetime. A pane may
	// launch Claude, exit, then launch Codex (or a second Codex); each launch
	// below creates a new generation and cancels the previous resolver.
	resolveCtx, resolveCancel := context.WithCancel(ctx)
	var resolveMu sync.Mutex
	var resolveGeneration uint64
	var currentResolveCancel context.CancelFunc
	var restoredStartKind string

	startResolveGeneration := func(sess *session.Session, kind, resolveCwd, initialSID string, awaitRestoredStart bool) {
		resolveMu.Lock()
		if currentResolveCancel != nil {
			currentResolveCancel()
		}
		resolveGeneration++
		generation := resolveGeneration
		generationCtx, generationCancel := context.WithCancel(resolveCtx)
		currentResolveCancel = generationCancel
		if awaitRestoredStart {
			restoredStartKind = kind
		} else {
			restoredStartKind = ""
		}
		resolveMu.Unlock()

		// SetOnAIClassified runs while session.mu is held, so event publication
		// and resolver work must stay off that callback stack. This goroutine
		// publishes the new projection before starting resolution, guaranteeing
		// that an immediate capture cannot overtake the clear/initial-SID event.
		go func() {
			resolveMu.Lock()
			if generation != resolveGeneration {
				resolveMu.Unlock()
				return
			}
			// Empty means "latest generation is unresolved"; the frontend
			// removes the old recovery entry instead of resuming it.
			h.onAISidCaptured(id, kind, initialSID)
			resolveMu.Unlock()

			if h.startSniffFn == nil {
				return
			}
			logDebug("ai-sid", "classified session=%s kind=%s generation=%d — start resolve", id, kind, generation)
			h.startSniffFn(generationCtx, sess, resolveCwd, kind, func(aiSid string) {
				resolveMu.Lock()
				if generation == resolveGeneration {
					// Keep validation + publication in one critical section. Otherwise
					// a new generation could publish its clearing event after this
					// check, then this stale callback could overwrite it with an old SID.
					h.onAISidCaptured(id, kind, aiSid)
				}
				resolveMu.Unlock()
			})
		}()
		if kind == "codex" && initialSID != "" {
			go startCodexKnownTitleResolve(generationCtx, sess, resolveCwd, initialSID)
		}
	}

	var cleanupOnce sync.Once
	combinedCleanup := func() {
		cleanupOnce.Do(func() {
			resolveMu.Lock()
			if currentResolveCancel != nil {
				currentResolveCancel()
			}
			resolveGeneration++
			resolveMu.Unlock()
			resolveCancel()
			cleanup()
			if plan.Cleanup != nil {
				plan.Cleanup()
			}
		})
	}

	// Every top-level AI OSC 133 C starts a new resolver generation. The only
	// exception is the first same-kind command injected for a restored pane: it
	// confirms the generation already established below instead of clearing the
	// known initial SID and starting a duplicate resolver.
	if sess, ok := h.server.Registry().Get(id); ok {
		sess.SetOnAIClassified(func(commandLine, cwd string) {
			kind := classifyAIKindFromCommand(commandLine)
			if kind == "" {
				return
			}
			resolveMu.Lock()
			confirmedRestoredStart := restoredStartKind != "" && restoredStartKind == kind
			restoredStartKind = ""
			resolveMu.Unlock()
			if !confirmedRestoredStart {
				startResolveGeneration(sess, kind, cwd, "", false)
			}
			// autoAttach="ai": attach the Feishu subscriber now that we know
			// this is an AI session. Use background context (not the session's
			// resolveCtx) so the anchor survives beyond the sniff lifetime.
			go h.attachFeishuSubscriberForAutoAttach(context.Background(), sess, id, "ai")
		})
		sess.SetOnTaskStateChange(func(sid uuid.UUID, prev, next string, meta session.TaskMeta) {
			disp := h.feishuDispatcher.Load()
			if disp == nil {
				return
			}
			// Lazy backfill: if the user toggled remote-terminal on after
			// this AI session started, no subscriber was registered at
			// NewSession time. Piggyback on the next task-state change to
			// attach the anchor now. Idempotent (gate + attach's own
			// double-attach guard) and non-blocking (goroutine inside).
			h.lazyAttachIfMissing(context.Background(), sess, sid)
			// Anchor status preamble: update the per-session runtime so the
			// next render() shows the new label. Done synchronously (no
			// session.mu re-entry — only touches atomic.Value), then a
			// goroutine re-PATCHes to avoid blocking the session callback.
			h.feishuSubsMu.Lock()
			var rt *anchorRuntime
			if fs := h.feishuSessions[sid.String()]; fs != nil {
				rt = fs.anchor
			}
			h.feishuSubsMu.Unlock()
			if rt != nil {
				rt.taskState.Store(next)
				go rt.render()
			}
			// IMPORTANT: this callback runs while session.mu is held (see
			// fireTaskStateLocked). Calling sess.Info() / sess.TailOutput()
			// here would re-acquire that lock and deadlock the session
			// goroutine — which stops its inbound pump, so keystrokes stop
			// reaching the PTY (terminal appears frozen). Defer every call
			// that touches the session lock to the goroutine below, which
			// runs after the lock is released.
			switch next {
			case proto.TaskStateCompleted, proto.TaskStateFailed:
				// meta.RecentOutput is the command summary computed at OSC 133;D
				// (already ANSI-stripped + line-limited, empty for E2EE sessions).
				tail := meta.RecentOutput
				go func() {
					info := sess.Info()
					if !shouldNotifySession(info.Type, h.cfg.Get().AINotificationsOnlyOrDefault()) {
						return
					}
					disp.DispatchCommandFinished(context.Background(),
						feishu.CommandFinishedEvent{
							SessionID:    sid,
							ExitCode:     meta.ExitCode,
							ElapsedMS:    meta.ElapsedMS,
							Label:        meta.Label,
							SealedBody:   meta.SealedBody,
							SessionTitle: info.Title,
							Cwd:          info.Cwd,
							OutputTail:   tail,
						})
				}()
			case proto.TaskStateWaitingInput:
				sealed := len(meta.SealedBody) != 0
				go func() {
					// Run outside the lock-held callback (see deadlock note above).
					info := sess.Info()
					if !shouldNotifySession(info.Type, h.cfg.Get().AINotificationsOnlyOrDefault()) {
						return
					}
					var recent string
					if !sealed {
						recent = string(session.StripANSI(sess.TailOutput(512)))
					}
					disp.DispatchWaitingInput(context.Background(),
						feishu.WaitingInputDispatchEvent{
							SessionID:      sid,
							Source:         feishu.WaitingSourceHeuristic,
							SessionTitle:   info.Title,
							Cwd:            info.Cwd,
							CurrentCommand: info.CurrentCommand,
							RecentOutput:   recent,
						})
				}()
			}
		})
	}

	// Restored AI session: req.AIKind is known up front (the pane was AI
	// before the crash). Two things:
	//  1. If we have a precise resume id, inject `claude --resume <id>` once the
	//     shell draws its first prompt (Go-side, written straight to the PTY —
	//     reliable, no frontend task-state dependency which never fires for a
	//     plain shell prompt).
	//  2. Kick id resolution to re-capture the id for the NEXT crash (after
	//     resume, claude appends to the same jsonl so the title match re-resolves
	//     the same id).
	// resumeClaimed tracks whether the block below actually registered a
	// SetOnFirstPrompt callback for AI-session resume. It is NOT the same as
	// req.AIKind != "": a freshly classified AI command (user just typed
	// "claude", no crash involved) has AIKind set but no
	// InitialAISessionID, so computeResumeArgs returns nil and nothing
	// claims the callback. Gating the profile-startup block on
	// req.AIKind == "" would then skip it on that path too, even though the
	// slot is free — and since a default profile applies to every new
	// session regardless of what was typed, that's not a corner case.
	resumeClaimed := false
	if req.AIKind != "" {
		if sess, ok := h.server.Registry().Get(id); ok {
			sidCopy := id
			resumeArgv := computeResumeArgs(req.AIKind, req.InitialAISessionID, req.InitialAICommandLine)
			startResolveGeneration(sess, req.AIKind, cwd, req.InitialAISessionID, resumeArgv != nil)
			if resumeArgv != nil {
				resumeClaimed = true
				line := strings.Join(resumeArgv, " ") + "\n"
				ptyCopy := pty
				sess.SetOnFirstPrompt(func() {
					logInfo("recovery", "restored ai session=%s — inject resume %q", sidCopy, strings.TrimSpace(line))
					go func() { _, _ = ptyCopy.Write([]byte(line)) }()
				})
			}
		}
	}

	// Profile startup command: written straight to the PTY once the shell
	// draws its first prompt, reusing the exact SetOnFirstPrompt mechanism
	// the AI-resume injection above uses — never a frontend sendInput of
	// "<cmd>\r". Codex reads a trailing CR as a paste, not a submitted
	// command; this repo has made and re-fixed that exact mistake three
	// times (PR #63 → #110 → #129, AGENTS.md redline #28), so StartupCmd
	// must not open a second "inject text into the PTY" implementation.
	//
	// Skipped when resumeClaimed: SetOnFirstPrompt is a single callback slot
	// (session.Session), and the restore block above claims it only when it
	// actually has a resume command to inject. A profile-selected new
	// session is never itself a genuine AI-restore session, so the two never
	// legitimately compete — but "genuine restore" means resumeClaimed, not
	// merely req.AIKind != "".
	if profile != nil && profile.StartupCmd != "" && !resumeClaimed {
		if sess, ok := h.server.Registry().Get(id); ok {
			line := profile.StartupCmd + "\n"
			ptyCopy := pty
			sidCopy := id
			sess.SetOnFirstPrompt(func() {
				// StartupCmd is user-authored and plausibly carries a secret
				// (e.g. "export GH_TOKEN=…"). Logs are bundled by
				// ExportDiagnostics and users share those bundles, so log its
				// length rather than its contents — enough to confirm
				// something was injected without leaking what.
				logInfo("profile", "session=%s profile=%s — injecting startup command (%d bytes)", sidCopy, profileID, len(profile.StartupCmd))
				go func() { _, _ = ptyCopy.Write([]byte(line)) }()
			})
		}
	}

	h.mu.Lock()
	if h.sessions == nil {
		h.mu.Unlock()
		combinedCleanup()
		_ = pty.Close()
		return uuid.Nil, fmt.Errorf("relay host stopped")
	}
	h.sessions[id] = &activeSession{host: pty, cleanup: combinedCleanup, restored: req.AIKind != ""}
	h.mu.Unlock()
	h.notifyChange()

	// autoAttach="all": attach the Feishu subscriber immediately for every
	// new session regardless of session type. Run in a goroutine so Feishu
	// I/O never blocks PTY creation.
	if sess, ok := h.server.Registry().Get(id); ok {
		go h.attachFeishuSubscriberForAutoAttach(ctx, sess, id, "all")
	}

	done := make(chan struct{})
	go h.watchCwd(id, pty, cwd, done)

	go func() {
		_ = pty.Wait()
		close(done)
		combinedCleanup()
		_ = pty.Close()
		h.mu.Lock()
		delete(h.sessions, id)
		h.mu.Unlock()
		// Detach and archive the Feishu anchor card (if any). Must run after
		// combinedCleanup so the subscriber's drain loop is stopped first.
		h.detachFeishuSubscriber(id)
		h.notifyChange()
	}()

	return id, nil
}

// watchCwd polls the child's /proc-reported cwd once a second and broadcasts
// a META frame whenever it changes. The local mini-relay fans the META out
// to attached clients and notifies the uplink so remote relays only receive
// an ANNOUNCE when the advertised snapshot actually changed.
func (h *relayHost) watchCwd(id uuid.UUID, pty *ptyhost.Host, initial string, done <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	last := initial
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		cwd := pty.Cwd()
		if cwd == "" || cwd == last {
			continue
		}
		last = cwd
		sess, ok := h.server.Registry().Get(id)
		if !ok {
			return
		}
		// UpdateMeta broadcasts a META frame internally with the full session
		// state (including driver fields) — don't construct a "lite" META
		// here, doing so clobbers driver_client_id and makes clients render
		// the viewer overlay on every cwd change.
		sess.UpdateMeta(proto.MetaPayload{Cwd: cwd})
		h.server.Registry().NotifyChange()
		h.notifyChange()
	}
}

func hostnameOrUnknown() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}

func usernameOrUid() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return fmt.Sprintf("uid%d", os.Getuid())
}

// defaultShellArgs decides whether to start the user's shell as a login
// shell when the frontend didn't specify any args. macOS GUI processes
// inherit a minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin); only login
// shells run /etc/zprofile, where path_helper extends PATH with
// /etc/paths and /etc/paths.d/* (where Docker Desktop and Homebrew
// install their binaries). Without -l, "docker", "brew", etc. are
// command-not-found inside atterm, even though they work in Terminal.app.
// Match Terminal.app's default and pass -l for zsh/bash/fish. If the
// caller already provided args, respect them — they know what they want.
func defaultShellArgs(command string, args []string) []string {
	if len(args) > 0 {
		return args
	}
	switch filepath.Base(command) {
	case "zsh", "bash", "fish":
		return []string{"-l"}
	}
	return args
}

// mergeShellIntegrationPlan returns (argv', env') with the plan's args
// appended after argv[0] and its env appended after base. Zero plans are
// the identity transform.
func mergeShellIntegrationPlan(argv, env []string, p shellintegration.Plan) ([]string, []string) {
	if len(p.ExtraArgs) == 0 && len(p.ExtraEnv) == 0 {
		return argv, env
	}
	outArgv := append([]string{}, argv...)
	if len(p.ExtraArgs) > 0 {
		outArgv = append(outArgv, p.ExtraArgs...)
	}
	outEnv := append([]string{}, env...)
	if len(p.ExtraEnv) > 0 {
		outEnv = append(outEnv, p.ExtraEnv...)
	}
	return outArgv, outEnv
}
