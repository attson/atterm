// Desktop side of the mobile "use a profile" flow (design doc §3): mobile
// cannot fork a PTY, so it asks a desktop to. This file turns a
// TypeSessionCreate frame arriving on the uplink into a real session and
// answers with TypeSessionCreated, reusing relayHost.NewSession — the exact
// path a locally-created tab/split already goes through — so the result is
// announced, recovers, and applies its profile identically.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// sessionCreateConcurrency bounds how many session-create forks this
// desktop will run at once.
//
// This is NOT a per-requesting-phone limit: SessionCreatePayload carries no
// client identity (see its doc comment in internal/proto/frame.go), and a
// desktop keeps exactly one uplink connection shared by every phone that
// talks to it — so a bound at this level is inherently per-desktop, not
// per-client. The true "does this one phone already have a request in
// flight" bound lives at the relay instead, in sessionCreateRouter
// (internal/relay/session_create_router.go), which is where client
// identity — one outbound channel per connected client — actually exists.
//
// What this bound is genuinely responsible for is the two things visible
// from here: never block the uplink read loop on a PTY fork, and never let
// an unbounded number of forks run at once if the relay is buggy or
// compromised and forwards more than it should. A modest concurrency
// window (not 1) covers that without pretending to see per-client identity
// it cannot: several phones legitimately creating sessions around the same
// moment should not queue behind each other on one desktop. Mirrors
// fsWorkerPool's shape (remote_fs.go) at a similar size.
const sessionCreateConcurrency = 8

const (
	// sessionCreateErrUnknownProfile answers a profile_id that does not
	// match anything in local config. Never silently substituted for the
	// configured default (that leniency belongs to resolveSessionProfile's
	// explicit/default precedence for locally-typed defaults going stale —
	// see relay_host.go) — a phone asked for something specific by id, and
	// handing it a different profile it never chose would be a worse
	// surprise than telling it plainly that the id is gone.
	sessionCreateErrUnknownProfile = "unknown_profile"
	// sessionCreateErrPermissionDenied answers a request arriving on an
	// uplink whose remote_permission is below what forking a shell
	// requires. Forking a PTY and (via a profile's startup_cmd) running
	// arbitrary commands is at least as privileged as typing into an
	// existing one, so this uses the same control-or-full bar
	// localFrameAllowedByPermission enforces for TypeIn/TypeResize
	// (uplink.go) — a desktop the owner published as view-only must not
	// fork a shell just because a request reached it.
	sessionCreateErrPermissionDenied = "permission_denied"
	// sessionCreateErrBusy answers a request arriving once
	// sessionCreateConcurrency forks are already running on this
	// connection.
	sessionCreateErrBusy = "session_create_busy"
)

// sessionCreateHandler runs TypeSessionCreate requests off the uplink read
// loop — the same reason fsWorkerPool (remote_fs.go) exists: that loop also
// carries every session's keystrokes, and forking a PTY talks to the OS
// (exec, pty allocation, cwd stat), which is not instant. Handling it inline
// would stall every other session on this uplink for the duration of one
// fork.
type sessionCreateHandler struct {
	// connCtx is this uplink connection's own lifetime — cancelled by
	// runOnce's defer on every return, including a routine reconnect. Used
	// only for concerns that should genuinely end with the connection:
	// sendResult's blocking-send escape hatch. It must NOT reach the actual
	// fork — see forkCtx.
	connCtx context.Context
	// forkCtx governs the OS process a create request spawns. Sourced from
	// host.SessionForkContext() (the app's own lifetime), not connCtx,
	// specifically so a session created for a phone survives the next
	// uplink reconnect the same way a locally-created tab already does
	// (App.NewSession passes a.ctx directly). A context tied to one
	// connection's lifetime governing a long-lived PTY was the bug here
	// before: connCtx cancels on every reconnect, a routine event for that
	// link, and that cancellation used to reach exec.CommandContext
	// (ptyhost.Open) and AdoptSession, killing the session within moments
	// of the very next network blip.
	forkCtx context.Context
	out     chan<- proto.Frame
	host    *relayHost

	// remotePermission is the uplink's raw (unnormalized) remote_permission
	// setting — the same value fsWorkerPool checks (u.rawRemotePermission
	// in uplink.go), used the same fail-closed way: only the exact strings
	// "control" or "full" pass; anything else, including an empty or
	// unrecognized value, is refused. This deliberately does not use
	// normalizeRemotePermission, which defaults an unrecognized value to
	// Full for the less-dangerous TypeIn/TypeResize path — forking a shell
	// (and, via a profile's startup_cmd, running arbitrary commands) is at
	// least as privileged as typing into an existing one and does not get
	// that leniency.
	remotePermission string

	// newSession forks the session. Defaults to host.NewSession; tests
	// override it (via uplink.sessionCreateExec) to control PTY-fork timing
	// without spawning a real shell, the same role fsExec plays for
	// filesystem requests.
	newSession func(ctx context.Context, req NewSessionReq) (uuid.UUID, error)

	mu       sync.Mutex
	inFlight int

	// testHook, when non-nil, runs at the top of run() before any real
	// work — nil in production. Lets tests hold a create "in flight"
	// deterministically to exercise sessionCreateConcurrency without
	// racing real PTY-spawn timing, mirroring the fsExec test seam
	// uplink.go already uses for the same reason on the FS path.
	testHook func()
}

// newSessionCreateHandler builds a handler bound to one uplink connection's
// lifetime (connCtx) and outbound queue (out). remotePermission is the
// connection's raw remote_permission setting (u.rawRemotePermission in
// uplink.go) — see the field doc comment for why it is checked unnormalized.
// The fork context is derived from host.SessionForkContext(), independent of
// connCtx — see the forkCtx field doc comment.
func newSessionCreateHandler(connCtx context.Context, out chan<- proto.Frame, host *relayHost, remotePermission string) *sessionCreateHandler {
	return &sessionCreateHandler{
		connCtx:          connCtx,
		forkCtx:          host.SessionForkContext(),
		out:              out,
		host:             host,
		remotePermission: remotePermission,
		newSession:       host.NewSession,
	}
}

// submit hands one TypeSessionCreate request to a worker goroutine and
// returns immediately — it never blocks the uplink read loop on a PTY fork,
// matching fsWorkerPool.submit's contract.
func (h *sessionCreateHandler) submit(req proto.SessionCreatePayload) {
	if !h.acquire() {
		h.sendBusy(req)
		return
	}
	go func() {
		defer h.release()
		h.run(req)
	}()
}

func (h *sessionCreateHandler) acquire() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inFlight >= sessionCreateConcurrency {
		return false
	}
	h.inFlight++
	return true
}

func (h *sessionCreateHandler) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inFlight > 0 {
		h.inFlight--
	}
}

// sendBusy answers a refused request. Called directly from the read loop
// (via submit's synchronous acquire() check), so like fsWorkerPool.sendBusy
// it must never block: a backed-up writer must not stall the loop that
// keystrokes ride on. A dropped busy response degrades to the phone's own
// 30s timeout, which is the same outcome a stalled link produces anyway.
func (h *sessionCreateHandler) sendBusy(req proto.SessionCreatePayload) {
	body, err := json.Marshal(proto.SessionCreatedPayload{
		RequestID: req.RequestID,
		OK:        false,
		Error:     sessionCreateErrBusy,
	})
	if err != nil {
		logWarn("uplink", "session_create busy-response marshal failed request_id=%s error=%v", req.RequestID, err)
		return
	}
	select {
	case h.out <- proto.Frame{Type: proto.TypeSessionCreated, Payload: body}:
	default:
		logWarn("uplink", "out chan full; dropping session_create busy response request_id=%s", req.RequestID)
	}
}

// run resolves req.ProfileID against LOCAL config and, if it names a real
// profile, forks a session through relayHost.NewSession — the exact path a
// locally-created tab already uses — so the result is announced, recovers,
// and applies the profile identically to one the desktop's own owner
// started.
//
// req is the entirety of what a phone gets a say in: SessionCreatePayload
// has exactly three string fields (request_id, host_id, profile_id) and no
// map or free-form field to smuggle a command/cwd/env/startup_cmd into (see
// its doc comment in internal/proto/frame.go). This function reads nothing
// from req but RequestID (to address the reply) and ProfileID (a lookup
// key into config this machine already owns) — never any other req field,
// because there is no other req field to read.
func (h *sessionCreateHandler) run(req proto.SessionCreatePayload) {
	if h.testHook != nil {
		h.testHook()
	}

	if h.remotePermission != proto.RemotePermissionControl && h.remotePermission != proto.RemotePermissionFull {
		h.sendResult(req.RequestID, false, uuid.Nil, sessionCreateErrPermissionDenied)
		return
	}

	var profiles []SessionProfile
	if h.host.cfg != nil {
		profiles = h.host.cfg.Get().Profiles
	}
	if _, ok := findProfileByID(profiles, req.ProfileID); !ok {
		h.sendResult(req.RequestID, false, uuid.Nil, sessionCreateErrUnknownProfile)
		return
	}

	shell, err := defaultShellForNewSession(h.host.cfg)
	if err != nil {
		h.sendResult(req.RequestID, false, uuid.Nil, err.Error())
		return
	}

	// forkCtx, not connCtx: this must not die on the next uplink reconnect.
	// See the forkCtx field doc comment above.
	//
	// This is deliberately not a fresh, request-scoped deadline either: the
	// phone already times out at 30s (design §4) and does not retry on
	// timeout; a second, shorter timeout here could fire first, mid-fork,
	// and leave a PTY nobody tracks while the phone is told only "timed
	// out," never "it actually started." NewSession failing loudly (bad
	// cwd, unresolvable shell) is still reported below — this is
	// specifically about not racing the phone's own patience with one of
	// our own.
	sid, err := h.newSession(h.forkCtx, NewSessionReq{
		Command:   shell,
		ProfileID: req.ProfileID,
	})
	if err != nil {
		// NewSession already validates everything about the profile that
		// needs validating beyond "does the id exist" — a missing absolute
		// Shell degrades to `shell` above and a missing Cwd fails naming
		// the profile (relay_host.go). Surfacing err.Error() as-is here
		// avoids a second copy of that logic that could drift from it.
		//
		// This is also the one place a raw local error string crosses the
		// relay to a mobile client: err may embed an absolute local path
		// (e.g. a missing profile cwd). Acceptable here specifically
		// because reaching this handler already required same-owner
		// relay authentication for this host — the recipient could already
		// attach to any session and read the filesystem through the FS
		// path, so a path fragment in an error string tells them nothing
		// they couldn't already see.
		h.sendResult(req.RequestID, false, uuid.Nil, err.Error())
		return
	}
	h.sendResult(req.RequestID, true, sid, "")
}

// findProfileByID looks up profile_id directly, unlike resolveSessionProfile
// (relay_host.go): that helper implements explicit/default precedence for a
// locally-typed session and treats a stale id as "no profile" — the right
// call when the id came from this machine's own now-possibly-edited config.
// A phone's profile_id is an explicit ask for one specific recipe; if it is
// gone, the caller must be told, not silently handed a different session.
func findProfileByID(profiles []SessionProfile, id string) (SessionProfile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return SessionProfile{}, false
}

// defaultShellForNewSession resolves the shell to launch when neither an
// explicit request nor the applied profile names one (an empty
// profile.Shell, or a profile whose absolute Shell doesn't exist here —
// NewSession's own fallback in relay_host.go degrades to exactly this
// value).
//
// For a locally-created tab, the frontend does this resolution itself —
// App.vue's spawnLocalShell calls ListShells() and passes shells[0] as
// NewSessionReq.Command before ever calling into relayHost.NewSession.
// TypeSessionCreate has no frontend in the loop, so this walks the exact
// same shellPriorityOrder App.ListShells uses (shell_resolve.go) and takes
// the first candidate that resolves on PATH — the two used to be
// independently written copies of the same priority list, which a review
// found could silently drift (see shellPriorityOrder's doc comment).
// Without this, a session-create for a profile with no explicit Shell would
// hand NewSession an empty Command, and NewSession refuses that outright
// ("empty command") — exactly the profile shape design §4 expects to work
// (Shell "empty = fall back to the global default_shell").
func defaultShellForNewSession(cfg *configStore) (string, error) {
	var found string
	shellPriorityOrder(cfg, func(shell string) bool {
		if shell == "" {
			return true
		}
		path, err := exec.LookPath(shell)
		if err != nil {
			return true
		}
		found = path
		return false
	})
	if found == "" {
		return "", fmt.Errorf("no shell found on this machine")
	}
	return found, nil
}

// sendResult queues the TypeSessionCreated reply. Runs on the worker
// goroutine (never the read loop), so unlike sendBusy it can afford to
// block on h.out — but only up to THIS connection's shutdown (connCtx), not
// the app's own lifetime (forkCtx): once the connection that asked is gone,
// nothing is left to deliver the reply to, and blocking on the app-lifetime
// context here would pin this goroutine (and, while sessionCreateConcurrency
// caps it, one of a bounded number of slots) for the life of the process
// instead of just the life of the connection.
func (h *sessionCreateHandler) sendResult(requestID string, ok bool, sid uuid.UUID, errStr string) {
	payload := proto.SessionCreatedPayload{RequestID: requestID, OK: ok, Error: errStr}
	frameSessionID := uuid.Nil
	if ok {
		payload.SessionID = sid.String()
		frameSessionID = sid
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logWarn("uplink", "session_create result marshal failed request_id=%s error=%v", requestID, err)
		return
	}
	frame := proto.Frame{Type: proto.TypeSessionCreated, SessionID: frameSessionID, Payload: body}
	select {
	case h.out <- frame:
	case <-h.connCtx.Done():
	}
}
