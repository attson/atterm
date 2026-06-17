// internal/feishu/token_test.go
package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTokenStubServer(t *testing.T, code int, token string, expiresIn int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":                code,
			"msg":                 "ok",
			"tenant_access_token": token,
			"expire":              expiresIn,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestTokenCache_FetchAndReuse(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_abc", 7200)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), func() time.Time { return time.Unix(1_000_000, 0) })

	tok, err := cache.Get(context.Background(), "app1", "secret1")
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	if tok != "t_abc" {
		t.Fatalf("token: %q", tok)
	}

	tok2, _ := cache.Get(context.Background(), "app1", "secret1")
	if tok2 != "t_abc" {
		t.Fatalf("cached token: %q", tok2)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

func TestTokenCache_Expires(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_short", 60)
	now := int64(1_000_000)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), func() time.Time { return time.Unix(atomic.LoadInt64(&now), 0) })

	_, _ = cache.Get(context.Background(), "app1", "s")
	atomic.StoreInt64(&now, 1_000_000+60+1) // past expire
	_, _ = cache.Get(context.Background(), "app1", "s")

	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected 2 calls after expiry, got %d", got)
	}
}

func TestTokenCache_Singleflight(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_sf", 7200)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), time.Now)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.Get(context.Background(), "app1", "s")
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("singleflight should coalesce; got %d upstream calls", got)
	}
}

func TestTokenCache_AuthClassError(t *testing.T) {
	srv, _ := newTokenStubServer(t, 99991663, "", 0) // invalid app_secret
	cache := NewTenantTokenCache(srv.URL, srv.Client(), time.Now)
	_, err := cache.Get(context.Background(), "app1", "s")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsAuthClassError(err) {
		t.Fatalf("expected auth-class error, got %v", err)
	}
}
