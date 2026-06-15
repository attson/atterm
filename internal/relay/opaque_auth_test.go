package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
