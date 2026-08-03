package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

// handleLoginInit consumes the client's KE1 (login_ke) for the named email,
// derives the server's KE2 (login_response) against the stored
// RegistrationRecord, and parks the per-request *opaque.Server in
// loginSessions under a fresh session_id so handleLoginFinalize can pick
// up the same instance and verify KE3.
//
// Existence semantics: a missing user OR a missing OPAQUE record OR a
// disabled user all return generic 401 "invalid credentials". We do NOT
// run a dummy OPAQUE round to mask timing — that's a known v1 trade-off
// (see spec §15). The shape of the response (status + body) does not
// distinguish the three cases.
//
// CredentialIdentifier + ClientIdentity: both are the email bytes, which
// must match what the client passed to RegistrationFinalize (see the test
// in TestRegisterFinalize_PersistsRecordAndWrap). The relay's static
// ServerIdentity ("atterm-relay") is supplied via SetKeyMaterial inside
// newServer(); the library will mix that into the AKE transcript.
func (h *OpaqueAuthHandler) handleLoginInit(w http.ResponseWriter, r *http.Request) {
	var req loginInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || len(req.LoginKE) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	user, err := h.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		// Generic 401 to avoid revealing whether the email is registered.
		// Timing equalization (dummy OPAQUE round) is acknowledged as a
		// v1 gap; see spec §15.
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if user.DisabledAt != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	recordBytes, err := h.store.GetOpaqueRecord(ctx, user.ID)
	if err != nil {
		// OPAQUE record never persisted (incomplete registration) —
		// surfaces to the client as a generic 401, same as a bad password.
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sv, err := h.srv.newServer()
	if err != nil {
		http.Error(w, "internal: opaque server", http.StatusInternalServerError)
		return
	}
	record, err := sv.Deserialize.RegistrationRecord(recordBytes)
	if err != nil {
		// Stored record is corrupt — operator problem, not a client one.
		http.Error(w, "internal: parse record", http.StatusInternalServerError)
		return
	}
	ke1, err := sv.Deserialize.KE1(req.LoginKE)
	if err != nil {
		http.Error(w, "bad login_ke", http.StatusBadRequest)
		return
	}

	emailBytes := []byte(user.Email) // post-normalization (lowercased)
	ke2, err := sv.LoginInit(ke1, &opaque.ClientRecord{
		CredentialIdentifier: emailBytes,
		ClientIdentity:       emailBytes,
		RegistrationRecord:   record,
	})
	if err != nil {
		http.Error(w, "internal: login init", http.StatusInternalServerError)
		return
	}

	sessionID, err := newLoginSessionID()
	if err != nil {
		http.Error(w, "internal: session id", http.StatusInternalServerError)
		return
	}
	h.loginSessions.Store(sessionID, &loginPending{
		email:     user.Email,
		userID:    user.ID,
		server:    sv,
		expiresAt: time.Now().Add(loginSessionTTL),
	})
	// Per-entry janitor: bounds memory if finalize never arrives. The
	// closure captures sessionID by value, not the *loginPending, so a
	// fast finalize-then-init reuse cycle that hits the same session_id
	// (cryptographically improbable but cheap to guard) would not stomp
	// the new entry's lifetime. Delete is a no-op if finalize already
	// removed the row.
	time.AfterFunc(loginSessionTTL, func() { h.loginSessions.Delete(sessionID) })

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginInitResponse{
		LoginResponse: ke2.Serialize(),
		SessionID:     sessionID,
	})
}

// newLoginSessionID returns a fresh, unguessable session_id used only as
// the loginSessions map key — it is NOT a bearer credential and is not
// persisted. 32 bytes of crypto/rand → ~43 chars of base64url, matching
// the format CreateSession uses for session tokens so the two look
// homogeneous in logs while remaining distinct namespaces.
func newLoginSessionID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// handleLoginFinalize consumes the client's KE3 for a parked session_id,
// verifies it against the *opaque.Server primed at login/init time, mints a
// real session token, and returns the wrapped account_key so the client can
// derive its account key locally from the OPAQUE exportKey.
//
// LoadAndDelete on the session_id is the single-use guarantee: any retry —
// whether honest (lost response, browser back-button) or hostile (KE3 replay)
// — will miss the map and get a generic 401. The expiresAt check after
// LoadAndDelete is defensive against the per-entry AfterFunc janitor racing
// with finalize on the slow path.
//
// Failure ordering mirrors handleLoginInit: we degrade to generic 401 for
// any credential-shaped failure (missing session, expired session, MAC
// mismatch), and only return 500 for true server-side faults (wrap fetch,
// token mint). The MAC verification inside LoginFinish is the actual
// credential check — a wrong password results in an error here, not at
// login/init time, because the OPAQUE protocol does not let the server
// distinguish a bad password from a tampered transcript until KE3 arrives.
func (h *OpaqueAuthHandler) handleLoginFinalize(w http.ResponseWriter, r *http.Request) {
	var req loginFinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.SessionID == "" || len(req.LoginKE3) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// LoadAndDelete is what enforces "session_id usable at most once":
	// honest retries and KE3 replays alike land in the !ok branch.
	raw, ok := h.loginSessions.LoadAndDelete(req.SessionID)
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	pending := raw.(*loginPending)
	// Defensive expiry check — the per-entry AfterFunc janitor normally
	// fires first, but a paused goroutine could in principle let an
	// expired entry survive long enough to reach LoadAndDelete.
	if pending.email != req.Email || time.Now().After(pending.expiresAt) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	ke3, err := pending.server.Deserialize.KE3(req.LoginKE3)
	if err != nil {
		http.Error(w, "bad login_ke3", http.StatusBadRequest)
		return
	}
	// LoginFinish runs the AKE MAC check. A wrong password (or any
	// transcript tamper) shows up here as a non-nil error. Either way the
	// client sees a generic 401 — we do not leak which.
	if err := pending.server.LoginFinish(ke3); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	wrap, err := h.store.GetAccountKeyWrap(ctx, pending.userID, "password")
	if err != nil {
		http.Error(w, "internal: wrap", http.StatusInternalServerError)
		return
	}

	homeURL, err := resolveHomeInstanceURL(ctx, h.store, pending.userID, h.instancePublicURL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tok, _, err := h.store.CreateSession(ctx, pending.userID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		http.Error(w, "internal: create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginFinalizeResponse{
		UserID:       pending.userID,
		SessionToken: tok,
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    wrap.Method,
			Wrapped:   wrap.Wrapped,
			Nonce:     wrap.Nonce,
			Salt:      wrap.Salt,
			KDFParams: wrap.KDFParams,
		},
		RealmID:         h.realmID,
		HomeInstanceURL: homeURL,
	})
}
