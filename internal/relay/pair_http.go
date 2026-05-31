package relay

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// pairingTTL is the lifetime of a freshly minted pairing token.
const pairingTTL = 5 * time.Minute

// publicBaseURL derives the relay's externally reachable origin from the
// request — Host header, plus X-Forwarded-Proto for HTTPS detection behind
// a reverse proxy. Single source of truth shared by qr_url (create) and
// relay_url (consume).
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handlePairCreate mints a new pairing token for the authenticated user.
// POST /api/pair/create — requireUser.
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

// handlePairConsume — body lands in Task C3.
func (a *AuthServer) handlePairConsume(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
	_ = json.NewEncoder // keep import; remove in C3
}
