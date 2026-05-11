package session

import (
	"sync"

	"github.com/google/uuid"
)

// Registry holds all live sessions, keyed by id.
type Registry struct {
	mu         sync.RWMutex
	sessions   map[uuid.UUID]*Session
	changeSubs map[*ChangeSubscriber]struct{}
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		sessions:   make(map[uuid.UUID]*Session),
		changeSubs: make(map[*ChangeSubscriber]struct{}),
	}
}

// ChangeSubscriber receives coalesced notifications whenever the registry's
// session list or advertised session metadata changes.
type ChangeSubscriber struct {
	reg    *Registry
	ch     chan struct{}
	closed bool
	mu     sync.Mutex
}

// C returns the notification channel. Multiple changes may coalesce into one
// signal; consumers should always read a fresh full snapshot after receiving.
func (s *ChangeSubscriber) C() <-chan struct{} { return s.ch }

// Close unregisters this subscriber. It is safe to call more than once.
func (s *ChangeSubscriber) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.reg.mu.Lock()
	delete(s.reg.changeSubs, s)
	s.reg.mu.Unlock()
}

// SubscribeChanges registers for coalesced registry change notifications.
func (r *Registry) SubscribeChanges() *ChangeSubscriber {
	sub := &ChangeSubscriber{reg: r, ch: make(chan struct{}, 1)}
	r.mu.Lock()
	r.changeSubs[sub] = struct{}{}
	r.mu.Unlock()
	return sub
}

// NotifyChange tells subscribers to refresh their full session list snapshot.
func (r *Registry) NotifyChange() {
	r.mu.RLock()
	subs := make([]*ChangeSubscriber, 0, len(r.changeSubs))
	for sub := range r.changeSubs {
		subs = append(subs, sub)
	}
	r.mu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.ch <- struct{}{}:
		default:
		}
	}
}

// Add registers a session. If one with the same id exists it is replaced
// (and the old one closed) to support agent reconnect with the same id.
func (r *Registry) Add(s *Session) {
	r.mu.Lock()
	old, ok := r.sessions[s.ID]
	r.sessions[s.ID] = s
	r.mu.Unlock()
	if ok && old != s {
		old.Close()
	}
	r.NotifyChange()
}

// Get returns the session with the given id, or (nil, false).
func (r *Registry) Get(id uuid.UUID) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Remove deletes a session by id and closes it. Safe to call multiple times.
func (r *Registry) Remove(id uuid.UUID) {
	r.mu.Lock()
	s, ok := r.sessions[id]
	if ok {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if ok {
		s.Close()
		r.NotifyChange()
	}
}

// List returns a snapshot of all sessions.
func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}
