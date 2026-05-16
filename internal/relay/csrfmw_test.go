package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// csrfFakeStore is a fakeStore that only overrides LookupWebSession, returning
// a fixed userID and csrfSecret for any non-empty cookie value.
type csrfFakeStore struct {
	userstore.Store // nil — unimplemented methods panic

	csrfSecret []byte
	userID     string
}

func (s *csrfFakeStore) LookupWebSession(_ context.Context, plaintext string) (string, []byte, error) {
	if plaintext == "" {
		return "", nil, userstore.ErrWebSessionInvalid
	}
	return s.userID, s.csrfSecret, nil
}

// GetUser returns a non-admin user — Resolve calls this after LookupWebSession
// to decide PrincipalUser vs PrincipalAdmin. csrf tests don't need admin.
func (s *csrfFakeStore) GetUser(_ context.Context, id string) (*userstore.User, error) {
	return &userstore.User{ID: id, IsAdmin: false}, nil
}

// okHandler returns a handler that writes 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// newCSRFSetup constructs a resolver backed by csrfFakeStore with the given
// secret and returns (resolver, cookieValue, expectedToken).
func newCSRFSetup(csrfSecret []byte) (*IdentityResolver, string, string) {
	store := &csrfFakeStore{csrfSecret: csrfSecret, userID: "u1"}
	resolver := NewIdentityResolver(store)
	cookieValue := "test-cookie-session-value"
	token := CSRFToken(cookieValue, csrfSecret)
	return resolver, cookieValue, token
}

func TestRequireCSRF_OkOnMatchingHeader(t *testing.T) {
	resolver, cookieValue, token := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: cookieValue})
	r.Header.Set("X-CSRF-Token", token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireCSRF_MissingHeader_403(t *testing.T) {
	resolver, cookieValue, _ := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: cookieValue})
	// X-CSRF-Token header intentionally omitted

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireCSRF_WrongToken_403(t *testing.T) {
	resolver, cookieValue, _ := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: cookieValue})
	r.Header.Set("X-CSRF-Token", "definitely-not-the-right-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireCSRF_GETBypasses(t *testing.T) {
	// Neither a cookie nor an X-CSRF-Token header — GET/HEAD must pass through.
	resolver := NewIdentityResolver(&csrfFakeStore{csrfSecret: []byte("s"), userID: "u1"})
	handler := RequireCSRF(resolver, okHandler)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/api/something", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", method, w.Code)
			}
		})
	}
}

func TestRequireCSRF_NoCookieReturns401(t *testing.T) {
	resolver, _, token := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	// Cookie intentionally omitted; token header present but irrelevant
	r.Header.Set("X-CSRF-Token", token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireCSRF_AdminCookieAccepted(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "1.2.3.0/24")
	resolver := NewIdentityResolver(store)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireCSRF(resolver, inner)

	req := httptest.NewRequest(http.MethodPost, "/anything", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
	req.Header.Set("X-CSRF-Token", CSRFToken(secret.Expose(), u.CSRFSecret()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin cookie + valid CSRF: status %d, want 200", rec.Code)
	}
}

// TestMuxEnumerator_EveryMutatingRouteWrapped fires synthetic requests at
// every known mutating route in the production mux and verifies that:
//   - Routes not flagged as isPublic must NOT return 200/201/204 without a
//     valid CSRF token or valid auth (i.e. they must be CSRF- or auth-gated).
//   - Public routes (signup, login, admin token-gated endpoints) are skipped
//     because they have their own protection gate that is not cookie+CSRF.
//
// If a new mutating route is added and not listed here, add it — the test is
// the living inventory of every state-changing HTTP endpoint.
func TestMuxEnumerator_EveryMutatingRouteWrapped(t *testing.T) {
	mux := BuildMux(testServerDeps(t))

	type routeCase struct {
		method   string
		path     string
		isPublic bool // exempt from CSRF check (public or admin-token-gated)
	}

	cases := []routeCase{
		// Public auth endpoints — no CSRF required by design.
		{"POST", "/api/auth/signup", true},
		{"POST", "/api/auth/login", true},
		// Cookie+CSRF-gated endpoints — must return 401 (no cookie) or 403 (bad CSRF).
		{"POST", "/api/auth/logout", false},
		{"POST", "/api/me/tokens", false},
		{"DELETE", "/api/me/tokens/testid", false},
		// Admin-token-gated endpoints — no cookie+CSRF, but admin Bearer token is required.
		// Treated as public here because any protection (401 from requireAdmin) is fine.
		{"POST", "/admin/api/invitations", true},
		{"POST", "/admin/api/users/u1/reset-password", true},
		{"POST", "/admin/api/users/u1/disable", true},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			// Deliberately omit cookies and X-CSRF-Token to test gate behaviour.
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if c.isPublic {
				// Public/admin-gated: any status is fine (handler decides).
				return
			}

			// CSRF-gated routes: must NOT succeed without a valid token.
			// Allowed responses: 401 (no session), 403 (bad CSRF), 405 (wrong method).
			// NOT allowed: 200, 201, 204, or any 5xx.
			code := rr.Code
			if code == http.StatusOK || code == http.StatusCreated || code == http.StatusNoContent {
				t.Errorf("%s %s: missing CSRF protection — got %d (want 401/403/405)", c.method, c.path, code)
			}
			if code >= 500 {
				t.Errorf("%s %s: server error %d — handler panicked or has a bug", c.method, c.path, code)
			}
		})
	}
}

// TestMuxEnumerator_PushRoutesCsrfGated verifies that the three mutating push routes
// on the production NewServer mux are CSRF-gated when Resolver is configured.
// Without a valid cookie+CSRF token they must return 401 or 403, never 200/201/204.
// This guards against the regression where push routes bypassed CSRF (Fix 2).
func TestMuxEnumerator_PushRoutesCsrfGated(t *testing.T) {
	deps := testServerDeps(t)
	srv := NewServer(Config{
		Resolver: deps.Resolver,
		Store:    deps.Store,
	})

	pushRoutes := []string{
		"/api/push/subscribe",
		"/api/push/unsubscribe",
		"/api/push/test",
	}

	for _, path := range pushRoutes {
		t.Run("POST "+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			// No cookie, no CSRF token — must be rejected.
			rr := httptest.NewRecorder()
			srv.mux.ServeHTTP(rr, req)

			code := rr.Code
			if code == http.StatusOK || code == http.StatusCreated || code == http.StatusNoContent {
				t.Errorf("POST %s: missing CSRF protection — got %d (want 401/403)", path, code)
			}
			if code >= 500 {
				t.Errorf("POST %s: server error %d — handler panicked or has a bug", path, code)
			}
		})
	}
}
