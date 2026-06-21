// binding_store_relay_test.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelayBackedBindingStore_Get(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feishu/bindings/me" || r.Method != "GET" {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-session-token" {
			t.Errorf("auth: %q", got)
		}
		called++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":   true,
			"bound":        true,
			"open_id":      "ou_relay",
			"disabled_at":  0,
			"callback_url": srv_url_for_test() + "/v1/feishu/events/HASH",
		})
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "test-session-token" })
	v, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.OpenID != "ou_relay" {
		t.Fatalf("OpenID: %q", v.OpenID)
	}
	if called != 1 {
		t.Fatalf("called: %d", called)
	}
}

func TestRelayBackedBindingStore_GetNotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": false, "bound": false})
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("expected ErrLocalBindingNotFound, got %v", err)
	}
}

func TestRelayBackedBindingStore_GetRelayDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mirrors the relay's serveFeishuSession 503 when the admin has
		// turned the integration off (handler not loaded).
		writeJSONStatusForTest(w, http.StatusServiceUnavailable, map[string]string{"error": "feishu integration disabled"})
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrRelayFeishuDisabled) {
		t.Fatalf("expected ErrRelayFeishuDisabled, got %v", err)
	}
}

func TestRelayBackedBindingStore_SetCredentials(t *testing.T) {
	var got struct {
		AppID       string `json:"app_id"`
		AppSecret   string `json:"app_secret"`
		EncryptKey  string `json:"encrypt_key"`
		VerifyToken string `json:"verify_token"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me" && r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"app_id_hash":  "h",
				"callback_url": "url",
			})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	err := s.SetCredentials(context.Background(), Credentials{
		AppID: "cli_z", AppSecret: "sec",
		EncryptKey: "ek", VerifyToken: "vt",
	})
	if err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if got.AppID != "cli_z" || got.AppSecret != "sec" {
		t.Fatalf("upstream got: %+v", got)
	}
}

func TestRelayBackedBindingStore_Delete(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me" && r.Method == "DELETE" {
			hit = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	if err := s.Delete(context.Background()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !hit {
		t.Fatalf("expected DELETE request")
	}
}

func TestRelayBackedBindingStore_SetBoundReturnsErrUnsupported(t *testing.T) {
	s := NewRelayBackedBindingStore("http://example", func() string { return "tok" })
	if err := s.SetBound(context.Background(), "x"); !errors.Is(err, ErrRelayManagedBoundState) {
		t.Fatalf("SetBound on relay-backed store must return ErrRelayManagedBoundState, got %v", err)
	}
}

// srv_url_for_test allows the callback_url string above to compile without
// importing httptest in the format string.
func srv_url_for_test() string { return "http://test" }

// writeJSONStatusForTest mirrors the relay's writeJSONStatus helper so the
// disabled-integration response shape matches production.
func writeJSONStatusForTest(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
