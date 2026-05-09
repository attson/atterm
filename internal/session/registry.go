package session

import (
	"sync"

	"github.com/google/uuid"
)

// Registry holds all live sessions, keyed by id.
type Registry struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{sessions: make(map[uuid.UUID]*Session)}
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
