// internal/relay/feishu_http_test.go
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/feishu"
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
	return NewFeishuHTTPHandler(st, svc), stub
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
