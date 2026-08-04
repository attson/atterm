package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// withWrap seeds the store with a password-method wrap for userID. Returns
// the wrap so tests can compare what comes back over the wire.
func withWrap(t *testing.T, store *userstore.DBStore, userID string) userstore.AccountKeyWrap {
	t.Helper()
	wrap := userstore.AccountKeyWrap{
		UserID:    userID,
		Method:    "password",
		Wrapped:   []byte("ciphertext-bytes-not-validated-by-relay"),
		Nonce:     []byte("nonce-24-byte-fixed-aaaaa"),
		Salt:      []byte("salt-16-bytesAAA"),
		KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
	}
	if err := store.StoreAccountKeyWrap(context.Background(), wrap); err != nil {
		t.Fatalf("StoreAccountKeyWrap: %v", err)
	}
	return wrap
}

func TestGetMeKey_ReturnsWrap(t *testing.T) {
	s, tok, uid := serverWithSessionAndUser(t)
	wrap := withWrap(t, s.Store(), uid)

	req := httptest.NewRequest(http.MethodGet, "/api/me/key", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got meKeyWrapPayload
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Method != wrap.Method ||
		string(got.Wrapped) != string(wrap.Wrapped) ||
		string(got.Nonce) != string(wrap.Nonce) ||
		string(got.Salt) != string(wrap.Salt) ||
		got.KDFParams != wrap.KDFParams {
		t.Fatalf("wrap mismatch: got=%+v want=%+v", got, wrap)
	}
}

func TestGetMeKey_NoWrap_404(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me/key", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetMeKey_NoSession_401(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me/key", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPutMeKey_StoresWrap(t *testing.T) {
	s, tok, uid := serverWithSessionAndUser(t)

	payload := meKeyWrapPayload{
		Method:    "password",
		Wrapped:   []byte("freshly-rewrapped-ciphertext"),
		Nonce:     []byte("new-nonce-24-byte-padBBBB"),
		Salt:      []byte("new-salt-16-byte"),
		KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/me/key", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	got, err := s.Store().GetAccountKeyWrap(context.Background(), uid, "password")
	if err != nil {
		t.Fatalf("GetAccountKeyWrap: %v", err)
	}
	if string(got.Wrapped) != string(payload.Wrapped) {
		t.Fatalf("wrap not persisted: got %q want %q", got.Wrapped, payload.Wrapped)
	}
}

func TestPutMeKey_Overwrites(t *testing.T) {
	s, tok, uid := serverWithSessionAndUser(t)
	withWrap(t, s.Store(), uid)

	payload := meKeyWrapPayload{
		Method:    "password",
		Wrapped:   []byte("after-password-change"),
		Nonce:     []byte("rotated-nonce-24-byteCC"),
		Salt:      []byte("rotated-salt-16b"),
		KDFParams: `{"alg":"argon2id","m":131072,"t":4,"p":1}`,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/me/key", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	got, err := s.Store().GetAccountKeyWrap(context.Background(), uid, "password")
	if err != nil {
		t.Fatalf("GetAccountKeyWrap: %v", err)
	}
	if string(got.Wrapped) != "after-password-change" {
		t.Fatalf("old wrap not overwritten: %q", got.Wrapped)
	}
	if got.KDFParams != payload.KDFParams {
		t.Fatalf("kdf_params not updated: %q", got.KDFParams)
	}
}

func TestPutMeKey_BadJSON_400(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	req := httptest.NewRequest(http.MethodPut, "/api/me/key", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutMeKey_MissingFields_400(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	cases := []meKeyWrapPayload{
		{Method: "", Wrapped: []byte("x"), Nonce: []byte("x"), Salt: []byte("x"), KDFParams: "x"},
		{Method: "password", Wrapped: nil, Nonce: []byte("x"), Salt: []byte("x"), KDFParams: "x"},
		{Method: "password", Wrapped: []byte("x"), Nonce: nil, Salt: []byte("x"), KDFParams: "x"},
		{Method: "password", Wrapped: []byte("x"), Nonce: []byte("x"), Salt: nil, KDFParams: "x"},
		{Method: "password", Wrapped: []byte("x"), Nonce: []byte("x"), Salt: []byte("x"), KDFParams: ""},
	}
	for i, p := range cases {
		body, _ := json.Marshal(p)
		req := httptest.NewRequest(http.MethodPut, "/api/me/key", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, want 400", i, rec.Code)
		}
	}
}

func TestPutMeKey_UnsupportedMethod_400(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	body, _ := json.Marshal(meKeyWrapPayload{
		Method:    "passkey", // future method, not yet supported
		Wrapped:   []byte("x"),
		Nonce:     []byte("x"),
		Salt:      []byte("x"),
		KDFParams: "x",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/key", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsupported method") {
		t.Fatalf("body does not mention unsupported method: %s", rec.Body.String())
	}
}

func TestPutMeKey_NoSession_401(t *testing.T) {
	s, _ := serverWithSession(t)
	body, _ := json.Marshal(meKeyWrapPayload{
		Method:    "password",
		Wrapped:   []byte("x"),
		Nonce:     []byte("x"),
		Salt:      []byte("x"),
		KDFParams: "x",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/key", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
