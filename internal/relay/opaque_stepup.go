package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/bytemare/opaque"
)

// OPAQUE step-up: a second, short-lived OPAQUE round-trip that an already-
// session-authenticated user runs before performing a sensitive
// operation (DELETE /api/me, password change, admin reset). Mirrors the
// regular login init/finalize protocol bytes — the difference is that
// finalize emits a step_up_token (60-second TTL, single-use) instead of
// a long-lived session token. Wiring step-up onto a specific endpoint is
// a follow-up slice; this file just lands the protocol surface and a
// LookupStepUpToken helper that any future handler can call.

// stepUpTokenTTL is short on purpose. The threat we hedge is a stolen
// session_token: an attacker holding the bearer cannot also pass the
// step-up gate without recently knowing the password. A 60-second
// window is tight enough that even an active phishing relay must keep
// the user engaged in real time.
const stepUpTokenTTL = 60 * time.Second

// stepUpSessionTTL bounds how long the init→finalize handshake can
// stall before the parked *opaque.Server gets garbage collected. 30s
// matches loginSessionTTL — there is no reason to be slower or faster
// than the regular login.
const stepUpSessionTTL = 30 * time.Second

// stepUpPending is the per-handshake OPAQUE state captured at
// /api/auth/stepup/init and consumed at /finalize. Structurally
// identical to loginPending; kept separate so the two handshake maps
// cannot leak into each other.
type stepUpPending struct {
	email     string
	userID    string
	server    *opaque.Server
	expiresAt time.Time
}

// stepUpToken is the minted token returned from /finalize. The struct
// only ever lives in the in-memory map — callers use LookupStepUpToken
// which copies the (userID, validUntil) tuple out.
type stepUpToken struct {
	userID    string
	expiresAt time.Time
}

// stepUpStore is the in-memory token store. Separate from the request-
// scoped sync.Map on OpaqueAuthHandler so the helper functions reading
// it (mint, lookup, consume) don't need a handler pointer in scope.
//
// One-process-one-relay: the store is process-local. A relay restart
// invalidates every outstanding step-up token, which is the correct
// behaviour — the user re-runs the step-up handshake.
var (
	stepUpMu     sync.Mutex
	stepUpTokens = map[string]stepUpToken{}
)

// MintStepUpToken issues a single-use step-up token for userID with the
// fixed TTL. Stored under the in-process map; an AfterFunc janitor
// deletes the entry when it expires. Returns the plaintext token (which
// the caller MUST hand back to the client and forget — the relay never
// re-emits it).
func MintStepUpToken(userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := "stepup_" + base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(stepUpTokenTTL)
	stepUpMu.Lock()
	stepUpTokens[tok] = stepUpToken{userID: userID, expiresAt: expires}
	stepUpMu.Unlock()
	time.AfterFunc(stepUpTokenTTL, func() {
		stepUpMu.Lock()
		delete(stepUpTokens, tok)
		stepUpMu.Unlock()
	})
	return tok, nil
}

// ConsumeStepUpToken looks up tok, verifies it is for userID and has
// not expired, and removes it from the store. Returns true on success,
// false on any failure mode (missing, expired, wrong user). Use this
// from handlers that gate a sensitive operation: "consume" ensures a
// stolen token cannot be replayed against a different sensitive
// endpoint after the first one fires.
func ConsumeStepUpToken(tok, userID string) bool {
	if tok == "" || userID == "" {
		return false
	}
	stepUpMu.Lock()
	defer stepUpMu.Unlock()
	entry, ok := stepUpTokens[tok]
	if !ok {
		return false
	}
	if entry.userID != userID {
		return false
	}
	if time.Now().After(entry.expiresAt) {
		delete(stepUpTokens, tok)
		return false
	}
	delete(stepUpTokens, tok)
	return true
}

// stepUpInitRequest mirrors loginInitRequest exactly — the OPAQUE
// protocol bytes are identical for login and step-up. Separate type
// just for clarity at the call site.
type stepUpInitRequest struct {
	Email   string `json:"email"`
	LoginKE []byte `json:"login_ke"`
}

type stepUpInitResponse struct {
	LoginResponse []byte `json:"login_response"`
	SessionID     string `json:"session_id"`
}

type stepUpFinalizeRequest struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	LoginKE3  []byte `json:"login_ke3"`
}

type stepUpFinalizeResponse struct {
	UserID       string `json:"user_id"`
	StepUpToken  string `json:"step_up_token"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

// stepUpSessions is the per-handler OPAQUE state map. Lives on the
// OpaqueAuthHandler to keep concurrent multi-tenant relays isolated.
// Accessor methods below are tiny so a future test can swap the
// implementation.

func (h *OpaqueAuthHandler) stepUpStore(sessionID string, p *stepUpPending) {
	h.stepUpSessions.Store(sessionID, p)
}

func (h *OpaqueAuthHandler) stepUpTake(sessionID string) (*stepUpPending, bool) {
	raw, ok := h.stepUpSessions.LoadAndDelete(sessionID)
	if !ok {
		return nil, false
	}
	return raw.(*stepUpPending), true
}

// handleStepUpInit consumes the client's KE1 for the named email, runs
// the server's LoginInit against the stored OPAQUE record, and parks the
// per-request *opaque.Server keyed by a freshly minted session_id. The
// only difference from handleLoginInit is which sync.Map gets written —
// a deliberate split so a stolen login session_id cannot be used as a
// step-up session_id and vice versa.
func (h *OpaqueAuthHandler) handleStepUpInit(w http.ResponseWriter, r *http.Request) {
	var req stepUpInitRequest
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
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if user.DisabledAt != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	recordBytes, err := h.store.GetOpaqueRecord(ctx, user.ID)
	if err != nil {
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
		http.Error(w, "internal: parse record", http.StatusInternalServerError)
		return
	}
	ke1, err := sv.Deserialize.KE1(req.LoginKE)
	if err != nil {
		http.Error(w, "bad login_ke", http.StatusBadRequest)
		return
	}
	emailBytes := []byte(user.Email)
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
	h.stepUpStore(sessionID, &stepUpPending{
		email:     user.Email,
		userID:    user.ID,
		server:    sv,
		expiresAt: time.Now().Add(stepUpSessionTTL),
	})
	time.AfterFunc(stepUpSessionTTL, func() { h.stepUpSessions.Delete(sessionID) })

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stepUpInitResponse{
		LoginResponse: ke2.Serialize(),
		SessionID:     sessionID,
	})
}

// handleStepUpFinalize verifies KE3 against the parked *opaque.Server
// for sessionID. On success mints a short-lived step_up_token (NOT a
// real session token) and returns it. The client uses the token within
// stepUpTokenTTL on the actual sensitive endpoint; that endpoint calls
// ConsumeStepUpToken to verify + invalidate.
func (h *OpaqueAuthHandler) handleStepUpFinalize(w http.ResponseWriter, r *http.Request) {
	var req stepUpFinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.SessionID == "" || len(req.LoginKE3) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	pending, ok := h.stepUpTake(req.SessionID)
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if pending.email != req.Email || time.Now().After(pending.expiresAt) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	ke3, err := pending.server.Deserialize.KE3(req.LoginKE3)
	if err != nil {
		http.Error(w, "bad login_ke3", http.StatusBadRequest)
		return
	}
	if err := pending.server.LoginFinish(ke3); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	tok, err := MintStepUpToken(pending.userID)
	if err != nil {
		http.Error(w, "internal: mint stepup token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stepUpFinalizeResponse{
		UserID:       pending.userID,
		StepUpToken:  tok,
		ExpiresInSec: int(stepUpTokenTTL / time.Second),
	})
}
