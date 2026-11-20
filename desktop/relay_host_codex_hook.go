package main

import "github.com/google/uuid"

// ResolveCodexSession returns the sole active atterm session whose current AI
// resolver generation owns aiSessionID. A shared Codex app-server can emit hook
// events for many terminal clients from one daemon environment, so zero or
// multiple matches must fail closed rather than guessing an owner.
func (h *relayHost) ResolveCodexSession(aiSessionID string) (uuid.UUID, bool) {
	if h == nil || aiSessionID == "" {
		return uuid.Nil, false
	}

	h.mu.Lock()
	active := make(map[uuid.UUID]*activeSession, len(h.sessions))
	for sid, session := range h.sessions {
		active[sid] = session
	}
	h.mu.Unlock()

	match := uuid.Nil
	for sid, session := range active {
		if session == nil || session.matchesCodexConversation == nil || !session.matchesCodexConversation(aiSessionID) {
			continue
		}
		if match != uuid.Nil {
			return uuid.Nil, false
		}
		match = sid
	}
	return match, match != uuid.Nil
}
