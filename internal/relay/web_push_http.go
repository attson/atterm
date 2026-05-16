package relay

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/webpush"
)

// requireUserPrincipal resolves the identity for a web-push request and
// enforces that the caller is an authenticated user (cookie or API token).
//
//   - PrincipalUser  → returns the userID, ok=true
//   - PrincipalNone  → writes 401 and returns ok=false
//   - PrincipalAdmin → writes 403 and returns ok=false
//
// When s.cfg.Resolver is nil the server is operating in legacy shared-token
// mode; in that case the original authorizeClientWithConfig gate is applied
// and the token hash is returned as the key.
func (s *Server) requireUserPrincipal(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	if s.cfg.Resolver == nil {
		// Legacy mode: fall back to shared-token gate.
		if authorizeClientWithConfig(r, s.cfg) == authNone {
			writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
			return "", false
		}
		token := tokenFromRequestNoQuery(r)
		if token == "" {
			writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
			return "", false
		}
		return tokenHash(token), true
	}

	p := s.cfg.Resolver.Resolve(r)
	switch p.Kind {
	case PrincipalUser:
		return p.UserID, true
	case PrincipalNone:
		writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return "", false
	default: // PrincipalAdmin or any future kind
		writePushJSON(w, http.StatusForbidden, map[string]string{"error": "user account required"})
		return "", false
	}
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writePushJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	// /api/push/key is read-only; allow any authenticated principal (including
	// admin) so the browser can fetch the VAPID key before subscribing.
	if s.cfg.Resolver != nil {
		p := s.cfg.Resolver.Resolve(r)
		if p.Kind == PrincipalNone {
			writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
			return
		}
	} else {
		if authorizeClientWithConfig(r, s.cfg) == authNone {
			writePushJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
			return
		}
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
