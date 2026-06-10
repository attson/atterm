package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLoginRemoteRelay_PersistsSessionToken verifies that a successful
// /api/auth/login call against the relay parses the session_token out of
// the response and persists it (along with the relay URL) into cfgStore.
//
// The httptest.Server returns http://127.0.0.1:PORT. LoginRemoteRelay
// normalizes that to ws:// for storage (the persisted relay URL is always
// the WebSocket form; HTTP API calls translate scheme on the fly — see
// MarkSessionsSeen at app.go:589).
func TestLoginRemoteRelay_PersistsSessionToken(t *testing.T) {
	var gotPath string
	var gotBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.URL.Path != "/api/auth/login" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_token": "ses_test_abc",
			"expires_at":    9999999999,
			"user":          map[string]any{"id": "u_1", "email": "u@example.com"},
		})
	}))
	defer fake.Close()

	a := newRelayTestApp(t)
	if err := a.LoginRemoteRelay(fake.URL, "u@example.com", "pw", false); err != nil {
		t.Fatalf("LoginRemoteRelay: %v", err)
	}

	if gotPath != "/api/auth/login" {
		t.Fatalf("server path: got %q want /api/auth/login", gotPath)
	}
	if gotBody.Email != "u@example.com" || gotBody.Password != "pw" {
		t.Fatalf("server body: got %+v want email=u@example.com password=pw", gotBody)
	}

	cfg := a.GetRelayConfig()
	if cfg.Token != "ses_test_abc" {
		t.Fatalf("token: got %q want ses_test_abc", cfg.Token)
	}
	// httptest gives http://127.0.0.1:PORT; we persist as ws://127.0.0.1:PORT
	// because validateRelayEndpoint + the uplink expect the WS form.
	wantURL := "ws://" + strings.TrimPrefix(fake.URL, "http://")
	if cfg.URL != wantURL {
		t.Fatalf("url: got %q want %q", cfg.URL, wantURL)
	}
}

// TestLoginRemoteRelay_BadStatusReturnsError verifies that a non-2xx
// response from the relay is surfaced as an error and that no session
// token is persisted.
func TestLoginRemoteRelay_BadStatusReturnsError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer fake.Close()

	a := newRelayTestApp(t)
	if err := a.LoginRemoteRelay(fake.URL, "u@example.com", "pw", false); err == nil {
		t.Fatal("expected error on 401")
	}

	cfg := a.GetRelayConfig()
	if cfg.Token != "" {
		t.Fatalf("token persisted on failure: got %q want empty", cfg.Token)
	}
	if cfg.URL != "" {
		t.Fatalf("url persisted on failure: got %q want empty", cfg.URL)
	}
}
