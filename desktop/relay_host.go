package main

import (
	"context"
	"fmt"
	"log"
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
	"github.com/attson/atterm/internal/hostid"
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
	store        userstore.Store        // closed on Stop()
	sqliteStore  *userstore.DBStore // concrete type for Feishu binding lookup

	hostID string
	host   string
	user   string

	cfg *configStore

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
	// receives every captured AI session id and emits a Wails event. Nil
	// when no app is wired (tests / standalone).
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

	// feishuSubs and feishuSubsMu guard the set of live FeishuSubscribers,
	// keyed by session ID string. A subscriber is added on attach and removed
	// on detach (session close or manual disconnect).
	feishuSubsMu sync.Mutex
	feishuSubs   map[string]*internalfeishu.FeishuSubscriber

	// feishuAnchorRuntimes carries the live status state (task state + last
	// body) per anchor so SetOnTaskStateChange and the elapsed-time ticker can
	// re-render the status preamble. Keyed by session ID string. Same lock
	// discipline as feishuSubs (parallel map, same lifetime).
	feishuAnchorRuntimes map[string]*anchorRuntime

	// lazyAttachInFlight tracks per-session lazy-backfill attempts kicked
	// off by SetOnTaskStateChange or Dispatcher.SetOnTurnMissingChunker.
	// Collapses a burst of concurrent events on the same session into a
	// single attachFeishuSubscriberForAutoAttach goroutine. Guarded by
	// feishuSubsMu (same map lifetime; keeps lock discipline minimal).
	lazyAttachInFlight map[string]bool
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
			log.Printf("desktop relay: %v", err)
		}
	}()
	return &relayHost{
		addr:                 ln.Addr().String(),
		sessionToken:         tok,
		adminUserID:          adminUser.ID,
		server:               srv,
		httpSrv:              httpSrv,
		store:                store,
		sqliteStore:          store,
		hostID:               hostid.Get(),
		host:                 hostnameOrUnknown(),
		user:                 usernameOrUid(),
		cfg:                  cfgStore,
		sessions:             make(map[uuid.UUID]*activeSession),
		changes:              make(chan struct{}, 1),
		uplinkSubs:           make(map[uuid.UUID]*session.Subscriber),
		startSniffFn:         startAIResolve,
		feishuCards:          internalfeishu.NewCardIndex(),
		feishuSubs:           make(map[string]*internalfeishu.FeishuSubscriber),
		feishuAnchorRuntimes: make(map[string]*anchorRuntime),
		lazyAttachInFlight:   make(map[string]bool),
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
// Used by the uplink to build ANNOUNCE payloads.
func (h *relayHost) Snapshot() []proto.SessionInfo {
	sessions := h.server.Registry().List()
	out := make([]proto.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Info())
	}
	return out
}

// HostMeta returns the (host_id, host, user) triple identifying this machine.
func (h *relayHost) HostMeta() (hostID, host, user string) {
	return h.hostID, h.host, h.user
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
	log.Printf("desktop-uplink: subscribe_local_ok session=%s since_seq=%d replay_to_seq=%d cols=%d rows=%d client_id=%q", id, sinceSeq, replayToSeq, info.Cols, info.Rows, uplinkSubClientID)
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
		log.Printf("desktop-repaint: skip session=%s reason=no_active_host", id)
		return
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		log.Printf("desktop-repaint: skip session=%s reason=no_session", id)
		return
	}
	info := sess.Info()
	if info.Cols < 2 || info.Rows < 2 {
		log.Printf("desktop-repaint: skip session=%s reason=invalid_size cols=%d rows=%d", id, info.Cols, info.Rows)
		return
	}
	log.Printf("desktop-repaint: nudge_start session=%s cols=%d rows=%d", id, info.Cols, info.Rows)
	go func(cols, rows uint16) {
		if err := active.host.Resize(cols, rows-1); err != nil {
			log.Printf("desktop-repaint: nudge_failed session=%s step=shrink cols=%d rows=%d error=%v", id, cols, rows-1, err)
			return
		}
		time.Sleep(25 * time.Millisecond)
		if err := active.host.Resize(cols, rows); err != nil {
			log.Printf("desktop-repaint: nudge_failed session=%s step=restore cols=%d rows=%d error=%v", id, cols, rows, err)
			return
		}
		log.Printf("desktop-repaint: nudge_ok session=%s cols=%d rows=%d", id, cols, rows)
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

// NewSession spawns a PTY for the given command and adopts it as a session.
func (h *relayHost) NewSession(ctx context.Context, req NewSessionReq) (uuid.UUID, error) {
	if req.Command == "" {
		return uuid.Nil, fmt.Errorf("empty command")
	}
	cwd := req.Cwd
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

	argv := append([]string{req.Command}, defaultShellArgs(req.Command, req.Args)...)
	log.Printf("desktop-newsession: command=%q args=%v cwd=%q aiKind=%q -> argv=%v",
		req.Command, req.Args, cwd, req.AIKind, argv)
	env := terminalEnvForXterm(os.Environ())

	enabled := true
	if h.cfg != nil {
		enabled = h.cfg.Get().ShellIntegrationEnabledOrDefault()
	}
	sid := uuid.New() // generated here so the plan can scope temp files by id
	plan := shellintegration.Prepare(req.Command, enabled, sid.String())
	argv, env = mergeShellIntegrationPlan(argv, env, plan)
	env = appendFeishuHookEnv(env, sid.String(), h.FeishuHookEndpoint)
	if plan.Shell != "" {
		log.Printf("desktop-shell-integration: enabled session=%s shell=%s", sid, plan.Shell)
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
		return uuid.Nil, fmt.Errorf("open pty: %w", err)
	}

	id := sid
	info := proto.SessionInfo{
		Command:   strings.Join(argv, " "),
		Cwd:       cwd,
		Title:     req.Command,
		Cols:      cols,
		Rows:      rows,
		HostID:    h.hostID,
		Host:      h.host,
		User:      h.user,
		StartedAt: time.Now().Unix(),
	}

	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty, cfg: h.cfg}, h.adminUserID)

	// resolveCtx bounds the AI id-resolution goroutine to the session's
	// lifetime: it tracks the active conversation continuously (so /resume
	// switching to another conversation re-captures the new id) and must stop
	// when the PTY exits. Cancelled from combinedCleanup.
	resolveCtx, resolveCancel := context.WithCancel(ctx)

	// resolveOnce ensures a single id-resolution goroutine per session: a
	// restored session would otherwise start one here AND again when its
	// injected `claude --resume` re-triggers SetOnAIClassified.
	var resolveOnce sync.Once

	var cleanupOnce sync.Once
	combinedCleanup := func() {
		cleanupOnce.Do(func() {
			resolveCancel()
			cleanup()
			if plan.Cleanup != nil {
				plan.Cleanup()
			}
		})
	}

	// Hook fresh AI sniff: session's OSC 133;C handler fires once when type
	// transitions shell→ai (user types claude/codex/aider at the prompt).
	// Restored sessions skip this path because req.AIKind triggers sniff below.
	if sess, ok := h.server.Registry().Get(id); ok {
		sidCopy := id
		sess.SetOnAIClassified(func(commandLine, cwd string) {
			kind := classifyAIKindFromCommand(commandLine)
			if kind == "" || h.startSniffFn == nil {
				return
			}
			resolveOnce.Do(func() {
				log.Printf("recovery: ai classified session=%s kind=%s — start resolve", sidCopy, kind)
				go h.startSniffFn(resolveCtx, sess, cwd, kind, func(aiSid string) {
					h.onAISidCaptured(sidCopy, kind, aiSid)
				})
			})
			// autoAttach="ai": attach the Feishu subscriber now that we know
			// this is an AI session. Use background context (not the session's
			// resolveCtx) so the anchor survives beyond the sniff lifetime.
			go h.attachFeishuSubscriberForAutoAttach(context.Background(), sess, sidCopy, "ai")
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
			rt := h.feishuAnchorRuntimes[sid.String()]
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
	if req.AIKind != "" {
		if sess, ok := h.server.Registry().Get(id); ok {
			sidCopy := id
			if req.InitialAISessionID != "" {
				h.onAISidCaptured(sidCopy, req.AIKind, req.InitialAISessionID)
				if req.AIKind == "codex" {
					go startCodexKnownTitleResolve(resolveCtx, sess, cwd, req.InitialAISessionID)
				}
			}
			if argv := computeResumeArgs(req.AIKind, req.InitialAISessionID, req.InitialAICommandLine); argv != nil {
				line := strings.Join(argv, " ") + "\n"
				ptyCopy := pty
				sess.SetOnFirstPrompt(func() {
					log.Printf("recovery: restored ai session=%s — inject resume %q", sidCopy, strings.TrimSpace(line))
					go func() { _, _ = ptyCopy.Write([]byte(line)) }()
				})
			}
			if h.startSniffFn != nil {
				resolveOnce.Do(func() {
					log.Printf("recovery: restored ai session=%s kind=%s — start resolve", sidCopy, req.AIKind)
					go h.startSniffFn(resolveCtx, sess, cwd, req.AIKind, func(sid string) {
						h.onAISidCaptured(sidCopy, req.AIKind, sid)
					})
				})
			}
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

