package relay

import (
	"encoding/json"
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
	if items == nil {
		items = []userstore.PreferenceItem{}
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items})
}

// nowMs returns the server's current ms epoch. Indirection lets tests
// inject a clock if needed.
func nowMs() int64 { return time.Now().UnixMilli() }

type putPreferencesRequest struct {
	Items []struct {
		Key             string          `json:"key"`
		Value           json.RawMessage `json:"value"`
		ClientUpdatedAt int64           `json:"client_updated_at"`
	} `json:"items"`
}

func (a *AuthServer) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body putPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	items := make([]userstore.PreferenceItem, 0, len(body.Items))
	for _, it := range body.Items {
		items = append(items, userstore.PreferenceItem{
			Key:       it.Key,
			ValueJSON: it.Value,
			UpdatedAt: it.ClientUpdatedAt,
		})
	}

	result, err := a.Store.SetUserPreferences(r.Context(), user.ID, nowMs(), items)
	if err != nil {
		switch {
		case errorsIsUnknown(err):
			writeError(w, http.StatusBadRequest, "unknown_key")
		case errorsIsInvalid(err):
			writeError(w, http.StatusBadRequest, "invalid_value")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	if result == nil {
		result = []userstore.PreferenceItem{}
	}
	if len(body.Items) > 0 && a.OnPreferencesChanged != nil {
		a.OnPreferencesChanged(user.ID)
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": result})
}

func errorsIsUnknown(err error) bool {
	for err != nil {
		if err == userstore.ErrUnknownPreferenceKey {
			return true
		}
		type w interface{ Unwrap() error }
		u, ok := err.(w)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func errorsIsInvalid(err error) bool {
	for err != nil {
		if err == userstore.ErrInvalidPreferenceValue {
			return true
		}
		type w interface{ Unwrap() error }
		u, ok := err.(w)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
