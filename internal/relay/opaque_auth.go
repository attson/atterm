package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

// OpaqueAuthHandler owns the OPAQUE-based registration and login HTTP
// endpoints. It is wired by server.go after the OpaqueServer singleton
// has loaded its persisted seed.
//
// loginSessions holds the per-flow OPAQUE state between LoginInit (KE2 sent
// to client) and LoginFinish (KE3 received from client). The map is keyed
// by a freshly minted random session_id — distinct from the user-facing
// session_token issued post-login — so two concurrent in-flight login
// attempts for the same email do not collide. Entries TTL out after 30s
// via a per-entry time.AfterFunc janitor; the short window is fine because
// the client is expected to call finalize immediately. The map is held
// in-memory only; a relay restart cancels every pending flow, which is
// safe (the client will see a 401 on finalize and re-issue init).
type OpaqueAuthHandler struct {
	store *userstore.SQLiteStore
	srv   *OpaqueServer
	// bootstrapEmail is ATTERM_BOOTSTRAP_ADMIN_EMAIL. The first-run setup
	// flow auto-promotes a registration to admin when its email matches this
	// (case-insensitive) AND no admin exists yet — no claim token needed.
	// Empty disables the email-gated path (claim tokens still work).
	bootstrapEmail string
	// realmID is the realm this relay belongs to. It is echoed back in
	// every auth-finalize response so clients can anchor their account_key
	// to a (relay, realm) pair rather than just the origin URL.
	realmID        string
	loginSessions  sync.Map // session_id -> *loginPending
	stepUpSessions sync.Map // session_id -> *stepUpPending (M1i)
}

// loginPending is the in-flight OPAQUE login state for a single (email,
// session_id) pair. server holds the per-request *opaque.Server primed
// with SetKeyMaterial in handleLoginInit — re-using the same instance in
// handleLoginFinish is what lets the library's internal AKE bookkeeping
// (session key derivation, MAC verification) line up across the two HTTP
// round-trips. expiresAt is checked defensively at finalize time in case
// the AfterFunc janitor has not yet fired.
type loginPending struct {
	email     string
	userID    string
	server    *opaque.Server
	expiresAt time.Time
}

// loginSessionTTL bounds how long the relay will hold OPAQUE init state
// before discarding it. The client is expected to call finalize
// immediately after receiving KE2; 30s comfortably covers slow links and
// is short enough that abandoned flows do not accumulate.
const loginSessionTTL = 30 * time.Second

// NewOpaqueAuthHandler constructs the handler. Both store and srv must be
// non-nil; the OpaqueServer is expected to have been initialized via
// LoadOrInitOpaqueServer before this constructor is called.
func NewOpaqueAuthHandler(store *userstore.SQLiteStore, srv *OpaqueServer, bootstrapEmail, realmID string) *OpaqueAuthHandler {
	return &OpaqueAuthHandler{store: store, srv: srv, bootstrapEmail: strings.TrimSpace(bootstrapEmail), realmID: realmID}
}

// ----- Wire types -----

type registerInitRequest struct {
	Email          string `json:"email"`
	RegistrationKE []byte `json:"registration_ke"` // KE1 bytes from client
}

type registerInitResponse struct {
	RegistrationResponse []byte `json:"registration_response"` // KE2 bytes
}

type registerFinalizeRequest struct {
	Email              string                `json:"email"`
	RegistrationRecord []byte                `json:"registration_record"`
	AccountKeyWrap     accountKeyWrapPayload `json:"account_key_wrap"`
	// ClaimToken is the optional bootstrap / operator-issued one-time
	// token that promotes the new account to the role baked into the
	// token (e.g. "admin"). Empty for a normal self-service registration.
	ClaimToken string `json:"claim_token,omitempty"`
}

type accountKeyWrapPayload struct {
	Method    string `json:"method"`
	Wrapped   []byte `json:"wrapped"`
	Nonce     []byte `json:"nonce"`
	Salt      []byte `json:"salt"`
	KDFParams string `json:"kdf_params"`
}

type registerFinalizeResponse struct {
	UserID       string `json:"user_id"`
	SessionToken string `json:"session_token"`
	IsAdmin      bool   `json:"is_admin"`
	RealmID      string `json:"realm_id"`
}

type loginInitRequest struct {
	Email   string `json:"email"`
	LoginKE []byte `json:"login_ke"`
}

type loginInitResponse struct {
	LoginResponse []byte `json:"login_response"`
	SessionID     string `json:"session_id"`
}

type loginFinalizeRequest struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	LoginKE3  []byte `json:"login_ke3"`
}

type loginFinalizeResponse struct {
	UserID         string                `json:"user_id"`
	SessionToken   string                `json:"session_token"`
	AccountKeyWrap accountKeyWrapPayload `json:"account_key_wrap"`
	RealmID        string                `json:"realm_id"`
}

// ----- Handlers (stubs filled in by Tasks 7-10) -----

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
				log.Printf("opaque-register: set admin on claimed user %s: %v", user.ID, err)
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
			log.Printf("opaque-register: admin-exists check: %v", err)
		} else if !adminExists {
			if err := h.store.SetUserAdmin(ctx, user.ID, true); err != nil {
				log.Printf("opaque-register: first-run auto-admin %s: %v", user.ID, err)
			} else {
				isAdmin = true
				log.Printf("opaque-register: first-run admin created for %s", req.Email)
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
		RealmID: h.realmID,
	})
}

// Register adds the four OPAQUE routes to mux. Mirrors the style used by
// AuthServer.RegisterInto: method-prefixed patterns (Go 1.22+ ServeMux) with
// mux.Handle + http.HandlerFunc. The OPAQUE endpoints are all public — the
// protocol itself authenticates the client — so no session wrapper is applied.
func (h *OpaqueAuthHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/register/init", http.HandlerFunc(h.handleRegisterInit))
	mux.Handle("POST /api/auth/register/finalize", http.HandlerFunc(h.handleRegisterFinalize))
	mux.Handle("POST /api/auth/login/init", http.HandlerFunc(h.handleLoginInit))
	mux.Handle("POST /api/auth/login/finalize", http.HandlerFunc(h.handleLoginFinalize))
	// M1i: step-up OPAQUE round-trip for sensitive operations. Same
	// protocol bytes as login; emits a short-lived single-use step_up_token.
	mux.Handle("POST /api/auth/stepup/init", http.HandlerFunc(h.handleStepUpInit))
	mux.Handle("POST /api/auth/stepup/finalize", http.HandlerFunc(h.handleStepUpFinalize))
}
