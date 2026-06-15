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
//   - last-admin guard: refuses if the caller is the only remaining admin,
//     so the deploy can never be locked out by an accidental delete.
//
// Note: under password auth this also did a password re-verify. With
// OPAQUE we cannot replay credentials server-side without a full client
// round, so re-verification will be folded into a separate OPAQUE
// step-up flow in M1b. For now, holding the bearer token plus typing
// the email is the required proof.
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
