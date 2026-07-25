package relay

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

const pairingTTL = 5 * time.Minute

func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *AuthServer) handlePairCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if a.Limits != nil && !a.Limits.AllowPairCreate(p.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var body struct {
		Wrap string `json:"wrap,omitempty"`
	}
	// Decode is best-effort — an empty/missing body is allowed for
	// backward compatibility with the old create endpoint.
	_ = json.NewDecoder(r.Body).Decode(&body)
	var wrap []byte
	if body.Wrap != "" {
		var err error
		wrap, err = base64.StdEncoding.DecodeString(body.Wrap)
		if err != nil {
			writeError(w, http.StatusBadRequest, "wrap_invalid")
			return
		}
	}

	secret, row, err := a.Store.CreatePairingToken(r.Context(), p.UserID, pairingTTL, wrap)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	plaintext := secret.Expose()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"token":      plaintext,
		"expires_at": row.ExpiresAt.Unix(),
		"qr_url":     publicBaseURL(r) + "/pair?t=" + plaintext,
	})
}

// handlePairConsume exchanges a pairing token for a fresh session token + relay URL
// + user info. No auth header required: the pairing token IS the credential
// (same trust model as OAuth Device Code Flow).
func (a *AuthServer) handlePairConsume(w http.ResponseWriter, r *http.Request) {
	if a.Limits != nil && !a.Limits.AllowPairConsume(ipPrefix(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"code": "pair_invalid"})
		return
	}

	// Consume the pairing token to retrieve the user + any wrapped
	// account key bundled with it (E2EE pairing, Task 2).
	user, wrap, err := a.Store.ConsumePairingToken(r.Context(), body.Token)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, userstore.ErrPairingConsumed) {
			status = http.StatusConflict
		} else if errors.Is(err, userstore.ErrPairingExpired) {
			status = http.StatusGone
		}
		writeJSONStatus(w, status, map[string]string{"code": "pair_invalid"})
		return
	}

	// Create a new session for the user.
	tok, sess, err := a.Store.CreateSession(r.Context(), user.ID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	homeURL, err := resolveHomeInstanceURL(r.Context(), a.Store, user.ID, a.InstancePublicURL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"session_token": tok,
		"expires_at":    sess.ExpiresAt.Unix(),
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
		},
		"realm_id":          a.RealmID,
		"home_instance_url": homeURL,
	}
	if len(wrap) > 0 {
		resp["wrap"] = base64.StdEncoding.EncodeToString(wrap)
	}
	writeJSONStatus(w, http.StatusOK, resp)
}
