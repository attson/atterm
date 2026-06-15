package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
	"github.com/attson/atterm/internal/webpush"
)

// newWebPushTestServer builds a Server wired with an IdentityResolver + Store
// (so /api/push/* are gated by requireSession) plus a real webpush.Service.
// Returns the Server, the WebPush service, and a fresh user's session token.
func newWebPushTestServer(t *testing.T) (*Server, *webpush.Service, string, string) {
	t.Helper()
	svc, err := webpush.Open(t.TempDir(), "mailto:test@example.com")
	if err != nil {
		t.Fatalf("webpush.Open: %v", err)
	}
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "push@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "push-test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		WebPush:  svc,
		Resolver: resolver,
		Store:    store,
	})
	return srv, svc, tok, u.ID
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
	srv, svc, tok, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", tok, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct {
		Key string `json:"key"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Key != svc.PublicKey() {
		t.Fatalf("Key = %q; want %q", out.Key, svc.PublicKey())
	}
}

func TestPushKeyRejectsMissingToken(t *testing.T) {
	srv, _, _, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d; want 401", resp.StatusCode)
	}
}

func TestPushKey503WhenWebPushDisabled(t *testing.T) {
	store := userstore.NewInMemory(t)
	u, _ := store.CreateOpaqueUser(context.Background(), "p@b")
	tok, _, _ := store.CreateSession(context.Background(), u.ID, "p", "127.0.0.1", 24*time.Hour)
	srv := NewServer(Config{Store: store, Resolver: NewIdentityResolver(store) /* WebPush intentionally nil */})
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", tok, "")
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d; want 503", resp.StatusCode)
	}
}

func TestSubscribeHappyPath(t *testing.T) {
	srv, svc, tok, userID := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tok, body)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, raw)
	}
	subs := svc.SubscriptionsForUser(userID)
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/abc" {
		t.Fatalf("subs = %+v; want endpoint stored under user %s", subs, userID)
	}
}

func TestSubscribe400OnInvalidJSON(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tok, "{not json}")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnHTTPEndpoint(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	body := `{"endpoint":"http://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tok, body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnMissingKeys(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"","auth":""}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tok, body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc"}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", tok, body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	resp2 := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", tok, body)
	if resp2.StatusCode != 200 {
		t.Fatalf("repeat status = %d; want 200", resp2.StatusCode)
	}
}

func TestTestNotificationCountsSubscriptions(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	_ = doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tok, body)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", tok, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct {
		Sent int `json:"sent"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 1 {
		t.Fatalf("sent = %d; want 1", out.Sent)
	}
}

func TestTestNotificationZeroWhenNoSubs(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", tok, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct {
		Sent int `json:"sent"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 0 {
		t.Fatalf("sent = %d; want 0", out.Sent)
	}
}

// TestWebPushHTTP_RequiresUserPrincipal verifies the protected push routes
// reject missing/invalid Bearer tokens with 401 and accept valid session
// tokens.
func TestWebPushHTTP_RequiresUserPrincipal(t *testing.T) {
	srv, _, tok, _ := newWebPushTestServer(t)
	validSubBody := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/push/subscribe", validSubBody},
		{http.MethodPost, "/api/push/unsubscribe", `{"endpoint":"https://push.example/abc"}`},
		{http.MethodPost, "/api/push/test", ""},
	}

	for _, route := range routes {
		route := route
		t.Run(route.path, func(t *testing.T) {
			// Valid session token → 200.
			respOK := doRequest(t, srv, route.method, route.path, tok, route.body)
			if respOK.StatusCode != 200 {
				raw, _ := io.ReadAll(respOK.Body)
				t.Fatalf("valid session: status = %d; want 200; body=%s", respOK.StatusCode, raw)
			}

			// Bogus Bearer → 401.
			respBad := doRequest(t, srv, route.method, route.path, "ses_does_not_exist", route.body)
			if respBad.StatusCode != 401 {
				raw, _ := io.ReadAll(respBad.Body)
				t.Fatalf("bad bearer: status = %d; want 401; body=%s", respBad.StatusCode, raw)
			}

			// No auth → 401.
			respNone := doRequest(t, srv, route.method, route.path, "", route.body)
			if respNone.StatusCode != 401 {
				raw, _ := io.ReadAll(respNone.Body)
				t.Fatalf("no auth: status = %d; want 401; body=%s", respNone.StatusCode, raw)
			}
		})
	}
}

// TestWebPushHTTP_KeysSubscriptionByUserID verifies that subscribe with user_A's
// Bearer stores the subscription under user_A's ID, never under any other user.
func TestWebPushHTTP_KeysSubscriptionByUserID(t *testing.T) {
	srv, svc, tokA, userAID := newWebPushTestServer(t)
	// Mint a second user/session to confirm subscriptions don't bleed across.
	store := srv.cfg.Store.(*userstore.SQLiteStore)
	userB, _ := store.CreateOpaqueUser(context.Background(), "userb@example.com")

	body := `{"endpoint":"https://push.example/userA","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", tokA, body)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("subscribe status = %d; body=%s", resp.StatusCode, raw)
	}

	// Subscription must appear under userA.
	subsA := svc.SubscriptionsForUser(userAID)
	if len(subsA) != 1 || subsA[0].Endpoint != "https://push.example/userA" {
		t.Fatalf("SubscriptionsForUser(userA) = %v; want 1 sub", subsA)
	}

	// Subscription must NOT appear under userB.
	subsB := svc.SubscriptionsForUser(userB.ID)
	if len(subsB) != 0 {
		t.Fatalf("SubscriptionsForUser(userB) = %v; want empty", subsB)
	}
}
