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

// okHandler returns a handler that writes 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// newCSRFSetup constructs a resolver backed by csrfFakeStore with the given
// secret and returns (resolver, cookieValue, expectedToken).
func newCSRFSetup(csrfSecret []byte) (*IdentityResolver, string, string) {
	store := &csrfFakeStore{csrfSecret: csrfSecret, userID: "u1"}
	resolver := NewIdentityResolver(store, "")
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
	resolver := NewIdentityResolver(&csrfFakeStore{csrfSecret: []byte("s"), userID: "u1"}, "")
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

func TestMuxEnumerator_EveryMutatingRouteWrapped(t *testing.T) {
	t.Skip("pending Task 3.4: BuildMux not yet exposed")
}
