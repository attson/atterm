package relay

import (
	"encoding/json"
	"net/http"

	"github.com/attson/atterm/internal/userstore"
)

// sessionRow is the JSON view sent to the client.
type sessionRow struct {
	IDHash    string `json:"id_hash"`
	UserAgent string `json:"user_agent"`
	IPPrefix  string `json:"ip_prefix"`
	CreatedAt int64  `json:"created_at"` // unix milliseconds
	ExpiresAt int64  `json:"expires_at"` // unix milliseconds
	IsCurrent bool   `json:"is_current"`
}

// handleListSessions implements GET /api/me/sessions. Returns the
// caller's sessions rows. Marks the row that matches the current
// cookie as is_current=true so the UI hides Revoke on that row.
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

	// Compute the current cookie's hash so we can mark is_current.
	var currentHash string
	if c, cerr := r.Cookie("atterm_session"); cerr == nil && c.Value != "" {
		currentHash = userstore.SessionHash(c.Value)
	}

	out := make([]sessionRow, 0, len(rows))
	for _, s := range rows {
		out = append(out, sessionRow{
			IDHash:    s.IDHash,
			UserAgent: s.UserAgent,
			IPPrefix:  s.IPPrefix,
			CreatedAt: s.CreatedAt.UnixMilli(),
			ExpiresAt: s.ExpiresAt.UnixMilli(),
			IsCurrent: s.IDHash == currentHash,
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
// matching the current cookie. Returns 200 + {"deleted": N}.
func (a *AuthServer) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	c, err := r.Cookie("atterm_session")
	if err != nil || c.Value == "" {
		// requireUser already authed via cookie OR api token; without a
		// cookie we can't preserve "this device", so just error out.
		http.Error(w, "current session not cookie-based", http.StatusBadRequest)
		return
	}
	currentHash := userstore.SessionHash(c.Value)
	n, err := a.Store.DeleteOtherSessionsForUser(r.Context(), p.UserID, currentHash)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"deleted": n})
}
