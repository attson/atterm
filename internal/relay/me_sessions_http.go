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
// caller's web_sessions rows. Marks the row that matches the current
// cookie as is_current=true so the UI hides Revoke on that row.
func (a *AuthServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	rows, err := a.Store.ListUserWebSessions(r.Context(), p.UserID)
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
