package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

// TestOPAQUE_FullRegisterAndLogin drives a real HTTP server round-trip
// through net/http (httptest.NewServer + http.Post) instead of dispatching
// directly to handler methods. The direct-handler tests in
// opaque_auth_test.go cover the per-endpoint semantics; this one is the
// safety net for wire-shape regressions — mux routing, JSON marshal /
// unmarshal mismatches, missing Content-Type handling — that a
// httptest.NewRecorder cannot catch because it skips the mux entirely.
//
// Three flows in one test, sequentially so they share the registered user:
//  1. register/init → register/finalize for "alice@example.com" with
//     password "hunter2"
//  2. login/init → login/finalize with the correct password, asserting the
//     account_key wrap blob round-trips byte-for-byte and a non-empty
//     session token is minted
//  3. login/init → login/finalize with a wrong password, expecting either a
//     client-side LoginFinish rejection (the OPAQUE library's KE2 MAC won't
//     verify under the wrong password) or — if the client somehow accepts
//     KE2 — a server-side 401 on finalize.
func TestOPAQUE_FullRegisterAndLogin(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	h := NewOpaqueAuthHandler(store, srv, "")

	mux := http.NewServeMux()
	h.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	conf := defaultConfig()
	client, _ := conf.Client()

	// ---- registration ----
	ke1 := client.RegistrationInit([]byte("hunter2"))
	initBody, _ := json.Marshal(registerInitRequest{
		Email:          "alice@example.com",
		RegistrationKE: ke1.Serialize(),
	})
	initResp, err := http.Post(ts.URL+"/api/auth/register/init", "application/json", bytes.NewReader(initBody))
	if err != nil {
		t.Fatalf("register init POST: %v", err)
	}
	if initResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(initResp.Body)
		t.Fatalf("register init: status=%d body=%s", initResp.StatusCode, b)
	}
	var initR registerInitResponse
	if err := json.NewDecoder(initResp.Body).Decode(&initR); err != nil {
		t.Fatalf("decode register init: %v", err)
	}
	initResp.Body.Close()

	ke2, err := client.Deserialize.RegistrationResponse(initR.RegistrationResponse)
	if err != nil {
		t.Fatalf("ke2: %v", err)
	}
	// Identities must match what T8 / T10 established and what the direct
	// handler tests in opaque_auth_test.go bind:
	// ClientIdentity = email, ServerIdentity = "atterm-relay". A mismatch
	// here surfaces as a MAC-failure 401 at login finalize time.
	record, _ := client.RegistrationFinalize(ke2, opaque.ClientRegistrationFinalizeOptions{
		ClientIdentity: []byte("alice@example.com"),
		ServerIdentity: []byte("atterm-relay"),
	})
	finBody, _ := json.Marshal(registerFinalizeRequest{
		Email:              "alice@example.com",
		RegistrationRecord: record.Serialize(),
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    "password",
			Wrapped:   []byte("ct"),
			Nonce:     []byte("nonce-24-byte-fixed-aaaa"),
			Salt:      []byte("salt-16-byteAA"),
			KDFParams: `{"alg":"argon2id"}`,
		},
	})
	finResp, err := http.Post(ts.URL+"/api/auth/register/finalize", "application/json", bytes.NewReader(finBody))
	if err != nil {
		t.Fatalf("register finalize POST: %v", err)
	}
	if finResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(finResp.Body)
		t.Fatalf("register finalize: status=%d body=%s", finResp.StatusCode, b)
	}
	finResp.Body.Close()

	// ---- login ok ----
	clientL, _ := conf.Client()
	ke1L := clientL.LoginInit([]byte("hunter2"))
	initBodyL, _ := json.Marshal(loginInitRequest{
		Email:   "alice@example.com",
		LoginKE: ke1L.Serialize(),
	})
	rL1, err := http.Post(ts.URL+"/api/auth/login/init", "application/json", bytes.NewReader(initBodyL))
	if err != nil {
		t.Fatalf("login init POST: %v", err)
	}
	if rL1.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(rL1.Body)
		t.Fatalf("login init: status=%d body=%s", rL1.StatusCode, b)
	}
	var ir loginInitResponse
	if err := json.NewDecoder(rL1.Body).Decode(&ir); err != nil {
		t.Fatalf("decode login init: %v", err)
	}
	rL1.Body.Close()

	ke2L, err := clientL.Deserialize.KE2(ir.LoginResponse)
	if err != nil {
		t.Fatalf("ke2L: %v", err)
	}
	ke3L, _, err := clientL.LoginFinish(ke2L, opaque.ClientLoginFinishOptions{
		ClientIdentity: []byte("alice@example.com"),
		ServerIdentity: []byte("atterm-relay"),
	})
	if err != nil {
		t.Fatalf("client LoginFinish: %v", err)
	}
	finBodyL, _ := json.Marshal(loginFinalizeRequest{
		Email:     "alice@example.com",
		SessionID: ir.SessionID,
		LoginKE3:  ke3L.Serialize(),
	})
	rL2, err := http.Post(ts.URL+"/api/auth/login/finalize", "application/json", bytes.NewReader(finBodyL))
	if err != nil {
		t.Fatalf("login finalize POST: %v", err)
	}
	if rL2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(rL2.Body)
		t.Fatalf("login finalize: status=%d body=%s", rL2.StatusCode, b)
	}
	var lr loginFinalizeResponse
	if err := json.NewDecoder(rL2.Body).Decode(&lr); err != nil {
		t.Fatalf("decode login finalize: %v", err)
	}
	rL2.Body.Close()
	if string(lr.AccountKeyWrap.Wrapped) != "ct" {
		t.Fatalf("wrap mismatch: %q", lr.AccountKeyWrap.Wrapped)
	}
	if lr.SessionToken == "" {
		t.Fatalf("empty session token")
	}

	// ---- login wrong password ----
	// The OPAQUE protocol does not let the server distinguish a bad
	// password until KE3 arrives — the server happily produces a KE2 for
	// any well-formed KE1. The MAC verification happens client-side first
	// (clientW.LoginFinish fails because the KE2 MAC won't match under the
	// wrong password) and, if that ever stops being the case, the
	// finalize endpoint MUST return 401. We accept either path.
	clientW, _ := conf.Client()
	ke1W := clientW.LoginInit([]byte("wrong"))
	initBodyW, _ := json.Marshal(loginInitRequest{
		Email:   "alice@example.com",
		LoginKE: ke1W.Serialize(),
	})
	rW1, err := http.Post(ts.URL+"/api/auth/login/init", "application/json", bytes.NewReader(initBodyW))
	if err != nil {
		t.Fatalf("login init wrong POST: %v", err)
	}
	if rW1.StatusCode != http.StatusOK {
		t.Fatalf("login init (wrong pw) status=%d", rW1.StatusCode)
	}
	var iw loginInitResponse
	json.NewDecoder(rW1.Body).Decode(&iw)
	rW1.Body.Close()

	ke2W, err := clientW.Deserialize.KE2(iw.LoginResponse)
	if err != nil {
		// Acceptable: client-side rejection of KE2 with wrong password.
		t.Logf("client-side KE2 deserialize rejected wrong password: %v", err)
		return
	}
	ke3W, _, err := clientW.LoginFinish(ke2W, opaque.ClientLoginFinishOptions{
		ClientIdentity: []byte("alice@example.com"),
		ServerIdentity: []byte("atterm-relay"),
	})
	if err != nil {
		// Acceptable: client-side rejection (server's KE2 contains a MAC
		// that won't verify under the wrong password).
		t.Logf("client-side LoginFinish rejected wrong password: %v", err)
		return
	}
	// If we got this far, the server must reject KE3.
	finBodyW, _ := json.Marshal(loginFinalizeRequest{
		Email:     "alice@example.com",
		SessionID: iw.SessionID,
		LoginKE3:  ke3W.Serialize(),
	})
	rW2, err := http.Post(ts.URL+"/api/auth/login/finalize", "application/json", bytes.NewReader(finBodyW))
	if err != nil {
		t.Fatalf("login finalize wrong POST: %v", err)
	}
	if rW2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password, got %d", rW2.StatusCode)
	}
}
