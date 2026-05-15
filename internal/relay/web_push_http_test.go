package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/webpush"
)

func newWebPushTestServer(t *testing.T) (*Server, *webpush.Service) {
	t.Helper()
	svc, err := webpush.Open(t.TempDir(), "mailto:test@example.com")
	if err != nil {
		t.Fatalf("webpush.Open: %v", err)
	}
	srv := NewServer(Config{
		Token:          "write-token",
		ReadOnlyTokens: []string{"read-token"},
		WebPush:        svc,
	})
	return srv, svc
}

func doRequest(t *testing.T, srv *Server, method, path, token, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Result()
}

func TestPushKeyReturnsBase64URLPublicKey(t *testing.T) {
	srv, svc := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Key string `json:"key"` }
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Key != svc.PublicKey() {
		t.Fatalf("Key = %q; want %q", out.Key, svc.PublicKey())
	}
}

func TestPushKeyAllowsReadOnlyToken(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "read-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
}

func TestPushKeyRejectsMissingToken(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d; want 401", resp.StatusCode)
	}
}

func TestPushKey503WhenWebPushDisabled(t *testing.T) {
	srv := NewServer(Config{Token: "write-token", WebPush: nil})
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "write-token", "")
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d; want 503", resp.StatusCode)
	}
}

func TestSubscribeHappyPath(t *testing.T) {
	srv, svc := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, raw)
	}
	subs := svc.SubscriptionsForUser(tokenHash("write-token"))
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/abc" {
		t.Fatalf("subs = %+v", subs)
	}
}

func TestSubscribe400OnInvalidJSON(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", "{not json}")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnHTTPEndpoint(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"http://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnMissingKeys(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"","auth":""}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc"}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", "write-token", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	resp2 := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", "write-token", body)
	if resp2.StatusCode != 200 {
		t.Fatalf("repeat status = %d; want 200", resp2.StatusCode)
	}
}

func TestTestNotificationCountsSubscriptions(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	_ = doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Sent int `json:"sent"` }
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 1 {
		t.Fatalf("sent = %d; want 1", out.Sent)
	}
}

func TestTestNotificationZeroWhenNoSubs(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Sent int `json:"sent"` }
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 0 {
		t.Fatalf("sent = %d; want 0", out.Sent)
	}
}
