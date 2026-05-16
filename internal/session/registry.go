package session

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

// ErrSessionOwnerMismatch is returned by Registry.Add when a session with the
// same ID already exists with a non-empty OwnerUserID that differs from the
// OwnerUserID on the incoming session. This enforces the owner-binding
// invariant: once a session is bound to a user, ownership is immutable for the
// lifetime of the registry entry.
var ErrSessionOwnerMismatch = errors.New("session: owner mismatch on duplicate id")

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

// Add registers a session. Three cases:
//
//  1. No existing entry: the session is stored and returned.
//  2. Existing entry with the same (or empty) OwnerUserID: the existing
//     session is returned unchanged (idempotent re-publish, preserving
//     red-line #3 dedup-by-session-id behaviour).
//  3. Existing entry with a non-empty OwnerUserID that differs from s's
//     non-empty OwnerUserID: ErrSessionOwnerMismatch is returned and the
//     registry is not modified.
//
// Legacy callers that do not set OwnerUserID (empty string) are always
// accepted so that existing paths continue to work without modification.
func (r *Registry) Add(s *Session) (*Session, error) {
	r.mu.Lock()
	existing, ok := r.sessions[s.ID]
	if ok {
		// Owner-binding invariant: reject when both sides have distinct non-empty
		// owner IDs. Empty on either side is treated as "legacy / unset".
		if existing.OwnerUserID != "" && s.OwnerUserID != "" && existing.OwnerUserID != s.OwnerUserID {
			r.mu.Unlock()
			return nil, ErrSessionOwnerMismatch
		}
		// Same owner (or one side is empty): idempotent re-publish — return the
		// existing session without replacing it.
		r.mu.Unlock()
		return existing, nil
	}
	r.sessions[s.ID] = s
	r.mu.Unlock()
	r.NotifyChange()
	return s, nil
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
