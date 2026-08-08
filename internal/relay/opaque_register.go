package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/userstore"
)

// handleRegisterInit consumes the client's KE1 (RegistrationRequest) and
// returns the server's KE2 (RegistrationResponse). No user row is created at
// this step — finalize is what writes to the DB. The credential identifier
// we hand to the OPAQUE library is the email bytes; this must stay stable
// across init/finalize/login for a given user, which is naturally true
// because the email is the lookup key on the client side too.
func (h *OpaqueAuthHandler) handleRegisterInit(w http.ResponseWriter, r *http.Request) {
	var req registerInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || len(req.RegistrationKE) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	sv, err := h.srv.newServer()
	if err != nil {
		http.Error(w, "internal: opaque server", http.StatusInternalServerError)
		return
	}

	// bytemare/opaque v0.10 exposes deserialization via the Server's
	// Deserialize field (not on Configuration directly). Same goes for
	// turning the stored AKE public-key bytes back into a *group.Element
	// — RegistrationResponse takes the typed element, not raw bytes.
	regReq, err := sv.Deserialize.RegistrationRequest(req.RegistrationKE)
	if err != nil {
		http.Error(w, "bad registration_ke", http.StatusBadRequest)
		return
	}
	pks, err := sv.Deserialize.DecodeAkePublicKey(h.srv.akePublic)
	if err != nil {
		http.Error(w, "internal: decode server public key", http.StatusInternalServerError)
		return
	}

	ke2 := sv.RegistrationResponse(regReq, pks, []byte(req.Email), h.srv.oprfSeed)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(registerInitResponse{
		RegistrationResponse: ke2.Serialize(),
	})
}

// handleRegisterFinalize consumes the client's RegistrationRecord and the
// account_key wrap blob, persists both alongside a fresh user row, and mints
// a session token so the client can immediately call authenticated APIs.
//
// Failure ordering matters: we validate inputs cheaply (JSON, required
// fields, well-formed record bytes) before touching the DB; CreateOpaqueUser
// runs first so we can map UNIQUE-email failures to a 409 without leaving
// orphaned opaque_record / wrap rows. Only after the user row exists do we
// persist the OPAQUE record and the wrap; failures of either fall through
// to 500 — the user row is left in place because (a) re-registering with
// the same email would now collide on UNIQUE, and (b) the relay does not
// model a "user exists but unusable" state. In practice both StoreOpaque*
// calls only fail on transient DB errors; the operator can clean up by
// hand if needed.
//
// Session token minting goes straight through Store.CreateSession with no
// intermediate helper: it's a one-liner and OPAQUE is now the only path
// that mints sessions, so there's nothing to share with.
func (h *OpaqueAuthHandler) handleRegisterFinalize(w http.ResponseWriter, r *http.Request) {
	var req registerFinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" ||
		len(req.RegistrationRecord) == 0 ||
		len(req.AccountKeyWrap.Wrapped) == 0 ||
		len(req.AccountKeyWrap.Nonce) == 0 ||
		len(req.AccountKeyWrap.Salt) == 0 ||
		req.AccountKeyWrap.Method == "" ||
		req.AccountKeyWrap.KDFParams == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// Validate the registration record bytes before any DB write so a
	// garbage payload can't leave an orphaned user row behind. The
	// Deserializer lives on the per-request *opaque.Server (same pattern
	// as handleRegisterInit), not on the Configuration.
	sv, err := h.srv.newServer()
	if err != nil {
		http.Error(w, "internal: opaque server", http.StatusInternalServerError)
		return
	}
	if _, err := sv.Deserialize.RegistrationRecord(req.RegistrationRecord); err != nil {
		http.Error(w, "bad registration record", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// If a claim token is supplied, validate it BEFORE creating the user
	// row so a bogus / expired / consumed token can't leave an orphan
	// account behind. The token is consumed (and the role applied) after
	// CreateOpaqueUser succeeds, see below.
	var claimedRole string
	if req.ClaimToken != "" {
		tok, err := h.store.LookupClaimToken(ctx, req.ClaimToken)
		if err != nil {
			// Collapse NotFound / Consumed / Expired into one 401 so the
			// endpoint isn't a token-validity oracle. Operator-facing
			// detail is already in the sentinel error in logs if needed.
			http.Error(w, "invalid claim token", http.StatusUnauthorized)
			return
		}
		if !strings.EqualFold(tok.Email, req.Email) {
			http.Error(w, "claim token email mismatch", http.StatusUnauthorized)
			return
		}
		claimedRole = tok.Role
	}

	user, err := h.store.CreateOpaqueUser(ctx, req.Email)
	if err != nil {
		if errors.Is(err, userstore.ErrEmailTaken) {
			http.Error(w, "email taken", http.StatusConflict)
			return
		}
		http.Error(w, "internal: create user", http.StatusInternalServerError)
		return
	}

	if err := h.store.StoreOpaqueRecord(ctx, user.ID, req.RegistrationRecord); err != nil {
		http.Error(w, "internal: store record", http.StatusInternalServerError)
		return
	}

	if err := h.store.StoreAccountKeyWrap(ctx, userstore.AccountKeyWrap{
		UserID:    user.ID,
		Method:    req.AccountKeyWrap.Method,
		Wrapped:   req.AccountKeyWrap.Wrapped,
		Nonce:     req.AccountKeyWrap.Nonce,
		Salt:      req.AccountKeyWrap.Salt,
		KDFParams: req.AccountKeyWrap.KDFParams,
	}); err != nil {
		http.Error(w, "internal: store wrap", http.StatusInternalServerError)
		return
	}

	// Consume the claim token (if any) and apply its role. The token was
	// already validated above, but a concurrent finalize for the same token
	// could have consumed it in the meantime — the atomic UPDATE inside
	// ConsumeClaimToken is what serializes that race. We deliberately
	// consume AFTER the user row exists: at this point the registration is
	// otherwise complete, so racing finalizes that lose the consume race
	// see a 401 and the operator must mint a new token. The orphan user is
	// acceptable (UNIQUE-email guards against the same email being claimed
	// twice anyway).
	isAdmin := false
	switch {
	case claimedRole != "":
		if err := h.store.ConsumeClaimToken(ctx, req.ClaimToken); err != nil {
			http.Error(w, "claim token race", http.StatusUnauthorized)
			return
		}
		if claimedRole == "admin" {
			if err := h.store.SetUserAdmin(ctx, user.ID, true); err != nil {
				// Promotion failure is non-fatal: registration succeeded,
				// the operator can re-promote the user via the admin
				// console. Log loudly so the gap is visible.
				logging.Error("opaque", "set admin on claimed user %s: %v", user.ID, err)
			} else {
				isAdmin = true
			}
		}
	case h.bootstrapEmail != "" && strings.EqualFold(req.Email, h.bootstrapEmail):
		// First-run setup: no claim token, but the email matches the
		// configured bootstrap admin AND no admin exists yet → auto-promote.
		// The channel closes the moment the first admin exists; email
		// UNIQUEness means only one account can ever hold this email.
		if adminExists, err := h.store.AdminExists(ctx); err != nil {
			logging.Warn("opaque", "admin-exists check: %v", err)
		} else if !adminExists {
			if err := h.store.SetUserAdmin(ctx, user.ID, true); err != nil {
				logging.Error("opaque", "first-run auto-admin %s: %v", user.ID, err)
			} else {
				isAdmin = true
				logging.Info("opaque", "first-run admin created for %s", req.Email)
			}
		}
	}

	tok, _, err := h.store.CreateSession(ctx, user.ID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		http.Error(w, "internal: create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(registerFinalizeResponse{
		UserID:       user.ID,
		SessionToken: tok,
		IsAdmin:      isAdmin,
		RealmID:      h.realmID,
	})
}
