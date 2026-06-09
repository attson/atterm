package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// handleSessionsSeenHTTP implements POST /api/sessions/seen. Body is either
// {"all": true} (mark every session the caller owns) or
// {"session_ids": ["..."]} (mark a specific set; ids not owned by the caller
// are silently ignored). Auth: requireSession middleware places the user in
// the request context.
func (s *Server) handleSessionsSeenHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	var body struct {
		All        bool     `json:"all"`
		SessionIDs []string `json:"session_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var ids []string
	if body.All {
		for _, ss := range s.registry.List() {
			if ss.OwnerUserID == u.ID {
				ids = append(ids, ss.ID.String())
			}
		}
	} else {
		for _, raw := range body.SessionIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				continue
			}
			if ss, ok := s.registry.Get(id); ok && ss.OwnerUserID == u.ID {
				ids = append(ids, ss.ID.String())
			}
		}
	}

	if len(ids) > 0 {
		if err := s.cfg.Store.SetSeen(r.Context(), u.ID, ids, time.Now().Unix()); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
