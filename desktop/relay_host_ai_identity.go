package main

import "github.com/google/uuid"

// ApplyHookAISessionIdentity accepts an agent-native recovery credential from
// the localhost hook bridge. The active session owns generation validation:
// a hook for an old process must not revive a credential after a newer AI
// command has become authoritative in the same PTY.
func (h *relayHost) ApplyHookAISessionIdentity(sid uuid.UUID, kind, aiSessionID string) {
	h.mu.Lock()
	active := h.sessions[sid]
	h.mu.Unlock()
	if active == nil || active.applyHookAIIdentity == nil {
		return
	}
	active.applyHookAIIdentity(kind, aiSessionID)
}
