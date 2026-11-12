package main

import (
	"context"
	"errors"
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
	startSniffFn func(ctx context.Context, sess *session.Session, cwd, kind string, rootPID int, onCapture func(sid string))

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

	// promptHistory is independent of Feishu and receives verified Codex user
	// turns from the rollout bound to this PTY. Atomic publication keeps session
	// goroutines race-free while app startup wires the device-local store.
	promptHistory atomic.Pointer[PromptHistoryStore]

	// inputHistoryStore persists each live session's bounded input list so a
	// replacement PTY created by recovery can inherit the previous generation.
	inputHistoryStore *sessionInputHistoryStore

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
	aiChunker          *internalfeishu.AIChunker
	lazyAttachInFlight bool
}

// anchorRuntime is the per-session live state the anchor card's status
// preamble depends on: task state, elapsed time, the last inner body we
// flushed, and when that body last changed (so a status-only refresh can
// re-emit without losing the AI body or moving the last-output clock).
type anchorRuntime struct {
	createdAt    time.Time
	taskState    atomic.Value // string (proto.TaskState*)
	lastInner    atomic.Value // string
	lastOutputAt atomic.Value // time.Time
	aiMode       atomic.Bool  // rejects late shell flushes after an AI upgrade
	patches      *latestAnchorPatchQueue
	flush        internalfeishu.FlushFunc
	render       func() // re-build wrapper from current state + re-PATCH
}

// latestAnchorPatchQueue serializes slow CardKit PATCH calls while retaining
// only the newest body snapshot. Feishu round trips are routinely slower than
// the 100ms output cadence; queueing every intermediate snapshot makes fresh
// output wait behind stale requests and can even apply old bodies late.
type latestAnchorPatchQueue struct {
	mu         sync.Mutex
	apply      func(string)
	running    bool
	active     string
	pending    string
	hasPending bool
	stopped    bool
}

func newLatestAnchorPatchQueue(apply func(string)) *latestAnchorPatchQueue {
	return &latestAnchorPatchQueue{apply: apply}
}

func (q *latestAnchorPatchQueue) Enqueue(body string) {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return
	}
	if q.running {
		if body == q.active {
			q.pending = ""
			q.hasPending = false
		} else {
			q.pending = body
			q.hasPending = true
		}
		q.mu.Unlock()
		return
	}
	q.running = true
	q.active = body
	q.mu.Unlock()
	go q.run(body)
}

func (q *latestAnchorPatchQueue) run(body string) {
	for {
		q.apply(body)
		q.mu.Lock()
		if q.stopped || !q.hasPending {
			q.running = false
			q.active = ""
			q.pending = ""
			q.hasPending = false
			q.mu.Unlock()
			return
		}
		body = q.pending
		q.active = body
		q.pending = ""
		q.hasPending = false
		q.mu.Unlock()
	}
}

func (q *latestAnchorPatchQueue) Stop() {
	q.mu.Lock()
	q.stopped = true
	q.pending = ""
	q.hasPending = false
	q.mu.Unlock()
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
	host                sessionPTY
	cleanup             func()
	restored            bool // true when NewSession was invoked with AIKind set (recovery path)
	applyHookAIIdentity func(kind, aiSessionID string)
	feishuUserActivated atomic.Bool
	inputHistory        *sessionInputHistory
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

func (h *relayHost) SetPromptHistoryStore(store *PromptHistoryStore) {
	h.promptHistory.Store(store)
}

func (h *relayHost) SetSessionInputHistoryStore(store *sessionInputHistoryStore) {
	h.inputHistoryStore = store
}

// RecordPrompt fans one verified submission into the PTY-lifetime history and
// the independent device-level suggestion store.
func (h *relayHost) RecordPrompt(id uuid.UUID, prompt string) error {
	sessionErr := h.recordSessionInput(id, prompt)
	var suggestionErr error
	if history := h.promptHistory.Load(); history != nil {
		suggestionErr = history.Record(prompt)
	}
	return errors.Join(sessionErr, suggestionErr)
}

func (h *relayHost) handleCodexTranscriptTurn(id uuid.UUID, aiSID string, turn codexTranscriptTurn) {
	if turn.Role == codexTurnRoleUser {
		if err := h.RecordPrompt(id, turn.Text); err != nil {
			logWarn("prompt-history", "record codex rollout prompt session=%s: %v", id, err)
		}
	}

	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	event := feishu.TurnEvent{Kind: feishu.TurnAssistantFinal, Text: turn.Text}
	if turn.Role == codexTurnRoleUser {
		event.Kind = feishu.TurnUserPrompt
		// AIChunker uses transcript identity to reset when a second Codex
		// conversation starts in the same terminal pane.
		event.TranscriptPath = "codex:" + aiSID
	} else if turn.Role == codexTurnRoleQuestion {
		event.FormQuestions = make([]feishu.AskUserQuestionEntry, 0, len(turn.Questions))
		for _, q := range turn.Questions {
			opts := make([]feishu.QuestionOption, 0, len(q.Options))
			for _, opt := range q.Options {
				opts = append(opts, feishu.QuestionOption{Label: opt.Label, Description: opt.Description})
			}
			event.FormQuestions = append(event.FormQuestions, feishu.AskUserQuestionEntry{
				Question:  q.Question,
				Options:   opts,
				InputMode: internalfeishu.AskFormInputCodex,
			})
		}
	}
	disp.DispatchTurn(id.String(), event)
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
	// Register the same-size resize before touching the PTY so the resulting
	// SIGWINCH redraw is displayed without being mistaken for task activity.
	sess.UpdateSize(info.Cols, info.Rows)
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
	if h.inputHistoryStore != nil {
		if err := h.inputHistoryStore.Close(); err != nil {
			logWarn("session-input-history", "flush on shutdown: %v", err)
		}
		h.inputHistoryStore = nil
	}
}

// NewSession spawns a PTY for the given command and adopts it as a session.
func (h *relayHost) NewSession(ctx context.Context, req NewSessionReq) (uuid.UUID, error) {
	command := req.Command
	if command == "" {
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

	argv := append([]string{command}, defaultShellArgs(command, req.Args)...)
	logInfo("session", "command=%q args=%v cwd=%q aiKind=%q -> argv=%v",
		command, req.Args, cwd, req.AIKind, argv)
	env := terminalEnvForXterm(os.Environ())

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
	var currentResolveKind string
	var currentAISessionID string
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
		currentResolveKind = kind
		currentAISessionID = initialSID
		if awaitRestoredStart {
			restoredStartKind = kind
		} else {
			restoredStartKind = ""
		}
		if kind == "codex" && initialSID != "" {
			holdCodexBinding(generationCtx, resolveCwd, initialSID)
		}
		resolveMu.Unlock()
		var codexStreamOnce sync.Once
		startCodexStream := func(aiSID string, replayExisting bool) {
			if kind != "codex" || aiSID == "" {
				return
			}
			codexStreamOnce.Do(func() {
				go startCodexTurnStream(generationCtx, resolveCwd, aiSID, replayExisting, func(turn codexTranscriptTurn) {
					h.handleCodexTranscriptTurn(id, aiSID, turn)
				})
			})
		}

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
			// A restored Codex SID came from the previous pane generation and is
			// already the authoritative resume credential. Recent Codex versions
			// let a global app-server daemon write rollout files, so the cwd/mtime
			// fallback cannot prove that an advanced same-cwd rollout belongs to
			// this PTY. Running it here could replace the known SID with a VS Code
			// or another terminal's conversation. Keep tracking the known rollout's
			// title below, but never guess a replacement SID for this generation.
			if kind == "codex" && initialSID != "" {
				return
			}
			logDebug("ai-sid", "classified session=%s kind=%s generation=%d — start resolve", id, kind, generation)
			h.startSniffFn(generationCtx, sess, resolveCwd, kind, pty.Pid(), func(aiSid string) {
				resolveMu.Lock()
				if generation == resolveGeneration {
					// Keep validation + publication in one critical section. Otherwise
					// a new generation could publish its clearing event after this
					// check, then this stale callback could overwrite it with an old SID.
					currentAISessionID = aiSid
					h.onAISidCaptured(id, kind, aiSid)
					startCodexStream(aiSid, initialSID == "")
				}
				resolveMu.Unlock()
			})
		}()
		if kind == "codex" && initialSID != "" {
			startCodexStream(initialSID, false)
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
	active := &activeSession{
		host:         pty,
		cleanup:      combinedCleanup,
		restored:     req.AIKind != "",
		inputHistory: h.newSessionInputHistory(id),
	}
	if req.InputHistoryFromSessionID != "" {
		previousID, err := uuid.Parse(req.InputHistoryFromSessionID)
		if err != nil || previousID == uuid.Nil {
			logWarn("session-input-history", "ignore invalid restore source session=%s", req.InputHistoryFromSessionID)
		} else if err := h.restorePersistedInputHistory(previousID, active.inputHistory); err != nil {
			logWarn("session-input-history", "restore source=%s target=%s: %v", previousID, id, err)
		}
	}
	active.applyHookAIIdentity = func(kind, aiSessionID string) {
		var codexSession *session.Session
		var codexCwd string
		if kind == "codex" {
			if sess, ok := h.server.Registry().Get(id); ok {
				codexSession = sess
				codexCwd = sess.Info().Cwd
			}
		}
		resolveMu.Lock()
		if kind == "" || aiSessionID == "" {
			logWarn("ai-sid", "ignore hook identity session=%s reason=empty-identity", id)
			resolveMu.Unlock()
			return
		}
		if currentResolveKind != kind {
			logDebug("ai-sid", "ignore hook identity session=%s kind=%s reason=kind-mismatch current_kind=%s", id, kind, currentResolveKind)
			resolveMu.Unlock()
			return
		}
		if currentAISessionID == aiSessionID {
			logDebug("ai-sid", "ignore hook identity session=%s kind=%s reason=duplicate", id, kind)
			resolveMu.Unlock()
			return
		}
		// The hook is directly bound by ATTERM_SESSION_ID, so it supersedes the
		// filesystem/process heuristic for this generation. Bump the generation
		// before publishing so any callback already racing out of the cancelled
		// resolver is rejected by its generation check.
		if currentResolveCancel != nil {
			currentResolveCancel()
		}
		resolveGeneration++
		generationCtx, generationCancel := context.WithCancel(resolveCtx)
		currentResolveCancel = generationCancel
		currentAISessionID = aiSessionID
		if codexCwd != "" {
			holdCodexBinding(generationCtx, codexCwd, aiSessionID)
		}
		h.onAISidCaptured(id, kind, aiSessionID)
		logInfo("ai-sid", "applied hook identity session=%s kind=%s source=session-start", id, kind)
		resolveMu.Unlock()

		if codexSession != nil {
			go startCodexTurnStream(generationCtx, codexCwd, aiSessionID, false, func(turn codexTranscriptTurn) {
				h.handleCodexTranscriptTurn(id, aiSessionID, turn)
			})
			go startCodexKnownTitleResolve(generationCtx, codexSession, codexCwd, aiSessionID)
		}
	}
	active.feishuUserActivated.Store(!req.Recovered)

	// Every top-level AI OSC 133 C starts a new resolver generation. The only
	// exception is the first same-kind command injected for a restored pane: it
	// confirms the generation already established below instead of clearing the
	// known initial SID and starting a duplicate resolver.
	if sess, ok := h.server.Registry().Get(id); ok {
		sess.SetOnCommandSubmitted(active.inputHistory.Append)
		if req.Recovered {
			// Recovery deliberately skips eager Feishu attachment so restoring a
			// workspace does not recreate cards for every idle pane. This gate also
			// suppresses AI classification and task-state events emitted by the
			// automatically injected resume command. The first accepted client input
			// is authoritative user activity and opens the lazy-attach path.
			sess.SetOnInput(func() {
				active.feishuUserActivated.Store(true)
				h.lazyAttachIfMissing(context.Background(), sess, id)
			})
		}
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
			// autoAttach="all" may have created this card while the PTY was
			// still a shell. Switch its body source before the first AI hook
			// event arrives so TUI redraw bytes cannot pollute the card.
			h.upgradeFeishuSessionToAI(id.String())
			// autoAttach="ai": attach the Feishu subscriber now that we know
			// this is an AI session. A restored session remains silent while its
			// resume command starts automatically; real client input opens the gate.
			if active.feishuUserActivated.Load() {
				h.lazyAttachIfMissing(context.Background(), sess, id)
			}
		})
		sess.SetOnTaskStateChange(func(sid uuid.UUID, prev, next string, meta session.TaskMeta) {
			disp := h.feishuDispatcher.Load()
			if disp == nil {
				return
			}
			// Lazy backfill: piggyback on the next task-state change, except while
			// a recovered pane is only replaying its automatic resume sequence.
			// Idempotence and network I/O are handled by lazyAttachIfMissing.
			if active.feishuUserActivated.Load() {
				h.lazyAttachIfMissing(context.Background(), sess, sid)
			}
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
	if req.AIKind != "" {
		if sess, ok := h.server.Registry().Get(id); ok {
			sidCopy := id
			resumeArgv := computeResumeArgs(req.AIKind, req.InitialAISessionID, req.InitialAICommandLine)
			startResolveGeneration(sess, req.AIKind, cwd, req.InitialAISessionID, resumeArgv != nil)
			if resumeArgv != nil {
				line := strings.Join(resumeArgv, " ") + "\n"
				ptyCopy := pty
				sess.SetOnFirstPrompt(func() {
					logInfo("recovery", "restored ai session=%s — inject resume %q", sidCopy, strings.TrimSpace(line))
					go func() { _, _ = ptyCopy.Write([]byte(line)) }()
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
	h.sessions[id] = active
	h.mu.Unlock()
	h.notifyChange()

	// autoAttach="all": attach immediately for genuinely new sessions. A
	// recovery can recreate many idle panes at once; those defer attachment
	// until their first accepted client input opens the lazy-attach gate.
	// Run in a goroutine so Feishu I/O never blocks PTY creation.
	if sess, ok := h.server.Registry().Get(id); ok && shouldEagerAttachFeishuSession(req) {
		h.lazyAttachIfMissing(ctx, sess, id)
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

func shouldEagerAttachFeishuSession(req NewSessionReq) bool {
	return !req.Recovered
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

// mergeShellIntegrationPlan returns (argv', env') with the plan's args and
// env merged into the shell invocation. Zero plans are the identity transform.
func mergeShellIntegrationPlan(argv, env []string, p shellintegration.Plan) ([]string, []string) {
	if len(p.ExtraArgs) == 0 && len(p.ExtraEnv) == 0 {
		return argv, env
	}
	outArgv := append([]string{}, argv...)
	bashEmulateLogin := false
	if len(p.ExtraArgs) > 0 {
		if p.Shell == "bash" && len(p.ExtraArgs) >= 2 && p.ExtraArgs[0] == "--rcfile" {
			// Bash both stops parsing long options after a short option and ignores
			// --rcfile entirely in login mode. Put --rcfile first, remove login
			// flags, and let the generated rc reproduce login-profile loading.
			outArgv = append([]string{argv[0]}, p.ExtraArgs[:2]...)
			for _, arg := range argv[1:] {
				if arg == "-l" || arg == "--login" {
					bashEmulateLogin = true
					continue
				}
				outArgv = append(outArgv, arg)
			}
			outArgv = append(outArgv, p.ExtraArgs[2:]...)
		} else {
			outArgv = append(outArgv, p.ExtraArgs...)
		}
	}
	outEnv := append([]string{}, env...)
	if bashEmulateLogin {
		outEnv = append(outEnv, "ATTERM_BASH_EMULATE_LOGIN=1")
	}
	if len(p.ExtraEnv) > 0 {
		outEnv = append(outEnv, p.ExtraEnv...)
	}
	return outArgv, outEnv
}
