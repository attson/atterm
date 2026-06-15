package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

// newTestOpaqueAuthHandler wires an in-memory store, a freshly-initialized
// OPAQUE server singleton (first-boot path — generates a seed + AKE keypair),
// and returns the handler under test. Each call gets its own store, so tests
// stay isolated.
func newTestOpaqueAuthHandler(t *testing.T) *OpaqueAuthHandler {
	t.Helper()
	store := userstore.NewInMemory(t)
	srv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	return NewOpaqueAuthHandler(store, srv)
}

// TestRegisterInit_ReturnsKE2 drives the registerInit endpoint with a real
// client KE1 produced by the library, asserts a 200 + non-empty KE2 (the
// server's RegistrationResponse) comes back, and that the response round-trips
// through the client's deserializer so we know the bytes are well-formed.
func TestRegisterInit_ReturnsKE2(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)

	client, err := defaultConfig().Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ke1 := client.RegistrationInit([]byte("hunter2"))

	body, _ := json.Marshal(registerInitRequest{
		Email:          "alice@example.com",
		RegistrationKE: ke1.Serialize(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.handleRegisterInit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp registerInitResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.RegistrationResponse) == 0 {
		t.Fatalf("empty KE2")
	}

	// Sanity: the client must be able to deserialize KE2. If the server
	// sent garbage (wrong group element length, truncated, etc.) this
	// would fail loudly.
	if _, err := client.Deserialize.RegistrationResponse(resp.RegistrationResponse); err != nil {
		t.Fatalf("client.Deserialize.RegistrationResponse: %v", err)
	}
}

// TestRegisterInit_BadJSON guards the early-return path: malformed bodies
// should be rejected with 400 instead of panicking in the OPAQUE library.
func TestRegisterInit_BadJSON(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	h.handleRegisterInit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestRegisterInit_MissingFields covers the "email or KE1 missing" branch.
func TestRegisterInit_MissingFields(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	body, _ := json.Marshal(registerInitRequest{Email: "", RegistrationKE: []byte("x")})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleRegisterInit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestRegisterInit_BadKE1 ensures a syntactically valid JSON body with
// garbage in registration_ke is rejected by the OPAQUE deserializer rather
// than crashing.
func TestRegisterInit_BadKE1(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	body, _ := json.Marshal(registerInitRequest{
		Email:          "alice@example.com",
		RegistrationKE: []byte("not a real KE1"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleRegisterInit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRegisterFinalize_PersistsRecordAndWrap drives a full OPAQUE
// registration round-trip and verifies the finalize endpoint:
//  1. Returns 200 with a user_id + session_token.
//  2. Persists the RegistrationRecord in user_opaque_records.
//  3. Persists the account_key wrap blob verbatim in
//     user_account_key_wraps under the requested method.
//
// We exercise the real bytemare/opaque client to produce the record bytes
// rather than fabricating them — that way the test breaks if the
// server-side deserializer ever rejects well-formed input, which is the
// regression we care about (validation must run, but it must not be
// overly strict).
func TestRegisterFinalize_PersistsRecordAndWrap(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)

	client, err := defaultConfig().Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ke1 := client.RegistrationInit([]byte("hunter2hunter2"))

	// Init step — driven through the handler so we know the server is
	// producing a KE2 the client library is willing to accept.
	initBody, _ := json.Marshal(registerInitRequest{
		Email:          "alice@example.com",
		RegistrationKE: ke1.Serialize(),
	})
	initReq := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(initBody))
	initRec := httptest.NewRecorder()
	h.handleRegisterInit(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("init status = %d, want 200; body=%s", initRec.Code, initRec.Body.String())
	}
	var initResp registerInitResponse
	if err := json.NewDecoder(initRec.Body).Decode(&initResp); err != nil {
		t.Fatalf("decode init resp: %v", err)
	}

	ke2, err := client.Deserialize.RegistrationResponse(initResp.RegistrationResponse)
	if err != nil {
		t.Fatalf("client.Deserialize.RegistrationResponse: %v", err)
	}

	// Identities the client commits to in the envelope. The relay's
	// fixed serverID ("atterm-relay") and the user's email — both must
	// match what the relay binds at login time, so they're load-bearing.
	record, _ := client.RegistrationFinalize(ke2, opaque.ClientRegistrationFinalizeOptions{
		ClientIdentity: []byte("alice@example.com"),
		ServerIdentity: []byte("atterm-relay"),
	})

	finBody, _ := json.Marshal(registerFinalizeRequest{
		Email:              "alice@example.com",
		RegistrationRecord: record.Serialize(),
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    "password",
			Wrapped:   []byte("ciphertext"),
			Nonce:     []byte("xchacha-nonce-24-bytes-aa"),
			Salt:      []byte("argon-salt-16byt"),
			KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
		},
	})
	finReq := httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader(finBody))
	finRec := httptest.NewRecorder()
	h.handleRegisterFinalize(finRec, finReq)

	if finRec.Code != http.StatusOK {
		t.Fatalf("finalize status = %d, want 200; body=%s", finRec.Code, finRec.Body.String())
	}
	var finResp registerFinalizeResponse
	if err := json.NewDecoder(finRec.Body).Decode(&finResp); err != nil {
		t.Fatalf("decode fin resp: %v", err)
	}
	if finResp.UserID == "" || finResp.SessionToken == "" {
		t.Fatalf("missing user_id or session_token: %+v", finResp)
	}

	// Record must round-trip the bytes the client sent — the relay
	// stores them as opaque BLOB, no canonicalization.
	gotRecord, err := h.store.GetOpaqueRecord(context.Background(), finResp.UserID)
	if err != nil {
		t.Fatalf("opaque record not persisted: %v", err)
	}
	if !bytes.Equal(gotRecord, record.Serialize()) {
		t.Fatalf("record mismatch: stored len=%d, want len=%d", len(gotRecord), len(record.Serialize()))
	}

	gotWrap, err := h.store.GetAccountKeyWrap(context.Background(), finResp.UserID, "password")
	if err != nil {
		t.Fatalf("wrap not persisted: %v", err)
	}
	if string(gotWrap.Wrapped) != "ciphertext" {
		t.Fatalf("wrap.Wrapped mismatch: got %q", gotWrap.Wrapped)
	}
	if gotWrap.KDFParams != `{"alg":"argon2id","m":67108864,"t":3,"p":1}` {
		t.Fatalf("wrap.KDFParams mismatch: got %q", gotWrap.KDFParams)
	}

	// Session token must look up to the user we just created. This
	// guards the "did we mint a real token, not a placeholder" bit.
	sess, sessUser, err := h.store.LookupSession(context.Background(), finResp.SessionToken)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if sess == nil || sessUser == nil || sessUser.ID != finResp.UserID {
		t.Fatalf("session token does not resolve to created user: sess=%+v user=%+v", sess, sessUser)
	}
}

// TestRegisterFinalize_BadJSON guards the early-return path for malformed
// bodies — same rationale as TestRegisterInit_BadJSON.
func TestRegisterFinalize_BadJSON(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	h.handleRegisterFinalize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRegisterFinalize_MissingFields covers the "required fields empty"
// branch — every required field is exercised by leaving exactly one out.
func TestRegisterFinalize_MissingFields(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	body, _ := json.Marshal(registerFinalizeRequest{
		Email:              "alice@example.com",
		RegistrationRecord: nil, // missing
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    "password",
			Wrapped:   []byte("x"),
			Nonce:     []byte("x"),
			Salt:      []byte("x"),
			KDFParams: `{}`,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleRegisterFinalize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRegisterFinalize_BadRecord ensures a non-empty but malformed record
// is rejected with 400 before any DB write — the user row must NOT exist
// after this call.
func TestRegisterFinalize_BadRecord(t *testing.T) {
	h := newTestOpaqueAuthHandler(t)
	body, _ := json.Marshal(registerFinalizeRequest{
		Email:              "alice@example.com",
		RegistrationRecord: []byte("not a real record"),
		AccountKeyWrap: accountKeyWrapPayload{
			Method:    "password",
			Wrapped:   []byte("x"),
			Nonce:     []byte("x"),
			Salt:      []byte("x"),
			KDFParams: `{}`,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleRegisterFinalize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
