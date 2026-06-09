package relay

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/webpush"
)

// requireUserPrincipal pulls the authenticated user from the request context
// (set by requireSession middleware) and returns their userID. When the
// middleware ran the bool is always true; the false branch survives as
// defence against future wiring mistakes.
func (s *Server) requireUserPrincipal(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	u, present := UserFromContext(r.Context())
	if !present || u == nil {
		writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return "", false
	}
	return u.ID, true
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writePushJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	// /api/push/key is read-only; any authenticated user can fetch the VAPID
	// key before subscribing. requireSession has already vetted the request.
	if _, ok := UserFromContext(r.Context()); !ok {
		writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	writePushJSON(w, http.StatusOK, map[string]string{"key": s.cfg.WebPush.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writePushJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	userID, ok := s.requireUserPrincipal(w, r)
	if !ok {
		return
	}
	var sub webpush.Subscription
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&sub); err != nil {
		writePushJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if !strings.HasPrefix(sub.Endpoint, "https://") {
		writePushJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint must be https"})
		return
	}
	if !validBase64URLKey(sub.Keys.P256dh) || !validBase64URLKey(sub.Keys.Auth) {
		writePushJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid keys"})
		return
	}
	sub.CreatedAt = time.Now().Unix()
	if err := s.cfg.WebPush.AddSubscription(userID, sub); err != nil {
		writePushJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writePushJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writePushJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	userID, ok := s.requireUserPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writePushJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	_ = s.cfg.WebPush.RemoveSubscription(userID, body.Endpoint)
	writePushJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writePushJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	userID, ok := s.requireUserPrincipal(w, r)
	if !ok {
		return
	}
	n := s.cfg.WebPush.SendTest(userID)
	writePushJSON(w, http.StatusOK, map[string]int{"sent": n})
}

func validBase64URLKey(s string) bool {
	if s == "" {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return false
	}
	return len(data) >= 16 && len(data) <= 128
}

func writePushJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
