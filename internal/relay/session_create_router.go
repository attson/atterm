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

// hostRoute is where a connected desktop uplink registers itself, keyed by
// AnnouncePayload.HostID — present the moment ANNOUNCE lands, independent of
// the mirrored sessions list. That is what makes routing by host work even
// for a desktop with zero open sessions (design doc §3). ownerUserID is
// carried alongside so a TypeSessionCreate from a different user's client
// can be refused without a second lookup.
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
	// registeredAt anchors the TTL sweep; see reapExpiredLocked.
	registeredAt time.Time
}

// sessionCreateRouter holds two independent tables: hosts (uplinks,
// registered at ANNOUNCE and keyed by host_id) and requests (outstanding
// TypeSessionCreate calls, keyed by request_id). Neither can use
// Session.Broadcast: a host route exists before any session does, and a
// request's response belongs to one client only.
type sessionCreateRouter struct {
	mu       sync.Mutex
	hosts    map[string]hostRoute
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
		hosts:    make(map[string]hostRoute),
		requests: make(map[string]sessionCreateRequestRoute),
	}
}

// registerHost records an uplink's ANNOUNCE so a later TypeSessionCreate can
// find it by host_id. Called once per uplink connection, at the same point
// handleUplink already has ann.HostID and ownerUserID in hand — this reuses
// that fact rather than building a second index that could drift from it.
func (r *sessionCreateRouter) registerHost(hostID string, out chan<- proto.Frame, ownerUserID string) {
	if hostID == "" || out == nil {
		return
	}
	r.mu.Lock()
	r.hosts[hostID] = hostRoute{out: out, ownerUserID: ownerUserID}
	r.mu.Unlock()
}

// unregisterHost removes hostID's route, but only if it still belongs to
// this connection's out channel. Without that guard, a stale connection's
// teardown could race a fresher reconnection for the same host id and
// delete the newer, still-live registration.
func (r *sessionCreateRouter) unregisterHost(hostID string, out chan<- proto.Frame) {
	if hostID == "" || out == nil {
		return
	}
	r.mu.Lock()
	if route, ok := r.hosts[hostID]; ok && route.out == out {
		delete(r.hosts, hostID)
	}
	r.mu.Unlock()
}

// lookupHost returns the uplink registered for hostID, if any is currently
// connected.
func (r *sessionCreateRouter) lookupHost(hostID string) (hostRoute, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	route, ok := r.hosts[hostID]
	return route, ok
}

// registerRequest reserves requestID for clientOut, remembering fromHost (the
// uplink the request was forwarded to) so routeResponse can verify the reply
// came from the same connection. Returns false on a duplicate request id, the
// same contract fs_router's registerRequestRoute uses.
func (r *sessionCreateRouter) registerRequest(requestID string, clientOut, fromHost chan<- proto.Frame) bool {
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
	r.requests[requestID] = sessionCreateRequestRoute{out: clientOut, fromHost: fromHost, registeredAt: now}
	return true
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

	return sendSessionCreateFrame(route.out, f)
}

func sendSessionCreateFrame(out chan<- proto.Frame, f proto.Frame) bool {
	if out == nil {
		return false
	}
	select {
	case out <- f:
		return true
	default:
		return false
	}
}
