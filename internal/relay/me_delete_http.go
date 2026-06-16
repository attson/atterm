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
//   - X-Step-Up-Token header: a fresh, single-use OPAQUE step-up token
//     proving the caller re-verified their password in the last 60
//     seconds. Minted via POST /api/auth/stepup/{init,finalize}; consumed
//     here via ConsumeStepUpToken. Restores the password re-verify
//     defence that the M1a → OPAQUE migration retired (M1i-enforce).
//   - email match: typo-protection; the client must echo the exact email of
//     the user the cookie resolves to.
//   - last-admin guard: refuses if the caller is the only remaining admin,
//     so the deploy can never be locked out by an accidental delete.
//
// On success the user row is dropped (sessions cascade via FK;
// invitations.consumed_by is nulled by DeleteUser's transaction). The
// caller's bearer token is now invalid — the client is expected to discard
// it after a successful 204.
func (a *AuthServer) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	// Step-up gate. Single-use, 60s TTL, user-bound — see opaque_stepup.go.
	// A missing or invalid token is the most likely failure mode in
	// practice (UI forgot to run the handshake first), so the error code
	// is distinct from the other 400/409 reasons below.
	stepUpTok := strings.TrimSpace(r.Header.Get("X-Step-Up-Token"))
	if stepUpTok == "" {
		writeError(w, http.StatusUnauthorized, "step_up_required")
		return
	}
	if !ConsumeStepUpToken(stepUpTok, p.UserID) {
		writeError(w, http.StatusUnauthorized, "step_up_invalid")
		return
	}

	var body struct {
		Email string `json:"email"`
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

	// The bearer token is now orphaned (sessions row cascade-dropped). Client
	// is responsible for discarding it on the 204 response.
	w.WriteHeader(http.StatusNoContent)
}
