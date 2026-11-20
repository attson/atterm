package main

import (
	"testing"

	"github.com/google/uuid"
)

func TestRelayHostResolveCodexSessionRequiresUniqueConversationOwner(t *testing.T) {
	conversationID := "01a17b28-7aff-72c1-b292-a4510d537d46"
	firstID := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	secondID := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")

	h := &relayHost{sessions: map[uuid.UUID]*activeSession{
		firstID: {
			matchesCodexConversation: func(got string) bool { return got == conversationID },
		},
		secondID: {
			matchesCodexConversation: func(string) bool { return false },
		},
	}}

	if got, ok := h.ResolveCodexSession(conversationID); !ok || got != firstID {
		t.Fatalf("ResolveCodexSession() = %s, %v, want %s, true", got, ok, firstID)
	}
	if got, ok := h.ResolveCodexSession("01a17b28-7aff-72c1-b292-a4510d537d47"); ok || got != uuid.Nil {
		t.Fatalf("unbound ResolveCodexSession() = %s, %v, want nil, false", got, ok)
	}

	h.sessions[secondID].matchesCodexConversation = func(got string) bool { return got == conversationID }
	if got, ok := h.ResolveCodexSession(conversationID); ok || got != uuid.Nil {
		t.Fatalf("ambiguous ResolveCodexSession() = %s, %v, want nil, false", got, ok)
	}
}
