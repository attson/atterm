package relay

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
)

// sessionCreateRequestTTL bounds how long a TypeSessionCreate route waits
// for the desktop's TypeSessionCreated response before the reaper treats it
// as abandoned.
//
// The phone gives up waiting at 30s and does not retry (design doc §4): a
// retried "start a shell" that actually succeeded the first time would leave
// an orphan process nobody asked for. This TTL must stay comfortably above
// that 30s so the relay never reaps a route the phone itself is still
// waiting on — reaping early would turn a merely-slow fork into a silently
// dropped answer. Unlike fs_router's fsRequestTTL there is no SFTP-style
// round-trip tax to budget for (forking a local PTY is fast), so the margin
// here is purely "stay above the client's own patience," not network
// latency.
const sessionCreateRequestTTL = 60 * time.Second

// hostKey namespaces the host table by owner: host_id alone is not a safe
// key. It is an opaque per-machine UUID (internal/appdir.HostID), but
// nothing stops two different users' machines from producing the same one —
// a cloned config directory or a shared ATTERM_HOST_ID env var reproduces it
// with no attacker involved at all. Keying on {ownerUserID, hostID} means
// two owners registering the identical host_id occupy two different map
// entries instead of one colliding one, so neither can evict the other's
// route (register) or delete the other's route out from under it
// (unregister, which also checks the out channel — see unregisterHost).
type hostKey struct {
	ownerUserID string
	hostID      string
}

// hostRoute is where a connected desktop uplink registers itself, keyed by
// AnnouncePayload.HostID — present the moment ANNOUNCE lands, independent of
// the mirrored sessions list. That is what makes routing by host work even
// for a desktop with zero open sessions (design doc §3).
type hostRoute struct {
	out         chan<- proto.Frame
	ownerUserID string
}

// sessionCreateRequestRoute is one outstanding TypeSessionCreate, keyed by
// RequestID alone. Unlike fs_router's {sessionID, requestID} key, a create
// request has no session yet — that's the thing being created — so
// RequestID must be globally unique across every outstanding request on its
// own, which the client already guarantees the same way it does for FS
// request ids.
type sessionCreateRequestRoute struct {
	// out is the requesting client's outbound channel. The eventual
	// TypeSessionCreated reply goes here and nowhere else — the same rule
	// fs_router.go enforces for TypeFSResponse, and for the same reason: a
	// broadcast would let any client on the relay see, or race, another
	// client's session creation.
	out chan<- proto.Frame
	// fromHost is the uplink this request was forwarded to. A
	// TypeSessionCreated response is only accepted from this exact
	// connection — routeResponse checks it — so one uplink cannot forge a
	// reply for a request the relay routed to a different uplink, even if
	// it guesses (or is handed) the request_id.
	fromHost chan<- proto.Frame
	// onOverflow fires when the requester's channel is full at delivery
	// time, mirroring fsClientRoute.onOverflow. Without this, a full
	// requester channel silently swallows the reply after the route has
	// already been deleted — the request_id can never be reused (it already
	// isn't registered), but the client gets no error and just waits out its
	// own 30s timeout instead of the overflow being noticed anywhere.
	onOverflow func()
	// registeredAt anchors the TTL sweep; see reapExpiredLocked.
	registeredAt time.Time
}

// sessionCreateRouter holds two independent tables: hosts (uplinks,
// registered at ANNOUNCE and keyed by owner+host_id) and requests
// (outstanding TypeSessionCreate calls, keyed by request_id). Neither can
// use Session.Broadcast: a host route exists before any session does, and a
// request's response belongs to one client only.
type sessionCreateRouter struct {
	mu       sync.Mutex
	hosts    map[hostKey]hostRoute
	requests map[string]sessionCreateRequestRoute
}

// Keep router allocation lazy so every Server, including direct test-only
// literals, receives an independent router before first use. Mirrors
// serverFSRouters in fs_router.go.
var serverSessionCreateRouters sync.Map // map[*Server]*sessionCreateRouter

func (s *Server) sessionCreateRoutes() *sessionCreateRouter {
	if existing, ok := serverSessionCreateRouters.Load(s); ok {
		return existing.(*sessionCreateRouter)
	}
	routes := newSessionCreateRouter()
	actual, _ := serverSessionCreateRouters.LoadOrStore(s, routes)
	return actual.(*sessionCreateRouter)
}

func newSessionCreateRouter() *sessionCreateRouter {
	return &sessionCreateRouter{
		hosts:    make(map[hostKey]hostRoute),
		requests: make(map[string]sessionCreateRequestRoute),
	}
}

// registerHost records an uplink's ANNOUNCE so a later TypeSessionCreate can
// find it by host_id. Called once per uplink connection, at the same point
// handleUplink already has ann.HostID and ownerUserID in hand — this reuses
// that fact rather than building a second index that could drift from it.
//
// This is a same-owner-only unconditional overwrite: within ownerUserID's own
// namespace, a second registration under the same host_id (e.g. an
// unclean-reconnect race, or a misconfigured second machine sharing an id)
// still replaces the earlier entry last-write-wins, exactly as before. What
// changed is that it can no longer replace a *different* owner's entry, since
// they now live under different keys.
func (r *sessionCreateRouter) registerHost(hostID string, out chan<- proto.Frame, ownerUserID string) {
	if hostID == "" || out == nil {
		return
	}
	r.mu.Lock()
	r.hosts[hostKey{ownerUserID: ownerUserID, hostID: hostID}] = hostRoute{out: out, ownerUserID: ownerUserID}
	r.mu.Unlock()
}

// unregisterHost removes hostID's route for ownerUserID, but only if it
// still belongs to this connection's out channel. Without that out-channel
// guard, a stale connection's teardown could race a fresher reconnection for
// the same {owner, host_id} and delete the newer, still-live registration.
func (r *sessionCreateRouter) unregisterHost(hostID, ownerUserID string, out chan<- proto.Frame) {
	if hostID == "" || out == nil {
		return
	}
	key := hostKey{ownerUserID: ownerUserID, hostID: hostID}
	r.mu.Lock()
	if route, ok := r.hosts[key]; ok && route.out == out {
		delete(r.hosts, key)
	}
	r.mu.Unlock()
}

// lookupHost returns the uplink registered for hostID under ownerUserID, if
// any is currently connected. Scoping the lookup by the caller's own
// ownerUserID (rather than looking up by hostID alone and comparing owners
// after the fact) means a miss is structurally indistinguishable from "wrong
// owner" — there is no separate comparison step left to get wrong.
func (r *sessionCreateRouter) lookupHost(hostID, ownerUserID string) (hostRoute, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	route, ok := r.hosts[hostKey{ownerUserID: ownerUserID, hostID: hostID}]
	return route, ok
}

// registerRequest reserves requestID for clientOut, remembering fromHost
// (the uplink the request was forwarded to) so routeResponse can verify the
// reply came from the same connection. It is the no-overflow-callback
// convenience wrapper fs_router.go also offers (registerRequest vs.
// registerRequestRoute) — used directly by tests that don't care about
// overflow handling.
func (r *sessionCreateRouter) registerRequest(requestID string, clientOut, fromHost chan<- proto.Frame) bool {
	return r.registerRequestRoute(requestID, clientOut, fromHost, nil)
}

// registerRequestRoute is registerRequest plus an onOverflow callback,
// invoked if the eventual response can't be delivered because clientOut is
// full. Returns false on a duplicate request id, the same contract
// fs_router's registerRequestRoute uses.
func (r *sessionCreateRouter) registerRequestRoute(requestID string, clientOut, fromHost chan<- proto.Frame, onOverflow func()) bool {
	if requestID == "" || clientOut == nil || fromHost == nil {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapExpiredLocked(now)
	if _, exists := r.requests[requestID]; exists {
		return false
	}
	r.requests[requestID] = sessionCreateRequestRoute{
		out:          clientOut,
		fromHost:     fromHost,
		onOverflow:   onOverflow,
		registeredAt: now,
	}
	return true
}

// hasOutstandingRequest reports whether clientOut already has a
// TypeSessionCreate awaiting a reply.
//
// This is the true per-CLIENT bound design §4's "a phone that taps twice
// does not fork two shells" needs: client identity — one outbound channel
// per connected client — only exists here, at the relay. A desktop keeps
// exactly one uplink connection shared by every client that talks to it, so
// a bound placed on the desktop side (sessionCreateConcurrency in
// desktop/session_create_handler.go) is necessarily per-desktop, not
// per-client; it exists only as a coarse safety valve against an unbounded
// or buggy/compromised relay, not as this dedup.
//
// Recovery is automatic: reapExpiredLocked's existing TTL sweep (run here,
// under the lock, before the scan) means a client is never stuck refused
// forever by a desktop that wedges mid-fork — the same sweep that already
// protects registerRequestRoute from a permanently-squatted request_id.
func (r *sessionCreateRouter) hasOutstandingRequest(clientOut chan<- proto.Frame) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapExpiredLocked(time.Now())
	for _, route := range r.requests {
		if route.out == clientOut {
			return true
		}
	}
	return false
}

// unregisterRequest removes requestID's route, but only if it still belongs
// to clientOut — the same self-check pattern as fs_router's unregisterRequest,
// so a caller that raced a response (which already deleted the entry) or a
// reused request id can't remove someone else's route.
func (r *sessionCreateRouter) unregisterRequest(requestID string, clientOut chan<- proto.Frame) {
	r.mu.Lock()
	if route, ok := r.requests[requestID]; ok && route.out == clientOut {
		delete(r.requests, requestID)
	}
	r.mu.Unlock()
}

// unregisterClient drops every outstanding request registered for out. Called
// when a client connection tears down, mirroring fs_router's unregisterClient.
func (r *sessionCreateRouter) unregisterClient(out chan<- proto.Frame) {
	if out == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, route := range r.requests {
		if route.out == out {
			delete(r.requests, id)
		}
	}
}

// reapExpiredRequests sweeps requests older than sessionCreateRequestTTL,
// acquiring the lock itself. As in fs_router.go, this is not run on a timer:
// there is no owning goroutine to hang one off (routers are created lazily
// per *Server, including bare literals in tests), and a timer racing a
// response under a separate lock acquisition is exactly the kind of race
// this router must not introduce. Instead every call that already touches
// the requests map — register, and response routing — sweeps first, for
// free, under the lock it already holds. That makes a response-vs-reap race
// structurally impossible: a response for a request still within TTL can
// never be swept out from under the lookup that immediately follows, because
// both use the identical age comparison under the same lock.
func (r *sessionCreateRouter) reapExpiredRequests(now time.Time) {
	r.mu.Lock()
	r.reapExpiredLocked(now)
	r.mu.Unlock()
}

func (r *sessionCreateRouter) reapExpiredLocked(now time.Time) {
	for id, route := range r.requests {
		if now.Sub(route.registeredAt) < sessionCreateRequestTTL {
			continue
		}
		delete(r.requests, id)
		logging.Warn("relay-session-create", "reaped expired request request_id=%s age=%s", id, now.Sub(route.registeredAt))
	}
}

// routeResponse delivers a TypeSessionCreated frame to the one client that
// issued the matching TypeSessionCreate, and to nobody else. fromUplink must
// be the out channel of the uplink connection the frame was actually read
// from: a response whose request_id names a route registered for a
// different uplink is refused rather than delivered, so one desktop cannot
// forge a reply for a request the relay routed to another desktop.
//
// The relay reads only RequestID here — SessionCreatedPayload carries no
// sealed segments to hold a key against; unlike FS frames this pair was
// never given an E2EE envelope (design doc §3), so a plain json.Unmarshal
// of the whole payload already is "the routing head."
func (r *sessionCreateRouter) routeResponse(f proto.Frame, fromUplink chan<- proto.Frame) bool {
	var payload proto.SessionCreatedPayload
	if err := json.Unmarshal(f.Payload, &payload); err != nil || payload.RequestID == "" {
		return false
	}

	r.mu.Lock()
	// Sweep before the lookup, under the same lock: see reapExpiredRequests.
	r.reapExpiredLocked(time.Now())
	route, ok := r.requests[payload.RequestID]
	if !ok {
		r.mu.Unlock()
		return false
	}
	if route.fromHost != fromUplink {
		r.mu.Unlock()
		logging.Warn("relay-session-create", "dropped response from wrong uplink request_id=%s", payload.RequestID)
		return false
	}
	delete(r.requests, payload.RequestID)
	r.mu.Unlock()

	// sendFSFrame is fs_router.go's identical non-blocking channel send —
	// shared rather than duplicated, since the two routers' delivery
	// mechanics don't diverge here.
	if sendFSFrame(route.out, f) {
		return true
	}
	if route.onOverflow != nil {
		route.onOverflow()
	}
	return false
}
