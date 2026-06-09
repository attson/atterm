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
	secret, row, err := a.Store.CreatePairingToken(r.Context(), p.UserID, pairingTTL)
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

// handlePairConsume exchanges a pairing token for a fresh API token + relay URL
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

	// TODO(task-1.8): ConsumePairingToken now returns (*User, error). This
	// handler will be rewritten to mint a session token and return it instead
	// of the legacy api_token. Until then the route returns 410 Gone.
	user, err := a.Store.ConsumePairingToken(r.Context(), body.Token)
	if err != nil {
		if errors.Is(err, userstore.ErrPairingInvalid) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"code": "pair_invalid"})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_ = user
	http.Error(w, "gone", http.StatusGone)
}
