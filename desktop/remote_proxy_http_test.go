package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startHTTPProxyApp seeds an App-less remoteProxy pointed at the given fake
// relay. The proxy reads relay URL/token from the configStore on each request,
// mirroring the WS proxy, so tests only need to seed config once.
func newHTTPProxyFixture(t *testing.T, relay *httptest.Server) *remoteProxy {
	t.Helper()
	store := newTestConfigStore(t)
	if err := store.Set(appConfig{
		RelayURL:          strings.Replace(relay.URL, "http://", "ws://", 1),
		RelaySessionToken: "atk_test",
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	p, err := startRemoteProxy(store)
	if err != nil {
		t.Fatalf("startRemoteProxy: %v", err)
	}
	t.Cleanup(p.Stop)
	return p
}

func TestRemoteProxyHTTP_ForwardsPathQueryAndInjectsAuth(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotMethod string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"u1"}]`))
	}))
	defer relay.Close()

	p := newHTTPProxyFixture(t, relay)

	resp, err := http.Get(p.httpURL() + "/relay-http/admin/api/users?limit=10")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if gotMethod != "GET" {
		t.Errorf("relay method = %q; want GET", gotMethod)
	}
	if gotPath != "/admin/api/users" {
		t.Errorf("relay path = %q; want /admin/api/users", gotPath)
	}
	if gotQuery != "limit=10" {
		t.Errorf("relay query = %q; want limit=10", gotQuery)
	}
	if gotAuth != "Bearer atk_test" {
		t.Errorf("relay Authorization = %q; want Bearer atk_test", gotAuth)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("proxy status = %d; want 200", resp.StatusCode)
	}
	if string(body) != `[{"id":"u1"}]` {
		t.Errorf("proxy body = %q; want relay body echoed", string(body))
	}
}

func TestRemoteProxyHTTP_PassesThroughNon200(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer relay.Close()

	p := newHTTPProxyFixture(t, relay)

	resp, err := http.Get(p.httpURL() + "/relay-http/admin/api/users")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	// A 401 from the relay must reach the frontend unchanged so apiFetch's
	// 401 -> /login.html bounce still works through the proxy.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("proxy status = %d; want 401 passthrough", resp.StatusCode)
	}
}

func TestRemoteProxyHTTP_ForwardsRequestBodyAndMethod(t *testing.T) {
	var gotBody, gotCT string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}))
	defer relay.Close()

	p := newHTTPProxyFixture(t, relay)

	req, _ := http.NewRequest("POST", p.httpURL()+"/relay-http/admin/api/invitations",
		strings.NewReader(`{"count":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST through proxy: %v", err)
	}
	defer resp.Body.Close()

	if gotBody != `{"count":1}` {
		t.Errorf("relay body = %q; want request body forwarded", gotBody)
	}
	if gotCT != "application/json" {
		t.Errorf("relay Content-Type = %q; want application/json forwarded", gotCT)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("proxy status = %d; want 201", resp.StatusCode)
	}
}

func TestRemoteProxyHTTP_NoRelayConfigReturns503(t *testing.T) {
	store := newTestConfigStore(t)
	// No RelayURL / token seeded.
	p, err := startRemoteProxy(store)
	if err != nil {
		t.Fatalf("startRemoteProxy: %v", err)
	}
	t.Cleanup(p.Stop)

	resp, err := http.Get(p.httpURL() + "/relay-http/admin/api/users")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("proxy status = %d; want 503 when no relay configured", resp.StatusCode)
	}
}

func TestRemoteProxyHTTPURL_EmptyWhenNil(t *testing.T) {
	var p *remoteProxy
	if got := p.httpURL(); got != "" {
		t.Errorf("nil proxy httpURL() = %q; want empty", got)
	}
}
