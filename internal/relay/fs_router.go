package relay

import (
	"sync"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// fsRequestTTL bounds how long a request route waits for a response before
// the reaper treats it as abandoned.
//
// It must stay comfortably above the slowest legitimate FS operation: on a
// local filesystem that's microseconds, but SFTP adds a network round trip
// plus queueing behind the per-session bounded worker pool (see design doc
// mechanism 4.1) ahead of the actual op. 90s gives real margin above the
// spec's suggested 60s floor without leaving a genuinely dead request
// registered for anywhere near a client's session lifetime.
const fsRequestTTL = 90 * time.Second

type fsRouteKey struct {
	sessionID uuid.UUID
	id        string
}

type fsClientRoute struct {
	out        chan<- proto.Frame
	op         string
	watchID    string
	onOverflow func()
	// registeredAt is only meaningful for entries in fsRouter.requests: it
	// is what the reaper compares against fsRequestTTL. Watch routes carry
	// a zero value and are never reaped by it — see registerWatch.
	registeredAt time.Time
}

type fsOwnedWatch struct {
	sessionID uuid.UUID
	watchID   string
}

// fsRouter keeps the per-client routes needed by remote filesystem RPC. FS
// frames cannot use Session.Broadcast because every response and watch event
// belongs to its original requesting client only.
type fsRouter struct {
	mu       sync.Mutex
	requests map[fsRouteKey]fsClientRoute
	watches  map[fsRouteKey]fsClientRoute
}

// Keep router allocation lazy so every Server, including direct test-only
// literals, receives an independent router before first use.
var serverFSRouters sync.Map // map[*Server]*fsRouter

func (s *Server) fsRoutes() *fsRouter {
	if existing, ok := serverFSRouters.Load(s); ok {
		return existing.(*fsRouter)
	}
	routes := newFSRouter()
	actual, _ := serverFSRouters.LoadOrStore(s, routes)
	return actual.(*fsRouter)
}

func newFSRouter() *fsRouter {
	return &fsRouter{
		requests: make(map[fsRouteKey]fsClientRoute),
		watches:  make(map[fsRouteKey]fsClientRoute),
	}
}

func (r *fsRouter) registerRequest(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) bool {
	return r.registerRequestRoute(sessionID, proto.FSRequestPayload{RequestID: requestID}, out, nil)
}

func (r *fsRouter) registerRequestRoute(sessionID uuid.UUID, request proto.FSRequestPayload, out chan<- proto.Frame, onOverflow func()) bool {
	if request.RequestID == "" || out == nil {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapExpiredRequestsLocked(now)
	key := fsRouteKey{sessionID: sessionID, id: request.RequestID}
	if _, exists := r.requests[key]; exists {
		return false
	}
	r.requests[key] = fsClientRoute{
		out:          out,
		op:           request.Op,
		watchID:      request.WatchID,
		onOverflow:   onOverflow,
		registeredAt: now,
	}
	return true
}

func (r *fsRouter) unregisterRequest(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) {
	key := fsRouteKey{sessionID: sessionID, id: requestID}
	r.mu.Lock()
	if route, ok := r.requests[key]; ok && route.out == out {
		delete(r.requests, key)
	}
	r.mu.Unlock()
}

func (r *fsRouter) registerWatch(sessionID uuid.UUID, watchID string, out chan<- proto.Frame) {
	if watchID == "" || out == nil {
		return
	}
	r.mu.Lock()
	r.watches[fsRouteKey{sessionID: sessionID, id: watchID}] = fsClientRoute{out: out}
	r.mu.Unlock()
}

func (r *fsRouter) clientOwnsWatch(sessionID uuid.UUID, watchID string, out chan<- proto.Frame) bool {
	if watchID == "" || out == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	route, ok := r.watches[fsRouteKey{sessionID: sessionID, id: watchID}]
	return ok && route.out == out
}

func (r *fsRouter) unregisterClient(out chan<- proto.Frame) {
	if out == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, route := range r.requests {
		if route.out == out {
			delete(r.requests, key)
		}
	}
	for key, route := range r.watches {
		if route.out == out {
			delete(r.watches, key)
		}
	}
}

func (r *fsRouter) clientWatches(out chan<- proto.Frame) []fsOwnedWatch {
	if out == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	watches := make([]fsOwnedWatch, 0)
	for key, route := range r.watches {
		if route.out == out {
			watches = append(watches, fsOwnedWatch{sessionID: key.sessionID, watchID: key.id})
		}
	}
	return watches
}

func (r *fsRouter) unregisterSession(sessionID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.requests {
		if key.sessionID == sessionID {
			delete(r.requests, key)
		}
	}
	for key := range r.watches {
		if key.sessionID == sessionID {
			delete(r.watches, key)
		}
	}
}

// reapExpiredRequests sweeps and logs request routes older than
// fsRequestTTL, acquiring the lock itself. It is not run on a timer: the
// router has no owning goroutine to hang one off (fsRouter instances are
// created lazily per *Server via serverFSRouters, including bare literals
// in tests), and a timer racing a response under a separate lock acquisition
// is exactly the kind of race this task must not introduce. Instead every
// call that already touches the requests map — register, and response
// routing — sweeps first, for free, under the lock it already holds. The
// tradeoff: a request that goes stale on a router nobody touches again
// (e.g. the client never issues another FS op and never disconnects) stays
// registered past its TTL until something does touch the map. That is
// bounded by the existing unregisterClient/unregisterSession fallback, same
// as before this change.
//
// Only requests are reaped. Watch routes are a different kind of
// registration — a live subscription the client explicitly owns until
// unwatch_dir or disconnect — and applying a single-shot RPC TTL to it would
// silently kill directory live-updates while the user still has the folder
// open, which is worse than the leak being fixed here.
func (r *fsRouter) reapExpiredRequests(now time.Time) {
	r.mu.Lock()
	r.reapExpiredRequestsLocked(now)
	r.mu.Unlock()
}

// reapExpiredRequestsLocked is reapExpiredRequests' body; callers must hold r.mu.
func (r *fsRouter) reapExpiredRequestsLocked(now time.Time) {
	for key, route := range r.requests {
		if now.Sub(route.registeredAt) < fsRequestTTL {
			continue
		}
		delete(r.requests, key)
		logging.Warn("relay-fs", "reaped expired request session=%s request_id=%s op=%s age=%s", key.sessionID, key.id, route.op, now.Sub(route.registeredAt))
	}
}

func (r *fsRouter) routeResponse(f proto.Frame) bool {
	// Segment 0 only: the relay holds no key and must not see paths or
	// file bytes. request_id / ok / watch_id are all it needs to route.
	var payload proto.FSResponsePayload
	if err := proto.DecodeFSHead(f.Payload, &payload); err != nil || payload.RequestID == "" {
		return false
	}

	key := fsRouteKey{sessionID: f.SessionID, id: payload.RequestID}
	r.mu.Lock()
	// Sweep before the lookup, under the same lock: a response for `key`
	// that is still within TTL can never be swept out from under this
	// lookup, because the sweep uses the identical age comparison. There is
	// no separate reaper goroutine that could race this delete.
	r.reapExpiredRequestsLocked(time.Now())
	route, ok := r.requests[key]
	if !ok {
		r.mu.Unlock()
		return false
	}
	delete(r.requests, key)
	if payload.OK {
		switch route.op {
		case "watch_dir":
			if payload.WatchID != "" {
				watchKey := fsRouteKey{sessionID: f.SessionID, id: payload.WatchID}
				if existing, exists := r.watches[watchKey]; exists && existing.out != route.out {
					// Replace the whole frame with a single-segment error
					// rather than rewriting the head in place: OK=false
					// means the sealed segments carry nothing worth
					// preserving, and the relay cannot re-seal anyway.
					if errBody, encErr := proto.EncodeFSHead(proto.FSResponsePayload{
						RequestID: payload.RequestID,
						OK:        false,
						Error:     "duplicate_watch_id",
					}); encErr == nil {
						f.Payload = errBody
					}
				} else {
					route.watchID = payload.WatchID
					r.watches[watchKey] = route
				}
			}
		case "unwatch_dir":
			if route.watchID != "" {
				watchKey := fsRouteKey{sessionID: f.SessionID, id: route.watchID}
				if watchRoute, exists := r.watches[watchKey]; exists && watchRoute.out == route.out {
					delete(r.watches, watchKey)
				}
			}
		}
	}
	r.mu.Unlock()

	return sendFSFrameToRoute(route, f)
}

func (r *fsRouter) routeEvent(f proto.Frame) bool {
	var payload proto.FSEventPayload
	if err := proto.DecodeFSHead(f.Payload, &payload); err != nil || payload.WatchID == "" {
		return false
	}

	r.mu.Lock()
	route := r.watches[fsRouteKey{sessionID: f.SessionID, id: payload.WatchID}]
	r.mu.Unlock()

	return sendFSFrameToRoute(route, f)
}

func sendFSFrameToRoute(route fsClientRoute, f proto.Frame) bool {
	if sendFSFrame(route.out, f) {
		return true
	}
	if route.out != nil && route.onOverflow != nil {
		route.onOverflow()
	}
	return false
}

func sendFSFrame(out chan<- proto.Frame, f proto.Frame) bool {
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
