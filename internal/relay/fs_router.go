package relay

import (
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type fsRouteKey struct {
	sessionID uuid.UUID
	id        string
}

type fsClientRoute struct {
	out        chan<- proto.Frame
	op         string
	watchID    string
	onOverflow func()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fsRouteKey{sessionID: sessionID, id: request.RequestID}
	if _, exists := r.requests[key]; exists {
		return false
	}
	r.requests[key] = fsClientRoute{
		out:        out,
		op:         request.Op,
		watchID:    request.WatchID,
		onOverflow: onOverflow,
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

func (r *fsRouter) routeResponse(f proto.Frame) bool {
	// Segment 0 only: the relay holds no key and must not see paths or
	// file bytes. request_id / ok / watch_id are all it needs to route.
	var payload proto.FSResponsePayload
	if err := proto.DecodeFSHead(f.Payload, &payload); err != nil || payload.RequestID == "" {
		return false
	}

	key := fsRouteKey{sessionID: f.SessionID, id: payload.RequestID}
	r.mu.Lock()
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
