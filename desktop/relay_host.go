package main

import (
	"context"
	"errors"
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
	sqliteStore  *userstore.SQLiteStore // concrete type for Feishu binding lookup

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

// SetFeishuDispatcher atomically swaps the dispatcher used for command-finished
// and waiting-input Feishu cards. Safe to call concurrently with session
// callbacks. A nil dispatcher disables dispatch. Also wires the per-session
// anchor-button-swap callback so AskUserQuestion can flip the keystroke row
// to option buttons, and the AskUserQuestion form-insert callback.
func (h *relayHost) SetFeishuDispatcher(d *feishu.Dispatcher) {
	h.feishuDispatcher.Store(d)
	if d != nil {
		d.SetOnAnchorButtons(h.swapAnchorButtons)
		d.SetOnAskForm(h.updateAnchorAskForm)
		d.SetOnTurnMissingChunker(h.onTurnMissingChunker)
	}
}

// updateAnchorAskForm inserts an AskUserQuestion form container after the
// body markdown (questions non-empty), or removes it (questions nil).
// Best-effort: errors logged, never bubble up.
func (h *relayHost) updateAnchorAskForm(sessionIDStr string, questions []feishu.AskUserQuestionEntry) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	anchor := h.feishuCards.BySessionID(sessionIDStr)
	if anchor == nil {
		return
	}
	if len(questions) == 0 {
		go h.deleteAnchorForm(anchor)
		return
	}
	// Insert (or replace) the form: DELETE any existing first, then CREATE.
	// Both steps under SendMu so seq stays contiguous.
	specs := make([]internalfeishu.AskFormQuestion, 0, len(questions))
	for _, q := range questions {
		opts := make([]internalfeishu.AskFormOpt, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, internalfeishu.AskFormOpt{Label: o.Label})
		}
		specs = append(specs, internalfeishu.AskFormQuestion{Question: q.Question, Options: opts, MultiSelect: q.MultiSelect})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			log.Printf("feishu-anchor-form: token failed session=%s: %v", sessionIDStr, err)
			return
		}
		anchor.SendMu.Lock()
		defer anchor.SendMu.Unlock()
		if anchor.FormMounted {
			seqDel := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seqDel); err != nil {
				log.Printf("feishu-anchor-form: pre-DELETE failed session=%s: %v", sessionIDStr, err)
				return
			}
			anchor.FormMounted = false
		}
		// Also strip the standalone Type-here input while the form is up —
		// each question already has its own custom input, so keeping the
		// original input around just crowds the card. CurrentInputID == ""
		// means "no input currently mounted"; deleteAnchorForm restores it.
		if anchor.CurrentInputID != "" {
			seqInp := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorInputWithSeq(ctx, tok, anchor.CardToken, anchor.CurrentInputID, seqInp); err != nil {
				log.Printf("feishu-anchor-form: input DELETE failed session=%s: %v", sessionIDStr, err)
				// Non-fatal — keep going and mount the form anyway.
			} else {
				anchor.CurrentInputID = ""
			}
		}
		// Same for the buttons row (^C/^D/Esc/Enter/结束). The form has its
		// own 提交/重置 buttons; the keystroke row on top of that is noise.
		if anchor.ButtonsMounted {
			seqBtn := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, seqBtn); err != nil {
				log.Printf("feishu-anchor-form: buttons DELETE failed session=%s: %v", sessionIDStr, err)
			} else {
				anchor.ButtonsMounted = false
			}
		}
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		// Pass seqCre as the mount seq so widget element_ids get a fresh
		// suffix each time the form remounts on the same anchor card.
		// Otherwise the Feishu client caches widget state by element_id
		// and reloads the previous submission's dropdowns/inputs on
		// subsequent AskUserQuestion cycles (image #107).
		form := internalfeishu.RenderAskQuestionForm(sessionIDStr, specs, seqCre)
		if err := disp.InsertAnchorFormWithSeq(ctx, tok, anchor.CardToken, form, seqCre); err != nil {
			log.Printf("feishu-anchor-form: CREATE failed session=%s q=%d: %v", sessionIDStr, len(questions), err)
			return
		}
		anchor.FormMounted = true
		anchor.PendingForm = specs
		log.Printf("feishu-anchor-form: inserted session=%s q=%d seq=%d", sessionIDStr, len(questions), seqCre)
	}()
}

// deleteAnchorForm removes the currently-mounted form (if still there) and
// restores the standalone input + buttons row that were torn down when the
// form went up. Independent state checks — the service layer's
// handleAskFormSubmit ALSO calls a variant of this that flips FormMounted
// as soon as the form's DELETEd, so this path routinely arrives with the
// form already gone; it must still restore input/buttons in that case.
// Safe to call repeatedly: each of the three fixups is guarded on state.
func (h *relayHost) deleteAnchorForm(anchor *internalfeishu.CardAnchor) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := disp.GetToken(ctx)
	if err != nil {
		log.Printf("feishu-anchor-form: token failed session=%s: %v", anchor.SessionID, err)
		return
	}
	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	if anchor.FormMounted {
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seq); err != nil {
			log.Printf("feishu-anchor-form: DELETE failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.FormMounted = false
		anchor.PendingForm = nil
		log.Printf("feishu-anchor-form: removed session=%s seq=%d", anchor.SessionID, seq)
	}
	// Restore the standalone Type-here input we tore down when the form went
	// up. New element_id every time — Feishu's client caches by id and would
	// otherwise resurrect stale value even across DELETE+CREATE.
	if anchor.CurrentInputID == "" {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		newID := fmt.Sprintf("anchor_input_%d", seqCre)
		if err := disp.CreateAnchorInputWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, newID, seqCre); err != nil {
			log.Printf("feishu-anchor-form: input CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.CurrentInputID = newID
		log.Printf("feishu-anchor-form: input restored session=%s seq=%d new_id=%s", anchor.SessionID, seqCre, newID)
	}
	// Restore the keystroke buttons row after the input so the on-card
	// element order stays: body → input → buttons. CurrentInputID is the
	// insert-after anchor when present; when input somehow failed to
	// restore, fall back to inserting after the body.
	if !anchor.ButtonsMounted {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		target := anchor.CurrentInputID
		if target == "" {
			target = internalfeishu.AnchorBodyElementID
		}
		if err := disp.CreateAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, target, seqCre); err != nil {
			log.Printf("feishu-anchor-form: buttons CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.ButtonsMounted = true
		log.Printf("feishu-anchor-form: buttons restored session=%s seq=%d", anchor.SessionID, seqCre)
	}
}

// swapAnchorButtons PATCHes the anchor card's button row. options nil →
// restore the default keystroke row (^C/^D/Esc/Enter/结束); options non-empty
// → render one primary button per option label (clicking submits the label
// as if typed into the input box). Best-effort: errors logged, never bubble.
func (h *relayHost) swapAnchorButtons(sessionIDStr string, options []string) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	anchor := h.feishuCards.BySessionID(sessionIDStr)
	if anchor == nil {
		return
	}
	var partial map[string]any
	if len(options) > 0 {
		partial = internalfeishu.AskOptionsColumnSet(sessionIDStr, options)
	} else {
		partial = internalfeishu.DefaultButtonsColumnSet(sessionIDStr)
	}
	// PATCH the column_set element; `tag` is immutable per V2 contract so
	// strip it from the partial body.
	delete(partial, "tag")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			log.Printf("feishu-anchor-buttons: token failed session=%s: %v", sessionIDStr, err)
			return
		}
		anchor.SendMu.Lock()
		// Skip when the buttons element isn't on the card: an
		// AskUserQuestion form-mount tears down anchor_buttons, and the
		// PATCH here would then hit Feishu's 300313 "elementID not found".
		// The form tear-down path is responsible for CREATE-ing the row
		// back; we shouldn't step on it with a PATCH that can't work.
		if !anchor.ButtonsMounted {
			anchor.SendMu.Unlock()
			log.Printf("feishu-anchor-buttons: skip (buttons not mounted) session=%s opts=%d", sessionIDStr, len(options))
			return
		}
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		err = disp.PatchAnchorElement(ctx, tok, anchor.CardToken,
			internalfeishu.AnchorButtonsElementID, partial, seq)
		anchor.SendMu.Unlock()
		if err != nil {
			log.Printf("feishu-anchor-buttons: PATCH failed session=%s opts=%d: %v", sessionIDStr, len(options), err)
			return
		}
		log.Printf("feishu-anchor-buttons: swapped session=%s opts=%d seq=%d", sessionIDStr, len(options), seq)
	}()
}

// SetFeishuRemoteTermState installs the callback the anchor-card guard uses to
// read remote-terminal gate state for the current mode.
func (h *relayHost) SetFeishuRemoteTermState(fn func(ctx context.Context) (bool, string, string, bool)) {
	h.feishuRemoteTermState = fn
}

type activeSession struct {
	host     *ptyhost.Host
	cleanup  func()
	restored bool // true when NewSession was invoked with AIKind set (recovery path)
}

// appendFeishuHookEnv adds ATTERM_SESSION_ID + ATTERM_HOOK_ENDPOINT to
// a process env slice. The hook endpoint comes from the desktop's
// in-process FeishuService (set up in Task 21); empty endpoint = skip.
func appendFeishuHookEnv(env []string, sessionID, hookEndpoint string) []string {
	if sessionID != "" {
		env = append(env, "ATTERM_SESSION_ID="+sessionID)
	}
	if hookEndpoint != "" {
		env = append(env, "ATTERM_HOOK_ENDPOINT="+hookEndpoint)
	}
	return env
}

// shouldNotifySession reports whether a Feishu notification should fire for a
// session of the given workload type, honoring the "AI sessions only"
// preference. aiOnly=false → always notify; aiOnly=true → only ai sessions.
func shouldNotifySession(sessionType string, aiOnly bool) bool {
	if !aiOnly {
		return true
	}
	return sessionType == session.SessionTypeAI
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

// onAISidCaptured forwards a captured AI session id to the registered
// callback (set by the App during startup). Safe to call when the
// callback is nil — the sniff just fires and forgets.
func (h *relayHost) onAISidCaptured(localSessionID uuid.UUID, kind, aiSid string) {
	if h.aiSidCallback != nil {
		h.aiSidCallback(localSessionID, kind, aiSid)
	}
}

// tryStartLazyAttach acquires the per-session lazy-backfill slot for sid
// iff the session has no live FeishuSubscriber and no backfill goroutine
// is already running. Returns true when the caller now owns the slot and
// MUST release it via clearLazyAttachInFlight once the attach path exits.
// Under a concurrent burst of turn/state events on the same session (e.g.
// after the user toggles remote-terminal on and claude spits back a few
// hook events in quick succession), only one caller wins; the rest bail.
func (h *relayHost) tryStartLazyAttach(sidStr string) bool {
	h.feishuSubsMu.Lock()
	defer h.feishuSubsMu.Unlock()
	if _, exists := h.feishuSubs[sidStr]; exists {
		return false
	}
	if h.lazyAttachInFlight == nil {
		h.lazyAttachInFlight = map[string]bool{}
	}
	if h.lazyAttachInFlight[sidStr] {
		return false
	}
	h.lazyAttachInFlight[sidStr] = true
	return true
}

// clearLazyAttachInFlight releases the slot claimed by tryStartLazyAttach.
// Safe to call on a sid that was never claimed (delete on a missing key
// is a no-op).
func (h *relayHost) clearLazyAttachInFlight(sidStr string) {
	h.feishuSubsMu.Lock()
	delete(h.lazyAttachInFlight, sidStr)
	h.feishuSubsMu.Unlock()
}

// lazyAttachIfMissing gates on tryStartLazyAttach and, on success, kicks
// off attachFeishuSubscriberForAutoAttach in a goroutine (attach performs
// a network round-trip so callers — task-state callbacks, hook HTTP
// handlers — must not block). triggerMode "ai" is used so
// SessionAutoAttach="ai" bindings also honor the lazy path; "all" bindings
// still attach because their switch case is unconditional. Callers pass
// a nil-safe context (attach's own guards handle a nil dispatcher etc.,
// so a stale session that closed mid-flight is not a panic risk).
func (h *relayHost) lazyAttachIfMissing(ctx context.Context, sess *session.Session, sid uuid.UUID) {
	sidStr := sid.String()
	if !h.tryStartLazyAttach(sidStr) {
		return
	}
	go func() {
		defer h.clearLazyAttachInFlight(sidStr)
		h.attachFeishuSubscriberForAutoAttach(ctx, sess, sid, "ai")
	}()
}

// onTurnMissingChunker is registered with the Dispatcher so a TurnEvent
// arriving for a session with no AIChunker (i.e. the user toggled
// remote-terminal on after this AI session started) triggers a lazy
// backfill. The current turn's body is lost because attach runs
// asynchronously; the anchor card is in place for the next turn.
func (h *relayHost) onTurnMissingChunker(sessionIDStr string) {
	sid, err := uuid.Parse(sessionIDStr)
	if err != nil {
		return
	}
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		return
	}
	h.lazyAttachIfMissing(context.Background(), sess, sid)
}

// attachFeishuSubscriberForAutoAttach is called either immediately on session
// creation (triggerMode="all") or from the AI-classified callback
// (triggerMode="ai"). It checks whether the user's binding allows the attach
// given the configured SessionAutoAttach value, then posts an anchor card and
// starts the subscriber. All Feishu I/O is error-swallowed and non-blocking
// with respect to the session lifecycle.
func (h *relayHost) attachFeishuSubscriberForAutoAttach(ctx context.Context, sess *session.Session, sessID uuid.UUID, triggerMode string) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	if h.feishuRemoteTermState == nil {
		return
	}
	enabled, openID, autoAttach, ok := h.feishuRemoteTermState(ctx)
	if !ok {
		// No binding / not ready — silently skip.
		return
	}
	if !enabled {
		return
	}
	if openID == "" {
		return
	}
	// Gate on autoAttach: "ai" triggers only fire from the AI-classified
	// callback; "all" triggers fire immediately. "none" never auto-attaches.
	switch autoAttach {
	case "none":
		return
	case "ai":
		if triggerMode != "ai" {
			return
		}
	case "all":
		// always proceed regardless of triggerMode
	default:
		// unknown value — skip
		return
	}

	sessionIDStr := sessID.String()

	// Guard against double-attach: if a subscriber is already registered for this
	// session (e.g. autoAttach="all" already attached at NewSession time, then
	// SetOnAIClassified fires later and re-enters this function), bail.
	h.feishuSubsMu.Lock()
	if _, exists := h.feishuSubs[sessionIDStr]; exists {
		h.feishuSubsMu.Unlock()
		return
	}
	h.feishuSubsMu.Unlock()

	// Short label fallback when Info().Title is empty: first 8 chars of UUID.
	label := sessionIDStr
	if len(label) > 8 {
		label = label[:8]
	}

	info := sess.Info()
	h.mu.Lock()
	restored := false
	if as, ok := h.sessions[sessID]; ok {
		restored = as.restored
	}
	h.mu.Unlock()

	cardBody, err := internalfeishu.RenderAnchorCreate(internalfeishu.AnchorState{
		SessionID:    sessionIDStr,
		SessionLabel: label,
		Title:        info.Title,
		Cwd:          info.Cwd,
		// StatusText intentionally empty — the body element's PrependStatus
		// preamble carries live state instead. The subtitle is fixed-at-send
		// in V2 (no element-level PATCH for header), so a stale "running"
		// there is worse than just the cwd alone.
		StatusText:   "",
		BodyMarkdown: "",
		Template:     "blue",
		Restored:     restored,
	})
	if err != nil {
		log.Printf("feishu-anchor: render create failed session=%s: %v", sessID, err)
		return
	}

	msgID, cardToken, _, err := disp.SendAnchorCard(ctx, cardBody)
	if err != nil {
		log.Printf("feishu-anchor: send anchor card failed session=%s: %v", sessID, err)
		return
	}

	anchor := &internalfeishu.CardAnchor{
		SessionID:      sessionIDStr,
		CardMsgID:      msgID,
		CardToken:      cardToken,
		OwnerOpenID:    openID,
		CreatedAt:      time.Now(),
		CurrentInputID: internalfeishu.AnchorInputElementID,
		ButtonsMounted: true,
	}
	h.feishuCards.Put(anchor)

	// rt threads the live status state. Updated by SetOnTaskStateChange and
	// re-rendered on a 30s ticker so elapsed time refreshes even when the AI
	// is idle. render() composes the wrapper (status preamble + inner body)
	// from current state and PATCHes; everyone (chunker flush, state callback,
	// ticker) goes through it so the wire body stays consistent.
	rt := &anchorRuntime{createdAt: time.Now()}
	rt.taskState.Store("")
	rt.lastInner.Store("")

	patchWrapped := func(wrapped string) {
		go func() {
			tok, _, err := disp.GetToken(context.Background())
			if err != nil {
				log.Printf("feishu-anchor: patch token failed session=%s: %v", sessID, err)
				return
			}
			anchor.SendMu.Lock()
			seq := atomic.AddInt64(&anchor.PatchSeq, 1)
			err = internalfeishu.PatchWithRetry(func() error {
				return disp.PatchAnchor(context.Background(), tok, anchor.CardToken, wrapped, seq)
			})
			anchor.SendMu.Unlock()
			if err == nil {
				log.Printf("feishu-anchor: patch ok session=%s seq=%d", sessID, seq)
				return
			}
			if internalfeishu.IsCardGoneError(err) {
				log.Printf("feishu-anchor: card gone session=%s — detaching", sessID)
				h.feishuCards.RemoveBySessionID(sessID.String())
				h.detachFeishuSubscriber(sessID)
				return
			}
			var ace *internalfeishu.AuthClassError
			if errors.As(err, &ace) {
				log.Printf("feishu-anchor: auth refresh needed session=%s (%v)", sessID, err)
				return
			}
			log.Printf("feishu-anchor: patch gave up after retry session=%s: %v", sessID, err)
		}()
	}

	rt.render = func() {
		state, _ := rt.taskState.Load().(string)
		inner, _ := rt.lastInner.Load().(string)
		wrapped := internalfeishu.PrependStatus(state, time.Since(rt.createdAt), inner)
		patchWrapped(wrapped)
	}

	// flush is the Chunker callback — stash the latest inner body and re-render.
	flush := func(body string) {
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		log.Printf("feishu-anchor: flush session=%s body_len=%d preview=%q", sessID, len(body), preview)
		rt.lastInner.Store(body)
		rt.render()
	}

	h.feishuSubsMu.Lock()
	h.feishuAnchorRuntimes[sessionIDStr] = rt
	h.feishuSubsMu.Unlock()

	// AI sessions render their anchor card body from per-turn AIChunker events
	// (👤/🤖/🛠), not from raw PTY bytes. Shell sessions still need the PTY
	// roller because there are no hook events to fall back on. `info` was
	// captured above for header rendering — reuse it here.
	pumpPTYBytes := info.Type != session.SessionTypeAI
	sub := internalfeishu.AttachFeishuSubscriber(sess, openID, pumpPTYBytes, flush)

	h.feishuSubsMu.Lock()
	h.feishuSubs[sessionIDStr] = sub
	h.feishuSubsMu.Unlock()

	// AI session only: wire a per-session AIChunker into the dispatcher so
	// per-turn hook events stream into the same anchor card. The chunker
	// shares the flush closure (same anchor + token path). The tick goroutine
	// keeps the chunker flushing on idle; it exits cleanly via fs.Done().
	if info.Type == session.SessionTypeAI {
		aiChunker := internalfeishu.NewAIChunker(flush)
		disp.AttachAIChunker(sessionIDStr, aiChunker)
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					aiChunker.Tick()
				case <-sub.Done():
					return
				}
			}
		}()
	}

	// Status preamble refresher: re-render every 30s so elapsed time bumps
	// even when the AI is idle (no body change means flush is never called
	// otherwise). Exits with sub.Done() so detach kills it cleanly.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				rt.render()
			case <-sub.Done():
				return
			}
		}
	}()

	log.Printf("feishu-anchor: attached session=%s card_msg_id=%s", sessID, msgID)
}

// OnRemoteTerminalToggle reacts to changes in the binding's
// RemoteTerminalEnabled flag. When flipped off, detach every active
// FeishuSubscriber and PATCH each anchor to its archived state. Sessions
// themselves are unaffected. When flipped on, no bulk re-attach — new
// sessions pick up via autoAttach; pre-existing sessions need to recreate
// (P2 — explicit /attach command).
func (h *relayHost) OnRemoteTerminalToggle(enabled bool) {
	if enabled {
		return // No bulk re-attach on enable; new sessions pick up naturally.
	}
	// Snapshot the current session IDs under the lock, then release before
	// calling detachFeishuSubscriber (which re-acquires the lock for each
	// entry and removes it atomically).
	h.feishuSubsMu.Lock()
	sids := make([]string, 0, len(h.feishuSubs))
	for sid := range h.feishuSubs {
		sids = append(sids, sid)
	}
	h.feishuSubsMu.Unlock()
	for _, sid := range sids {
		if id, err := uuid.Parse(sid); err == nil {
			h.detachFeishuSubscriber(id)
		}
	}
}

// detachFeishuSubscriber removes and stops the FeishuSubscriber for sessID,
// then PATCHes the anchor card to archived state. Idempotent.
func (h *relayHost) detachFeishuSubscriber(sessID uuid.UUID) {
	sessionIDStr := sessID.String()

	h.feishuSubsMu.Lock()
	sub := h.feishuSubs[sessionIDStr]
	delete(h.feishuSubs, sessionIDStr)
	delete(h.feishuAnchorRuntimes, sessionIDStr)
	h.feishuSubsMu.Unlock()

	// Detach the AIChunker BEFORE sub.Detach() so in-flight hook events do not
	// push turns into a chunker whose tick goroutine is already exiting.
	disp := h.feishuDispatcher.Load()
	if disp != nil {
		disp.DetachAIChunker(sessionIDStr)
	}

	if sub != nil {
		sub.Detach()
	}

	anchor := h.feishuCards.BySessionID(sessionIDStr)
	h.feishuCards.RemoveBySessionID(sessionIDStr)

	if anchor == nil {
		return
	}

	// Archive the anchor card in a goroutine; session close must not block.
	go func() {
		disp := h.feishuDispatcher.Load()
		if disp == nil {
			return
		}
		tok, _, err := disp.GetToken(context.Background())
		if err != nil {
			log.Printf("feishu-anchor: archive token failed session=%s: %v", sessID, err)
			return
		}
		// Build the archive body markdown: last shell output + footer line.
		footer := "**已结束 at " + time.Now().Format("15:04:05") + "**"
		lastBody := anchor.LastBody
		archiveMD := lastBody
		if archiveMD != "" {
			archiveMD += "\n" + footer
		} else {
			archiveMD = footer
		}
		anchor.SendMu.Lock()
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		perr := disp.PatchAnchor(context.Background(), tok, anchor.CardToken, archiveMD, seq)
		anchor.SendMu.Unlock()
		if perr != nil {
			if !internalfeishu.IsCardGoneError(perr) {
				log.Printf("feishu-anchor: archive patch failed session=%s: %v", sessID, perr)
			}
		}
	}()
}
