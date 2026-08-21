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
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// maxSessionsPerHost is design §4's "per-host session cap": above this many
// concurrently-live sessions, a mobile create request is refused with a
// distinct error rather than forking another PTY. Every other session-
// creation path on this desktop is gated by someone physically sitting at
// the keyboard clicking "new tab"; TypeSessionCreate is the first one
// reachable purely over the network (relay -> uplink), so it is the one
// path that needs a hard ceiling — a buggy or malicious caller retrying
// past its own 30s timeout many times must not be able to fork the desktop
// into the ground.
const maxSessionsPerHost = 32

// sessionCreateMaxInFlight bounds concurrent session-create work to 1 per
// uplink connection.
//
// SessionCreatePayload deliberately carries no requesting-client identity —
// just request_id/host_id/profile_id (see its doc comment in
// internal/proto/frame.go; that emptiness is the same guarantee that keeps
// a phone from smuggling a profile body). That means this handler has no
// way to tell "the same phone tapped twice" apart from "two different
// phones asked at once": the uplink connection itself is the only
// requesting-client boundary it can observe, since one desktop keeps
// exactly one relay connection for every client that talks to it. Capping
// at 1 here is the tightest bound available on that boundary — a second
// request arriving while the first is still forking gets refused
// immediately (design §4: "a phone that taps twice does not fork two
// shells"), never silently queued behind the first.
const sessionCreateMaxInFlight = 1

const (
	// sessionCreateErrUnknownProfile answers a profile_id that does not
	// match anything in local config. Never silently substituted for the
	// configured default (that leniency belongs to resolveSessionProfile's
	// explicit/default precedence for locally-typed defaults going stale —
	// see relay_host.go) — a phone asked for something specific by id, and
	// handing it a different profile it never chose would be a worse
	// surprise than telling it plainly that the id is gone.
	sessionCreateErrUnknownProfile = "unknown_profile"
	// sessionCreateErrCapReached answers a request arriving once
	// maxSessionsPerHost is already live.
	sessionCreateErrCapReached = "session_cap_reached"
	// sessionCreateErrBusy answers a request arriving while another create
	// is already in flight on this connection (sessionCreateMaxInFlight).
	sessionCreateErrBusy = "session_create_busy"
)

// sessionCreateHandler runs TypeSessionCreate requests off the uplink read
// loop — the same reason fsWorkerPool (remote_fs.go) exists: that loop also
// carries every session's keystrokes, and forking a PTY talks to the OS
// (exec, pty allocation, cwd stat), which is not instant. Handling it inline
// would stall every other session on this uplink for the duration of one
// fork.
type sessionCreateHandler struct {
	ctx  context.Context
	out  chan<- proto.Frame
	host *relayHost
	cap  int

	// newSession forks the session. Defaults to host.NewSession; tests
	// override it (via uplink.sessionCreateExec) to control PTY-fork timing
	// without spawning a real shell, the same role fsExec plays for
	// filesystem requests.
	newSession func(ctx context.Context, req NewSessionReq) (uuid.UUID, error)

	mu       sync.Mutex
	inFlight int

	// testHook, when non-nil, runs at the top of run() before any real
	// work — nil in production. Lets tests hold a create "in flight"
	// deterministically to exercise sessionCreateMaxInFlight without
	// racing real PTY-spawn timing, mirroring the fsExec test seam
	// uplink.go already uses for the same reason on the FS path.
	testHook func()
}

// newSessionCreateHandler builds a handler bound to one uplink connection's
// lifetime (ctx) and outbound queue (out). sessionCap<=0 uses
// maxSessionsPerHost — the same "0 means default" convention
// newFSWorkerPool's limit parameter uses — so tests can install a small cap
// without spawning dozens of real PTYs to reach it.
func newSessionCreateHandler(ctx context.Context, out chan<- proto.Frame, host *relayHost, sessionCap int) *sessionCreateHandler {
	if sessionCap <= 0 {
		sessionCap = maxSessionsPerHost
	}
	return &sessionCreateHandler{ctx: ctx, out: out, host: host, cap: sessionCap, newSession: host.NewSession}
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
	if h.inFlight >= sessionCreateMaxInFlight {
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

	var profiles []SessionProfile
	if h.host.cfg != nil {
		profiles = h.host.cfg.Get().Profiles
	}
	if _, ok := findProfileByID(profiles, req.ProfileID); !ok {
		h.sendResult(req.RequestID, false, uuid.Nil, sessionCreateErrUnknownProfile)
		return
	}
	if h.host.SessionCount() >= h.cap {
		h.sendResult(req.RequestID, false, uuid.Nil, sessionCreateErrCapReached)
		return
	}

	shell, err := defaultShellForNewSession(h.host.cfg)
	if err != nil {
		h.sendResult(req.RequestID, false, uuid.Nil, err.Error())
		return
	}

	// h.ctx is the uplink connection's own lifetime context — not a fresh
	// deadline scoped to this one request. The phone already times out at
	// 30s (design §4) and does not retry on timeout; a second, shorter
	// timeout here could fire first, mid-fork, and leave a PTY nobody
	// tracks while the phone is told only "timed out," never "it actually
	// started." NewSession failing loudly (bad cwd, unresolvable shell) is
	// still reported below — this is specifically about not racing the
	// phone's own patience with one of our own.
	sid, err := h.newSession(h.ctx, NewSessionReq{
		Command:   shell,
		ProfileID: req.ProfileID,
	})
	if err != nil {
		// NewSession already validates everything about the profile that
		// needs validating beyond "does the id exist" — a missing absolute
		// Shell degrades to `shell` above and a missing Cwd fails naming
		// the profile (relay_host.go). Surfacing err.Error() as-is here
		// avoids a second copy of that logic that could drift from it.
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
// TypeSessionCreate has no frontend in the loop, so this mirrors
// ListShells' own priority order directly: the configured default shell
// (unless "auto"), then $SHELL, then the first well-known shell actually
// on PATH. Without this, a session-create for a profile with no explicit
// Shell would hand NewSession an empty Command, and NewSession refuses that
// outright ("empty command") — exactly the profile shape design §4 expects
// to work (Shell "empty = fall back to the global default_shell").
func defaultShellForNewSession(cfg *configStore) (string, error) {
	try := func(shell string) (string, bool) {
		if shell == "" {
			return "", false
		}
		path, err := exec.LookPath(shell)
		if err != nil {
			return "", false
		}
		return path, true
	}
	if cfg != nil {
		if configured := cfg.Get().DefaultShellOrDefault(); configured != defaultShellAuto {
			if path, ok := try(configured); ok {
				return path, nil
			}
		}
	}
	candidates := []string{"bash", "zsh", "fish", "sh"}
	if runtime.GOOS == "windows" {
		candidates = windowsShellCandidates()
	} else if path, ok := try(os.Getenv("SHELL")); ok {
		return path, nil
	}
	for _, c := range candidates {
		if path, ok := try(c); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("no shell found on this machine")
}

// sendResult queues the TypeSessionCreated reply. Runs on the worker
// goroutine (never the read loop), so unlike sendBusy it can afford to
// block on h.out up to connection shutdown instead of dropping — nothing
// else is waiting behind this specific send.
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
	case <-h.ctx.Done():
	}
}
