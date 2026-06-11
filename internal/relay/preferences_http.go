package relay

import (
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// handleGetPreferences answers GET /api/me/preferences with the user's
// full preference state (possibly empty). Auth is enforced by
// requireSession when the route is registered.
func (a *AuthServer) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := a.Store.GetUserPreferences(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if items == nil { items = []userstore.PreferenceItem{} }
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items})
}

// nowMs returns the server's current ms epoch. Indirection lets tests
// inject a clock if needed.
func nowMs() int64 { return time.Now().UnixMilli() }
