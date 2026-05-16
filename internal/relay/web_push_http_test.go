package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
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

// newWebPushTestServerWithResolver creates a Server with an IdentityResolver
// backed by the given fakeStore, plus a webpush.Service.
func newWebPushTestServerWithResolver(t *testing.T, store userstore.Store) (*Server, *webpush.Service) {
	t.Helper()
	svc, err := webpush.Open(t.TempDir(), "mailto:test@example.com")
	if err != nil {
		t.Fatalf("webpush.Open: %v", err)
	}
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		WebPush:  svc,
		Resolver: resolver,
	})
	return srv, svc
}

// pushFakeStore is a fakeStore preset for the webpush HTTP tests.
// sessionMap maps cookie plaintext -> (userID, csrfSecret).
// apiTokenMap maps token plaintext -> (tokenID, userID).
type pushFakeStore struct {
	userstore.Store
	sessionMap  map[string][2]string // plaintext -> {userID, csrfSecretStr}
	apiTokenMap map[string][2]string // plaintext -> {tokenID, userID}
}

func (s *pushFakeStore) LookupWebSession(_ context.Context, plaintext string) (string, []byte, error) {
	if v, ok := s.sessionMap[plaintext]; ok {
		return v[0], []byte(v[1]), nil
	}
	return "", nil, userstore.ErrWebSessionInvalid
}

func (s *pushFakeStore) LookupAPIToken(_ context.Context, plaintext string) (string, string, error) {
	if v, ok := s.apiTokenMap[plaintext]; ok {
		return v[0], v[1], nil
	}
	return "", "", userstore.ErrTokenInvalid
}

// doRequestWithCookie creates a request with a session cookie (no bearer token).
func doRequestWithCookie(t *testing.T, srv *Server, method, path, cookieVal, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: "atterm_session", Value: cookieVal})
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Result()
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

// doRequestWithCookieAndCSRF creates a request with a session cookie and a
// matching X-CSRF-Token header derived from the cookie value and csrf secret.
func doRequestWithCookieAndCSRF(t *testing.T, srv *Server, method, path, cookieVal string, csrfSecret []byte, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: "atterm_session", Value: cookieVal})
	}
	if len(csrfSecret) > 0 && cookieVal != "" {
		req.Header.Set("X-CSRF-Token", CSRFToken(cookieVal, csrfSecret))
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Result()
}

// TestWebPushHTTP_RequiresUserPrincipal verifies that /api/push/subscribe,
// /api/push/unsubscribe, and /api/push/test require a User principal:
//   - cookie session with CSRF (PrincipalUser) → 200/OK
//   - admin bearer token (PrincipalAdmin) → 401 (RequireCSRF rejects: no cookie)
//   - no credentials (PrincipalNone) → 401
//
// Note: when cfg.Resolver is set, all three push routes are CSRF-gated.
// Admin Bearer tokens carry no cookie so they fail at the CSRF layer with 401
// (not 403 from requireUserPrincipal) — both are acceptable rejections.
func TestWebPushHTTP_RequiresUserPrincipal(t *testing.T) {
	csrfSecret := []byte("csrf-secret-for-push-test")
	const cookieVal = "valid-cookie"
	store := &pushFakeStore{
		sessionMap:  map[string][2]string{cookieVal: {"user1", string(csrfSecret)}},
		apiTokenMap: map[string][2]string{},
	}
	srv, _ := newWebPushTestServerWithResolver(t, store)

	validSubBody := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`

	type tc struct {
		name       string
		doReq      func() *http.Response
		wantStatus int
	}
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
			cases := []tc{
				{
					// CSRF-gated: cookie + matching CSRF token required.
					name: "cookie user → 200",
					doReq: func() *http.Response {
						return doRequestWithCookieAndCSRF(t, srv, route.method, route.path, cookieVal, csrfSecret, route.body)
					},
					wantStatus: 200,
				},
				{
					// Admin uses Bearer token (no cookie) → RequireCSRF returns 401.
					name: "admin token → 401",
					doReq: func() *http.Response {
						return doRequest(t, srv, route.method, route.path, "admin-token-for-push-test", route.body)
					},
					wantStatus: 401,
				},
				{
					name: "no auth → 401",
					doReq: func() *http.Response {
						return doRequest(t, srv, route.method, route.path, "", route.body)
					},
					wantStatus: 401,
				},
			}
			for _, c := range cases {
				c := c
				t.Run(c.name, func(t *testing.T) {
					resp := c.doReq()
					if resp.StatusCode != c.wantStatus {
						raw, _ := io.ReadAll(resp.Body)
						t.Fatalf("status = %d; want %d; body=%s", resp.StatusCode, c.wantStatus, raw)
					}
				})
			}
		})
	}
}

// TestWebPushHTTP_KeysSubscriptionByUserID verifies that a successful subscribe
// with user_A's cookie stores the subscription under user_A's ID, not under
// any other user's ID.
func TestWebPushHTTP_KeysSubscriptionByUserID(t *testing.T) {
	const (
		userAID     = "01HXAAAAAAAAAAAAAAAAAAAAAA"
		userBID     = "01HXBBBBBBBBBBBBBBBBBBBBBB"
		cookieUserA = "cookie-for-userA"
	)
	csrfSecret := []byte("csrf-secret-for-userA")
	store := &pushFakeStore{
		sessionMap:  map[string][2]string{cookieUserA: {userAID, string(csrfSecret)}},
		apiTokenMap: map[string][2]string{},
	}
	srv, svc := newWebPushTestServerWithResolver(t, store)

	body := `{"endpoint":"https://push.example/userA","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequestWithCookieAndCSRF(t, srv, http.MethodPost, "/api/push/subscribe", cookieUserA, csrfSecret, body)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("subscribe status = %d; body=%s", resp.StatusCode, raw)
	}

	// Subscription must appear under userA.
	subsA := svc.SubscriptionsForUser(userAID)
	if len(subsA) != 1 || subsA[0].Endpoint != "https://push.example/userA" {
		t.Fatalf("SubscriptionsForUser(userA) = %v; want 1 sub", subsA)
	}

	// Subscription must NOT appear under userB or any other key.
	subsB := svc.SubscriptionsForUser(userBID)
	if len(subsB) != 0 {
		t.Fatalf("SubscriptionsForUser(userB) = %v; want empty", subsB)
	}
}
