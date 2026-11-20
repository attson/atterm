package main

import "github.com/google/uuid"

// ApplyHookAISessionIdentity accepts an agent-native recovery credential after
// the localhost hook bridge has resolved its conversation id to this exact
// active session. The active session still owns generation validation.
func (h *relayHost) ApplyHookAISessionIdentity(sid uuid.UUID, kind, aiSessionID string) {
	h.mu.Lock()
	active := h.sessions[sid]
	h.mu.Unlock()
	if active == nil {
		logDebug("ai-sid", "ignore hook identity session=%s kind=%s reason=session-not-active", sid, kind)
		return
	}
	if active.applyHookAIIdentity == nil {
		logDebug("ai-sid", "ignore hook identity session=%s kind=%s reason=handler-unavailable", sid, kind)
		return
	}
	active.applyHookAIIdentity(kind, aiSessionID)
}
