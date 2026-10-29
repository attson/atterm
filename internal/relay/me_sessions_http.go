package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// A connected client heartbeats every 30 seconds. Three missed heartbeats
// avoids flicker from short scheduler/DB stalls while keeping the UI useful
// as a presence indicator rather than a historical login list.
const sessionOnlineWindow = 90 * time.Second

func sessionIsOnline(lastSeen, now time.Time) bool {
	return !lastSeen.IsZero() && now.Sub(lastSeen) <= sessionOnlineWindow
}

// sessionRow is the JSON view sent to the client.
type sessionRow struct {
	IDHash     string `json:"id_hash"`
	UserAgent  string `json:"user_agent"`
	IPPrefix   string `json:"ip_prefix"`
	CreatedAt  int64  `json:"created_at"`   // unix milliseconds
	ExpiresAt  int64  `json:"expires_at"`   // unix milliseconds
	LastSeenAt int64  `json:"last_seen_at"` // unix milliseconds
	IsOnline   bool   `json:"is_online"`
	IsCurrent  bool   `json:"is_current"`
}

// handleListSessions implements GET /api/me/sessions. Returns the
// caller's sessions rows. Marks the row that matches the current
// bearer token as is_current=true so the UI hides Revoke on that row.
func (a *AuthServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	rows, err := a.Store.ListSessions(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Compute the current bearer token's hash so we can mark is_current.
	var currentHash string
	if tok := tokenFromRequest(r); tok != "" {
		currentHash = userstore.SessionHash(tok)
	}

	now := time.Now()
	out := make([]sessionRow, 0, len(rows))
	for _, s := range rows {
		out = append(out, sessionRow{
			IDHash:     s.IDHash,
			UserAgent:  s.UserAgent,
			IPPrefix:   s.IPPrefix,
			CreatedAt:  s.CreatedAt.UnixMilli(),
			ExpiresAt:  s.ExpiresAt.UnixMilli(),
			LastSeenAt: s.LastSeenAt.UnixMilli(),
			IsOnline:   sessionIsOnline(s.LastSeenAt, now),
			IsCurrent:  s.IDHash == currentHash,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleDeleteSession revokes a single session owned by the caller.
// Returns 204 if a row was deleted, 404 if no matching session
// belonged to this user. Cross-user attempts are indistinguishable
// from "doesn't exist".
func (a *AuthServer) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	idHash := r.PathValue("id_hash")
	if idHash == "" {
		http.Error(w, "missing id_hash", http.StatusBadRequest)
		return
	}
	deleted, err := a.Store.DeleteSessionByIDHash(r.Context(), p.UserID, idHash)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSignOutOthers deletes every session for the caller except the one
// matching the current bearer token. Returns 200 + {"deleted": N}.
func (a *AuthServer) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	tok := tokenFromRequest(r)
	if tok == "" {
		// requireSession already vetted this request, so the bearer token must
		// be present — this branch only survives as defence against future
		// wiring mistakes.
		http.Error(w, "current session token missing", http.StatusBadRequest)
		return
	}
	currentHash := userstore.SessionHash(tok)
	n, err := a.Store.DeleteOtherSessionsForUser(r.Context(), p.UserID, currentHash)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"deleted": n})
}
