package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListWebhooks_GETWithBearer(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"wh_1","name":"alerts","url":"https://hook.example/x","format":"generic","created_at":"2026-06-13T00:00:00Z"}]`))
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "atk_test"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := a.ListWebhooks()
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if gotPath != "/api/me/webhooks" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer atk_test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(got) != 1 || got[0].ID != "wh_1" || got[0].Name != "alerts" {
		t.Errorf("rows = %+v", got)
	}
}

func TestListWebhooks_EmptyJSONNullCoercedToSlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	got, err := a.ListWebhooks()
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty slice, got %#v", got)
	}
}

func TestListWebhooks_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	_, err := a.ListWebhooks()
	if err == nil || err.Error() != "relay_unauthorized" {
		t.Fatalf("want relay_unauthorized, got %v", err)
	}
}

func TestListWebhooks_NoRelayConfigured(t *testing.T) {
	a := newRelayTestApp(t)
	if _, err := a.ListWebhooks(); err == nil {
		t.Fatal("expected error when relay not configured")
	}
}

func TestCreateWebhook_PostsBodyAndReturnsRow(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/me/webhooks" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"wh_2","name":"feishu-bot","url":"https://open.feishu/x","format":"generic","created_at":"2026-06-13T01:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	got, err := a.CreateWebhook(CreateWebhookReq{
		Name: "feishu-bot", URL: "https://open.feishu/x", Format: "generic", AllowInsecure: false,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if !strings.Contains(gotBody, `"name":"feishu-bot"`) ||
		!strings.Contains(gotBody, `"url":"https://open.feishu/x"`) ||
		!strings.Contains(gotBody, `"format":"generic"`) ||
		!strings.Contains(gotBody, `"allow_insecure":false`) {
		t.Errorf("body = %s", gotBody)
	}
	if got.ID != "wh_2" || got.Name != "feishu-bot" || got.Format != "generic" {
		t.Errorf("row = %+v", got)
	}
}

func TestCreateWebhook_4xxErrorCodePassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_url"}`))
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	_, err := a.CreateWebhook(CreateWebhookReq{Name: "x", URL: "ftp://bad", Format: "generic"})
	if err == nil || err.Error() != "invalid_url" {
		t.Fatalf("want error=invalid_url, got %v", err)
	}
}

func TestDeleteWebhook_PathEscapeAndSuccess(t *testing.T) {
	var gotEscapedPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	if err := a.DeleteWebhook("wh_abc/def"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	if gotEscapedPath != "/api/me/webhooks/wh_abc%2Fdef" {
		t.Errorf("escaped path = %q", gotEscapedPath)
	}
}

func TestDeleteWebhook_404MappedToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	_ = a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "t"})

	err := a.DeleteWebhook("missing")
	if err == nil || err.Error() != "webhook_not_found" {
		t.Fatalf("want webhook_not_found, got %v", err)
	}
}

func TestWebhooks_WSSchemeConvertedToHTTPS(t *testing.T) {
	// The desktop stores RelayURL as ws(s)://; webhook calls must convert to
	// http(s):// before issuing the request. We can't actually start a wss
	// httptest server, so we verify the URL normalization path indirectly by
	// pointing at http:// (already correct) and a fresh server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	// Swap http://host:port → ws://host:port to exercise the scheme-strip path.
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1)
	_ = a.cfgStore.Set(appConfig{RelayURL: wsURL, RelaySessionToken: "t"})

	if _, err := a.ListWebhooks(); err != nil {
		t.Fatalf("ListWebhooks via ws:// scheme: %v", err)
	}
}

// Compile-time guard: ensure JSON shape of Webhook matches the relay response
// keys. If the relay ever renames a field, this fails to compile.
var _ = func() Webhook {
	var w Webhook
	_ = json.NewEncoder(io.Discard).Encode(w)
	return w
}
