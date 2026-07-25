package relay

import (
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
	secret, row, err := a.Store.CreatePairingToken(r.Context(), p.UserID, pairingTTL, nil)
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

	// Consume the pairing token to retrieve the user.
	user, _, err := a.Store.ConsumePairingToken(r.Context(), body.Token)
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

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"session_token": tok,
		"expires_at":    sess.ExpiresAt.Unix(),
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}
