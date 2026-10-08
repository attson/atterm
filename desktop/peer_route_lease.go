package main

import (
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// peerHostRouteLeaseKey identifies one logical Peer terminal attachment.
// Different devices and panes may still attach to the same session, while a
// route change for the same authenticated pane supersedes its old transport.
type peerHostRouteLeaseKey struct {
	remotePeerID     string
	sessionID        uuid.UUID
	clientInstanceID string
}

func (a *App) currentPeerHostRouteLease(key peerHostRouteLeaseKey) *peerHostAttempt {
	if a == nil {
		return nil
	}
	a.peerHostLeaseMu.Lock()
	defer a.peerHostLeaseMu.Unlock()
	return a.peerHostLeases[key]
}

func (a *App) claimPeerHostRouteLease(key peerHostRouteLeaseKey, expected, next *peerHostAttempt) (*peerHostAttempt, bool) {
	if a == nil || next == nil {
		return nil, false
	}
	a.peerHostLeaseMu.Lock()
	defer a.peerHostLeaseMu.Unlock()
	if a.peerHostLeases == nil {
		a.peerHostLeases = make(map[peerHostRouteLeaseKey]*peerHostAttempt)
	}
	current := a.peerHostLeases[key]
	// ReplacingSubscriber closes the old subscriber before this claim. Its
	// cleanup may therefore have released expected already, but no unrelated
	// route may have taken its place.
	if current != expected && (expected == nil || current != nil) {
		return current, false
	}
	a.peerHostLeases[key] = next
	if current == nil {
		current = expected
	}
	return current, true
}

func (a *App) releasePeerHostRouteLease(key peerHostRouteLeaseKey, owner *peerHostAttempt) {
	if a == nil || owner == nil {
		return
	}
	a.peerHostLeaseMu.Lock()
	if a.peerHostLeases[key] == owner {
		delete(a.peerHostLeases, key)
	}
	a.peerHostLeaseMu.Unlock()
}

func (a *peerHostAttempt) currentRouteLease() bool {
	if a == nil || a.host == nil || a.host.app == nil {
		return false
	}
	a.mu.Lock()
	closed := a.closed
	held := a.leaseHeld
	key := a.leaseKey
	a.mu.Unlock()
	return !closed && held && a.host.app.currentPeerHostRouteLease(key) == a
}

func (a *peerHostAttempt) currentSubscriberFor(sessionID uuid.UUID) *session.Subscriber {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.sessionID != sessionID {
		return nil
	}
	return a.sub
}
