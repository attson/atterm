package relay

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

const inputHistoryRequestTTL = 30 * time.Second

type inputHistoryRouteKey struct {
	sessionID uuid.UUID
	requestID string
}

type inputHistoryRoute struct {
	out          chan<- proto.Frame
	registeredAt time.Time
}

type inputHistoryRouter struct {
	mu     sync.Mutex
	routes map[inputHistoryRouteKey]inputHistoryRoute
}

var serverInputHistoryRouters sync.Map // map[*Server]*inputHistoryRouter

func (s *Server) inputHistoryRoutes() *inputHistoryRouter {
	if existing, ok := serverInputHistoryRouters.Load(s); ok {
		return existing.(*inputHistoryRouter)
	}
	router := newInputHistoryRouter()
	actual, _ := serverInputHistoryRouters.LoadOrStore(s, router)
	return actual.(*inputHistoryRouter)
}

func newInputHistoryRouter() *inputHistoryRouter {
	return &inputHistoryRouter{routes: make(map[inputHistoryRouteKey]inputHistoryRoute)}
}

func (r *inputHistoryRouter) register(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) bool {
	if sessionID == uuid.Nil || requestID == "" || out == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapLocked(time.Now())
	key := inputHistoryRouteKey{sessionID: sessionID, requestID: requestID}
	if _, exists := r.routes[key]; exists {
		return false
	}
	r.routes[key] = inputHistoryRoute{out: out, registeredAt: time.Now()}
	return true
}

func (r *inputHistoryRouter) unregister(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) {
	r.mu.Lock()
	key := inputHistoryRouteKey{sessionID: sessionID, requestID: requestID}
	if route, ok := r.routes[key]; ok && route.out == out {
		delete(r.routes, key)
	}
	r.mu.Unlock()
}

func (r *inputHistoryRouter) route(frame proto.Frame) bool {
	var response proto.InputHistoryResponsePayload
	if json.Unmarshal(frame.Payload, &response) != nil || response.RequestID == "" {
		return false
	}
	key := inputHistoryRouteKey{sessionID: frame.SessionID, requestID: response.RequestID}
	r.mu.Lock()
	r.reapLocked(time.Now())
	route, ok := r.routes[key]
	if ok {
		delete(r.routes, key)
	}
	r.mu.Unlock()
	return ok && sendFSFrame(route.out, frame)
}

func (r *inputHistoryRouter) unregisterClient(out chan<- proto.Frame) {
	r.mu.Lock()
	for key, route := range r.routes {
		if route.out == out {
			delete(r.routes, key)
		}
	}
	r.mu.Unlock()
}

func (r *inputHistoryRouter) unregisterSession(sessionID uuid.UUID) {
	r.mu.Lock()
	for key := range r.routes {
		if key.sessionID == sessionID {
			delete(r.routes, key)
		}
	}
	r.mu.Unlock()
}

func (r *inputHistoryRouter) has(sessionID uuid.UUID, requestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.routes[inputHistoryRouteKey{sessionID: sessionID, requestID: requestID}]
	return ok
}

func (r *inputHistoryRouter) reapLocked(now time.Time) {
	for key, route := range r.routes {
		if now.Sub(route.registeredAt) >= inputHistoryRequestTTL {
			delete(r.routes, key)
		}
	}
}
