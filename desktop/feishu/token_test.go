package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayBorrowedTokenSource_GetAndCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/relay-token/me" && r.Method == "POST" {
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tenant_access_token": "tt-1",
				"open_id":             "ou_x",
				"app_id_hash":         "h",
				"expires_in":          3000,
			})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	tok, openID, _, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok != "tt-1" || openID != "ou_x" {
		t.Fatalf("token: %q open: %q", tok, openID)
	}
	_, _, _, _ = ts.Get(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}
	ts.Invalidate()
	_, _, _, _ = ts.Get(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("expected 2 upstream calls after Invalidate, got %d", calls.Load())
	}
}

func TestRelayBorrowedTokenSource_NotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "feishu binding not configured", http.StatusNotFound)
	}))
	defer srv.Close()
	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	_, _, _, err := ts.Get(context.Background())
	if !errors.Is(err, ErrTokenNotConfigured) {
		t.Fatalf("want ErrTokenNotConfigured, got %v", err)
	}
}

func TestRelayBorrowedTokenSource_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disabled", http.StatusGone)
	}))
	defer srv.Close()
	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	_, _, _, err := ts.Get(context.Background())
	if !errors.Is(err, ErrTokenDisabled) {
		t.Fatalf("want ErrTokenDisabled, got %v", err)
	}
}

func TestLocalTenantTokenSource_DelegatesToFeishuClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "ok",
			"tenant_access_token": "local-tt",
			"expire":              7200,
		})
	}))
	defer upstream.Close()

	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{
		AppID: "cli_x", AppSecret: "sec",
		EncryptKey: "ek", VerifyToken: "vt",
	})
	_ = store.SetBound(context.Background(), "ou_local")

	ts := NewLocalTenantTokenSource(store, upstream.URL, nil, func() time.Time { return time.Unix(1000, 0) })
	tok, openID, hash, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok != "local-tt" {
		t.Fatalf("token: %q", tok)
	}
	if openID != "ou_local" {
		t.Fatalf("open: %q", openID)
	}
	if hash == "" {
		t.Fatalf("hash empty")
	}
}
