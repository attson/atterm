package relay

import (
	"encoding/json"
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type fsRouteKey struct {
	sessionID uuid.UUID
	id        string
}

// fsRouter keeps the per-client routes needed by remote filesystem RPC. FS
// frames cannot use Session.Broadcast because every response and watch event
// belongs to its original requesting client only.
type fsRouter struct {
	mu       sync.Mutex
	requests map[fsRouteKey]chan<- proto.Frame
	watches  map[fsRouteKey]chan<- proto.Frame
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
		requests: make(map[fsRouteKey]chan<- proto.Frame),
		watches:  make(map[fsRouteKey]chan<- proto.Frame),
	}
}

func (r *fsRouter) registerRequest(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) bool {
	if requestID == "" || out == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fsRouteKey{sessionID: sessionID, id: requestID}
	if _, exists := r.requests[key]; exists {
		return false
	}
	r.requests[key] = out
	return true
}

func (r *fsRouter) unregisterRequest(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) {
	key := fsRouteKey{sessionID: sessionID, id: requestID}
	r.mu.Lock()
	if r.requests[key] == out {
		delete(r.requests, key)
	}
	r.mu.Unlock()
}

func (r *fsRouter) registerWatch(sessionID uuid.UUID, watchID string, out chan<- proto.Frame) {
	if watchID == "" || out == nil {
		return
	}
	r.mu.Lock()
	r.watches[fsRouteKey{sessionID: sessionID, id: watchID}] = out
	r.mu.Unlock()
}

func (r *fsRouter) unregisterClient(out chan<- proto.Frame) {
	if out == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, route := range r.requests {
		if route == out {
			delete(r.requests, key)
		}
	}
	for key, route := range r.watches {
		if route == out {
			delete(r.watches, key)
		}
	}
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
	var payload proto.FSResponsePayload
	if err := json.Unmarshal(f.Payload, &payload); err != nil || payload.RequestID == "" {
		return false
	}

	key := fsRouteKey{sessionID: f.SessionID, id: payload.RequestID}
	r.mu.Lock()
	out := r.requests[key]
	delete(r.requests, key)
	if out != nil && payload.OK && payload.WatchID != "" {
		r.watches[fsRouteKey{sessionID: f.SessionID, id: payload.WatchID}] = out
	}
	r.mu.Unlock()

	return sendFSFrame(out, f)
}

func (r *fsRouter) routeEvent(f proto.Frame) bool {
	var payload proto.FSEventPayload
	if err := json.Unmarshal(f.Payload, &payload); err != nil || payload.WatchID == "" {
		return false
	}

	r.mu.Lock()
	out := r.watches[fsRouteKey{sessionID: f.SessionID, id: payload.WatchID}]
	r.mu.Unlock()

	return sendFSFrame(out, f)
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
