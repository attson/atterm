package relay

import (
	"encoding/json"
	"net/http"
	"time"
)

type nodeEntry struct {
	InstanceID string `json:"instance_id"`
	PublicURL  string `json:"public_url"`
}

// handleNodesHTTP serves GET /api/nodes — the live instance list for the
// client-side node picker (ping latency is measured client-side).
func (s *Server) handleNodesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := s.cfg.Store.ListLiveInstances(r.Context(), minHB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nodes := make([]nodeEntry, 0, len(live))
	for _, inst := range live {
		nodes = append(nodes, nodeEntry{InstanceID: inst.InstanceID, PublicURL: inst.PublicURL})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
}

// handleSetHomeHTTP serves PUT /api/me/home — set the account-level home node.
func (s *Server) handleSetHomeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstanceID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Validate the target is a live instance.
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := s.cfg.Store.ListLiveInstances(r.Context(), minHB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	found := false
	for _, inst := range live {
		if inst.InstanceID == req.InstanceID {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "unknown or dead instance", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Store.SetUserHome(r.Context(), u.ID, req.InstanceID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
