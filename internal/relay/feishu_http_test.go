// internal/relay/feishu_http_test.go
package relay

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
)

// stubFeishuServer mimics the Feishu /open-apis routes used by the
// relay's validation step (token mint) and the bind reply (im send).
type stubFeishuServer struct {
	tokenCode int
	tokenStr  string
}

func newStubFeishu(t *testing.T, sf *stubFeishuServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": sf.tokenCode, "msg": "ok",
			"tenant_access_token": sf.tokenStr, "expire": 7200,
		})
	})
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestUserStoreWithCipher(t *testing.T) *userstore.SQLiteStore {
	t.Helper()
	key := bytes.Repeat([]byte{0x11}, 32)
	c, err := userstore.NewSecretCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st, err := userstore.Open(context.Background(), ":memory:", userstore.WithSecretCipher(c))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// ctxWithUser injects a userstore.User via the canonical userCtxKey{}
// so production code's UserFromContext call returns the right ID.
func ctxWithUser(parent context.Context, userID string) context.Context {
	return context.WithValue(parent, userCtxKey{}, &userstore.User{ID: userID})
}

func newFeishuTestHandler(t *testing.T, st *userstore.SQLiteStore, tokenCode int, tokenStr string) (*FeishuHTTPHandler, *httptest.Server) {
	t.Helper()
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: tokenCode, tokenStr: tokenStr})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st),
		IM:    feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	return NewFeishuHTTPHandler(st, svc, session.NewRegistry()), stub
}

func TestFeishuHTTP_UpsertBinding_GoodCreds(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "fhh@example.com")
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	body, _ := json.Marshal(map[string]string{
		"app_id": "cli_xx", "app_secret": "ss",
		"encrypt_key": "ekekek", "verify_token": "vtvt",
	})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me", bytes.NewReader(body))
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		AppIDHash   string `json:"app_id_hash"`
		CallbackURL string `json:"callback_url"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.AppIDHash == "" || !strings.HasSuffix(resp.CallbackURL, resp.AppIDHash) {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestFeishuHTTP_UpsertBinding_BadCreds(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "fhb@example.com")
	h, _ := newFeishuTestHandler(t, st, 99991663, "")

	body, _ := json.Marshal(map[string]string{"app_id": "cli_x", "app_secret": "bad", "encrypt_key": "k", "verify_token": "v"})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me", bytes.NewReader(body))
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFeishuHTTP_BeginPair(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "bp@example.com")
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	// Need an existing binding first.
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_bp", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me/begin-pair", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Code      string `json:"code"`
		ExpiresAt int64  `json:"expires_at"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Code) != 6 || resp.ExpiresAt == 0 {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestFeishuHTTP_GetBindingMe(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "gm@example.com")
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	// Empty case.
	req := httptest.NewRequest("GET", "/v1/feishu/bindings/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	var resp1 struct {
		Configured bool `json:"configured"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp1)
	if resp1.Configured {
		t.Fatalf("should not be configured")
	}

	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	rr = httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	var resp2 struct {
		Configured  bool   `json:"configured"`
		Bound       bool   `json:"bound"`
		CallbackURL string `json:"callback_url"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp2)
	if !resp2.Configured || resp2.Bound || resp2.CallbackURL == "" {
		t.Fatalf("resp: %+v", resp2)
	}
}

func TestFeishuHTTP_DeleteBinding(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "del@example.com")
	h, _ := newFeishuTestHandler(t, st, 0, "tt")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})

	req := httptest.NewRequest("DELETE", "/v1/feishu/bindings/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)
	if rr.Code != 204 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := st.GetFeishuBinding(ctx, u.ID); err != userstore.ErrFeishuBindingNotFound {
		t.Fatalf("expected gone: %v", err)
	}
}

func feishuEncryptForTest(t *testing.T, encryptKey string, plain []byte) []byte {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, _ := aes.NewCipher(key[:])
	iv := bytes.Repeat([]byte{0x33}, aes.BlockSize)
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append([]byte{}, plain...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	combined := append(append([]byte{}, iv...), ct...)
	body, _ := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(combined)})
	return body
}

func TestFeishuHTTP_EventCallback_URLVerification(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "uv@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_uv", AppSecret: "s", EncryptKey: "ek-uv", VerifyToken: "vt-uv",
	})
	b, _ := st.GetFeishuBinding(ctx, u.ID)

	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	plain := []byte(`{"type":"url_verification","challenge":"xyz","token":"vt-uv"}`)
	body := feishuEncryptForTest(t, "ek-uv", plain)
	req := httptest.NewRequest("POST", "/v1/feishu/events/"+b.AppIDHash, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTPEvents(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Challenge != "xyz" {
		t.Fatalf("challenge: %q", resp.Challenge)
	}
}

func TestFeishuHTTP_EventCallback_UnknownHash(t *testing.T) {
	st := newTestUserStoreWithCipher(t)
	h, _ := newFeishuTestHandler(t, st, 0, "tt")
	req := httptest.NewRequest("POST", "/v1/feishu/events/00000000000000000000000000000000", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTPEvents(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unknown hash should still 200, got %d", rr.Code)
	}
}

// TestFeishuHTTP_EventCallback_LogsRejectReason: a misconfigured event (here a
// plaintext body when the relay requires encryption) must still 200 but log the
// short reject reason, so silent URL-verification failures are diagnosable.
func TestFeishuHTTP_EventCallback_LogsRejectReason(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "rej@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_rej", AppSecret: "s", EncryptKey: "ek-rej", VerifyToken: "vt-rej",
	})
	b, _ := st.GetFeishuBinding(ctx, u.ID)
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	// Plaintext url_verification (no "encrypt" field) → DecryptEnvelope rejects.
	req := httptest.NewRequest("POST", "/v1/feishu/events/"+b.AppIDHash,
		bytes.NewReader([]byte(`{"type":"url_verification","challenge":"x","token":"vt-rej"}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTPEvents(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if got := buf.String(); !strings.Contains(got, "decrypt_failed") {
		t.Fatalf("want decrypt_failed logged, got %q", got)
	}
}

func TestFeishuHTTP_EventCallback_CardAckUpdates(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ack@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_ack", AppSecret: "s", EncryptKey: "ek-ack", VerifyToken: "vt-ack",
	})
	b, _ := st.GetFeishuBinding(ctx, u.ID)
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	plain := []byte(`{"header":{"event_type":"card.action.trigger","token":"vt-ack","app_id":"cli_ack"},"event":{"action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},"operator":{"open_id":"ou_x"}}}`)
	body := feishuEncryptForTest(t, "ek-ack", plain)
	req := httptest.NewRequest("POST", "/v1/feishu/events/"+b.AppIDHash, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTPEvents(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "已确认") || !strings.Contains(rr.Body.String(), `"update_multi":true`) {
		t.Fatalf("expected update card in body: %s", rr.Body.String())
	}
}

func TestFeishuHTTP_RelayToken_Success(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "rt@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_rt", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	})
	_ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")
	h, _ := newFeishuTestHandler(t, st, 0, "tt-xyz")

	req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token     string `json:"tenant_access_token"`
		ExpiresIn int    `json:"expires_in"`
		OpenID    string `json:"open_id"`
		AppIDHash string `json:"app_id_hash"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Token != "tt-xyz" || resp.OpenID != "ou_user" {
		t.Fatalf("resp: %+v", resp)
	}
	if resp.AppIDHash == "" {
		t.Fatalf("expected app_id_hash in response")
	}
}

func TestFeishuHTTP_RelayToken_NoBinding(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "nb@example.com")
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestFeishuHTTP_RelayToken_Disabled(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "dis@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	})
	_ = st.MarkFeishuBindingDisabled(ctx, u.ID)
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)

	if rr.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rr.Code)
	}
}

func TestFeishuHTTP_RelayToken_UpstreamFail(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "up@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "x", AppSecret: "bad", EncryptKey: "k", VerifyToken: "v",
	})
	_ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")
	h, _ := newFeishuTestHandler(t, st, 99991000, "")

	req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
	req = req.WithContext(ctxWithUser(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFeishuHTTP_RelayToken_Unauthorized(t *testing.T) {
	st := newTestUserStoreWithCipher(t)
	h, _ := newFeishuTestHandler(t, st, 0, "tt")

	req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTPSession(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// TestFeishuRoutesRegisteredOnMux is the regression test for the missing
// /v1/feishu/relay-token/me route registration. The other RelayToken tests
// call h.ServeHTTPSession directly, bypassing the server mux — so they stayed
// green even while the route was never mounted, and the client's token-borrow
// POST hit Go's default mux and got a bare 404 "page not found" (mapped to
// ErrTokenNotConfigured → dispatch silently dropped → no Feishu card ever sent).
//
// This drives requests through the real Server.ServeHTTP so an unregistered
// path is observably different: a mounted-but-unauthenticated route returns
// 401 (requireSession), whereas an unmounted path returns 404 (mux miss). We
// assert every authenticated session route — including relay-token — is
// mounted by checking none of them 404 without a token.
func TestFeishuRoutesRegisteredOnMux(t *testing.T) {
	srv, _, _ := serverWithSessionAndUser(t) // Store is *SQLiteStore → feishu routes register

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/feishu/bindings/me"},
		{http.MethodPost, "/v1/feishu/bindings/me"},
		{http.MethodDelete, "/v1/feishu/bindings/me"},
		{http.MethodPost, "/v1/feishu/bindings/me/begin-pair"},
		{http.MethodPost, "/v1/feishu/relay-token/me"},
	}
	for _, rt := range routes {
		// No Authorization header: a registered route is gated by
		// requireSession → 401; an unregistered path → mux 404.
		req := httptest.NewRequest(rt.method, rt.path, nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Fatalf("%s %s not registered on mux (got 404 — route missing)", rt.method, rt.path)
		}
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401 (registered, unauthenticated), got %d", rt.method, rt.path, rr.Code)
		}
	}
}
