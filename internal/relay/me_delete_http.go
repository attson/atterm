package relay

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleDeleteMe implements DELETE /api/me. Hard-deletes the calling user.
//
// Defence in depth:
//   - requireUser: anonymous callers 401 immediately.
//   - email match: typo-protection; the client must echo the exact email of
//     the user the cookie resolves to.
//   - password re-verify: even with a stolen cookie, an attacker still needs
//     the plaintext password.
//   - last-admin guard: refuses if the caller is the only remaining admin,
//     so the deploy can never be locked out by an accidental delete.
//
// On success the user row is dropped (api_tokens and sessions cascade
// via FK; invitations.consumed_by is nulled by DeleteUser's transaction).
// The session cookie row is gone too, but we still emit a Max-Age=-1
// Set-Cookie so the browser stops sending the invalid cookie.
func (a *AuthServer) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := a.Store.GetUser(r.Context(), p.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(body.Email), user.Email) {
		writeError(w, http.StatusBadRequest, "email_mismatch")
		return
	}

	// Re-verify password — attacker with stolen cookie still needs plaintext.
	v, err := a.Store.VerifyPassword(r.Context(), user.Email, body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if v == nil {
		writeError(w, http.StatusUnauthorized, "password_incorrect")
		return
	}

	// Last-admin protection: don't let the deploy lose its only operator.
	if user.IsAdmin {
		admins, cerr := countAdmins(r.Context(), a.Store)
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
	}

	if err := a.Store.DeleteUser(r.Context(), p.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Clear the session cookie so the browser stops sending the now-orphaned
	// session id. The DB row was already cascade-dropped from sessions.
	setSessionCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}
