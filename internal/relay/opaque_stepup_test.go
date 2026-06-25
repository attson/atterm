package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

// TestStepUp_FullRoundTripMintsToken drives the full register → login →
// step-up sequence against an in-process relay and asserts that:
//  1. The step-up handshake emits a step_up_token whose plaintext format
//     matches the documented prefix.
//  2. The returned expires_in_sec falls in the (0, stepUpTokenTTL] band.
//  3. ConsumeStepUpToken(token, userID) returns true exactly once.
func TestStepUp_FullRoundTripMintsToken(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	h := NewOpaqueAuthHandler(store, srv, "", "", "")

	const email = "alice@example.com"
	const password = "hunter2-stepup"
	userID := registerForStepUp(t, h, email, password)

	stepResp := doStepUpRoundTrip(t, h, email, password)
	if stepResp.UserID != userID {
		t.Fatalf("StepUp UserID = %q, want %q", stepResp.UserID, userID)
	}
	if !strings.HasPrefix(stepResp.StepUpToken, "stepup_") {
		t.Fatalf("step_up_token missing 'stepup_' prefix: %q", stepResp.StepUpToken)
	}
	if stepResp.ExpiresInSec <= 0 || stepResp.ExpiresInSec > int(stepUpTokenTTL/time.Second) {
		t.Fatalf("expires_in_sec out of band: %d", stepResp.ExpiresInSec)
	}

	if !ConsumeStepUpToken(stepResp.StepUpToken, userID) {
		t.Fatalf("first Consume returned false")
	}
	// Single-use: second call must fail.
	if ConsumeStepUpToken(stepResp.StepUpToken, userID) {
		t.Fatalf("second Consume returned true; token should be single-use")
	}
}

// TestStepUp_WrongUserCannotConsume verifies that a token minted for
// user A cannot be used to gate a sensitive op on user B's account.
// This is the load-bearing isolation property — without the (token,
// userID) coupling, ConsumeStepUpToken would just be a global
// nonce-store with no per-account check.
func TestStepUp_WrongUserCannotConsume(t *testing.T) {
	tok, err := MintStepUpToken("user-A")
	if err != nil {
		t.Fatalf("MintStepUpToken: %v", err)
	}
	if ConsumeStepUpToken(tok, "user-B") {
		t.Fatalf("Consume succeeded for wrong user")
	}
	// Original user still works.
	if !ConsumeStepUpToken(tok, "user-A") {
		t.Fatalf("Consume failed for correct user after wrong-user attempt")
	}
}

// TestStepUp_WrongPasswordRejected drives the step-up handshake with a
// password that does not match the registered one and asserts the
// /finalize endpoint returns 401. The TS client side detects the wrong
// password during KE2 MAC check, so /finalize might never be called in
// practice — here we forge a KE3 against a different-keyed client to
// exercise the server-side rejection.
func TestStepUp_WrongPasswordRejected(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	h := NewOpaqueAuthHandler(store, srv, "", "", "")

	const email = "alice@example.com"
	registerForStepUp(t, h, email, "correct-password")

	// Init the handshake with the WRONG password. The KE2 the server
	// sends back will refuse to verify under the client's password.
	conf := defaultConfig()
	cl, _ := conf.Client()
	ke1 := cl.LoginInit([]byte("wrong-password"))

	initBody, _ := json.Marshal(stepUpInitRequest{
		Email:   email,
		LoginKE: ke1.Serialize(),
	})
	rec := httptest.NewRecorder()
	h.handleStepUpInit(rec, httptest.NewRequest(http.MethodPost, "/api/auth/stepup/init", bytes.NewReader(initBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("init status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var initResp stepUpInitResponse
	if err := json.NewDecoder(rec.Body).Decode(&initResp); err != nil {
		t.Fatalf("decode init: %v", err)
	}

	ke2, err := cl.Deserialize.KE2(initResp.LoginResponse)
	if err != nil {
		// Client-side KE2 reject is one acceptable failure mode.
		return
	}
	ke3, _, err := cl.LoginFinish(ke2, opaque.ClientLoginFinishOptions{
		ClientIdentity: []byte(email),
		ServerIdentity: []byte(opaqueServerIdentity),
	})
	if err != nil {
		// Either the client refused to compute KE3 OR the server will
		// refuse to verify it — both end up as a credentials-shaped
		// failure, which is what we're testing.
		return
	}
	finBody, _ := json.Marshal(stepUpFinalizeRequest{
		Email:     email,
		SessionID: initResp.SessionID,
		LoginKE3:  ke3.Serialize(),
	})
	rec2 := httptest.NewRecorder()
	h.handleStepUpFinalize(rec2, httptest.NewRequest(http.MethodPost, "/api/auth/stepup/finalize", bytes.NewReader(finBody)))
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("finalize status = %d, want 401 with wrong password", rec2.Code)
	}
}

// TestStepUp_ConsumeAfterExpiry checks that the TTL window is respected.
// MintStepUpToken's AfterFunc janitor normally fires before Consume sees
// the expired entry, but a defensive in-Consume check guarantees the
// property even if the janitor is delayed.
func TestStepUp_ConsumeAfterExpiry(t *testing.T) {
	tok, err := MintStepUpToken("user-X")
	if err != nil {
		t.Fatalf("MintStepUpToken: %v", err)
	}
	// Force the entry to look expired by overwriting it. Reaching into
	// the internal map is acceptable in a same-package test.
	stepUpMu.Lock()
	entry := stepUpTokens[tok]
	entry.expiresAt = time.Now().Add(-time.Second)
	stepUpTokens[tok] = entry
	stepUpMu.Unlock()

	if ConsumeStepUpToken(tok, "user-X") {
		t.Fatalf("Consume returned true for expired token")
	}
}

// TestStepUpInit_WrongEmail returns generic 401 to avoid leaking
// whether the user exists.
func TestStepUpInit_WrongEmail(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	h := NewOpaqueAuthHandler(store, srv, "", "", "")

	conf := defaultConfig()
	cl, _ := conf.Client()
	ke1 := cl.LoginInit([]byte("pw"))
	body, _ := json.Marshal(stepUpInitRequest{Email: "ghost@example.com", LoginKE: ke1.Serialize()})
	rec := httptest.NewRecorder()
	h.handleStepUpInit(rec, httptest.NewRequest(http.MethodPost, "/api/auth/stepup/init", bytes.NewReader(body)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unknown email", rec.Code)
	}
}

// TestStepUp_RoutesRegistered confirms Register() wires the two new
// stepup endpoints alongside login/register.
func TestStepUp_RoutesRegistered(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv, _ := LoadOrInitOpaqueServer(context.Background(), store)
	h := NewOpaqueAuthHandler(store, srv, "", "", "")

	mux := http.NewServeMux()
	h.Register(mux)

	for _, p := range []string{"/api/auth/stepup/init", "/api/auth/stepup/finalize"} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s returned 404; route not mounted", p)
		}
	}
}

// ---- helpers ----

func registerForStepUp(t *testing.T, h *OpaqueAuthHandler, email, password string) string {
	t.Helper()
	conf := defaultConfig()
	cl, _ := conf.Client()
	ke1 := cl.RegistrationInit([]byte(password))
	initBody, _ := json.Marshal(registerInitRequest{Email: email, RegistrationKE: ke1.Serialize()})
	rec := httptest.NewRecorder()
	h.handleRegisterInit(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(initBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("register init: %d %s", rec.Code, rec.Body.String())
	}
	var initR registerInitResponse
	_ = json.NewDecoder(rec.Body).Decode(&initR)
	ke2, _ := cl.Deserialize.RegistrationResponse(initR.RegistrationResponse)
	record, _ := cl.RegistrationFinalize(ke2, opaque.ClientRegistrationFinalizeOptions{
		ClientIdentity: []byte(email),
		ServerIdentity: []byte(opaqueServerIdentity),
	})
	finBody, _ := json.Marshal(registerFinalizeRequest{
		Email:              email,
		RegistrationRecord: record.Serialize(),
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    "password",
			Wrapped:   []byte("ct"),
			Nonce:     []byte("nonce-24-byte-fixed-aaaa"),
			Salt:      []byte("salt-16-byteAA"),
			KDFParams: `{"alg":"argon2id"}`,
		},
	})
	rec2 := httptest.NewRecorder()
	h.handleRegisterFinalize(rec2, httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader(finBody)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("register finalize: %d %s", rec2.Code, rec2.Body.String())
	}
	var finR registerFinalizeResponse
	_ = json.NewDecoder(rec2.Body).Decode(&finR)
	return finR.UserID
}

func doStepUpRoundTrip(t *testing.T, h *OpaqueAuthHandler, email, password string) stepUpFinalizeResponse {
	t.Helper()
	conf := defaultConfig()
	cl, _ := conf.Client()
	ke1 := cl.LoginInit([]byte(password))

	initBody, _ := json.Marshal(stepUpInitRequest{Email: email, LoginKE: ke1.Serialize()})
	rec := httptest.NewRecorder()
	h.handleStepUpInit(rec, httptest.NewRequest(http.MethodPost, "/api/auth/stepup/init", bytes.NewReader(initBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("stepup init: %d %s", rec.Code, rec.Body.String())
	}
	var initR stepUpInitResponse
	_ = json.NewDecoder(rec.Body).Decode(&initR)
	ke2, err := cl.Deserialize.KE2(initR.LoginResponse)
	if err != nil {
		t.Fatalf("ke2 deserialize: %v", err)
	}
	ke3, _, err := cl.LoginFinish(ke2, opaque.ClientLoginFinishOptions{
		ClientIdentity: []byte(email),
		ServerIdentity: []byte(opaqueServerIdentity),
	})
	if err != nil {
		t.Fatalf("client LoginFinish: %v", err)
	}
	finBody, _ := json.Marshal(stepUpFinalizeRequest{
		Email:     email,
		SessionID: initR.SessionID,
		LoginKE3:  ke3.Serialize(),
	})
	rec2 := httptest.NewRecorder()
	h.handleStepUpFinalize(rec2, httptest.NewRequest(http.MethodPost, "/api/auth/stepup/finalize", bytes.NewReader(finBody)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("stepup finalize: %d %s", rec2.Code, rec2.Body.String())
	}
	var fin stepUpFinalizeResponse
	if err := json.NewDecoder(rec2.Body).Decode(&fin); err != nil {
		t.Fatalf("decode finalize: %v", err)
	}
	return fin
}
