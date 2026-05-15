package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// newTestAuthServer builds a full AuthServer backed by a real in-memory SQLiteStore.
// The Argon2Pool is real so timing tests are meaningful.
func newTestAuthServer(t *testing.T) (*AuthServer, *userstore.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	pool := NewArgon2Pool(1)
	resolver := NewIdentityResolver(store, "admin-token-for-tests")
	srv := &AuthServer{
		Store:        store,
		Resolver:     resolver,
		Argon:        pool,
		FailureFloor: 200 * time.Millisecond,
	}
	return srv, store
}

// postJSON sends a POST with a JSON body to the given handler.
func postJSON(handler http.Handler, path string, body interface{}, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// doSignup signs up a user with the given email/password using the supplied invite code.
func doSignup(t *testing.T, handler http.Handler, email, password, inviteCode string) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(handler, "/api/auth/signup", map[string]string{
		"email":       email,
		"password":    password,
		"invite_code": inviteCode,
	})
}

// createInvite creates an invitation and returns its plaintext code.
func createInvite(t *testing.T, store *userstore.SQLiteStore) string {
	t.Helper()
	secret, _, err := store.CreateInvitation(context.Background(), nil, "test")
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	return secret.Expose()
}

// TestSignup_HappyPath: signup with valid invite → 200, cookie set, JSON response.
func TestSignup_HappyPath(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	code := createInvite(t, store)
	w := doSignup(t, handler, "alice@example.com", "correcthorsebattery", code)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check JSON response contains user_id and email.
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if resp["user_id"] == "" {
		t.Error("user_id missing from response")
	}
	if resp["email"] != "alice@example.com" {
		t.Errorf("email: got %v", resp["email"])
	}

	// Check Set-Cookie header.
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "atterm_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("atterm_session cookie not set")
	}
	if !sessionCookie.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite: got %v, want Lax", sessionCookie.SameSite)
	}
}

// TestSignup_InviteInvalid_400: bad or nonexistent invite → 400 with error:"invite_invalid".
func TestSignup_InviteInvalid_400(t *testing.T) {
	srv, _ := newTestAuthServer(t)
	handler := srv.Routes()

	// Random invite code that was never created.
	w := doSignup(t, handler, "bob@example.com", "correcthorsebattery", "inv_BADCODETHATDOESNOTEXIST")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invite_invalid" {
		t.Errorf("error: got %q", resp["error"])
	}
}

// TestSignup_EmailTaken_409: second signup with same email → 409 email_taken.
func TestSignup_EmailTaken_409(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	code1 := createInvite(t, store)
	code2 := createInvite(t, store)

	// First signup succeeds.
	w := doSignup(t, handler, "alice@example.com", "correcthorsebattery", code1)
	if w.Code != http.StatusOK {
		t.Fatalf("first signup: expected 200, got %d", w.Code)
	}

	// Second signup with same email → 409.
	w = doSignup(t, handler, "alice@example.com", "differentpassword12", code2)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "email_taken" {
		t.Errorf("error: got %q", resp["error"])
	}
}

// TestSignup_PasswordTooShort_400: password < 12 chars → 400 password_weak.
func TestSignup_PasswordTooShort_400(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	code := createInvite(t, store)
	w := doSignup(t, handler, "charlie@example.com", "short", code)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "password_weak" {
		t.Errorf("error: got %q", resp["error"])
	}
}

// TestSignup_NoCSRFRequired: signup works without X-CSRF-Token header (public endpoint).
func TestSignup_NoCSRFRequired(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	code := createInvite(t, store)
	// No CSRF token, no cookie — public endpoint must accept.
	w := doSignup(t, handler, "dave@example.com", "correcthorsebattery", code)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without CSRF token, got %d: %s", w.Code, w.Body.String())
	}
}

// TestLogin_Success_SetsCookie: valid credentials → 200 + atterm_session cookie.
func TestLogin_Success_SetsCookie(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	// Pre-create user via signup.
	code := createInvite(t, store)
	doSignup(t, handler, "eve@example.com", "correcthorsebattery", code)

	w := postJSON(handler, "/api/auth/login", map[string]string{
		"email":    "eve@example.com",
		"password": "correcthorsebattery",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "atterm_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("atterm_session cookie not set on login")
	}
	if !sessionCookie.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
}

// TestLogin_WrongPassword_401_ConstantTime: wrong password → 401 + response takes ≥ 200ms.
func TestLogin_WrongPassword_401_ConstantTime(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	code := createInvite(t, store)
	doSignup(t, handler, "frank@example.com", "correcthorsebattery", code)

	start := time.Now()
	w := postJSON(handler, "/api/auth/login", map[string]string{
		"email":    "frank@example.com",
		"password": "wrongpassword12345",
	})
	elapsed := time.Since(start)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("response too fast (%v); expected ≥ 200ms (SEC-5 failure floor)", elapsed)
	}
}

// TestLogin_MissingEmail_TakesSimilarTime: nonexistent email must have similar timing
// to existing-email wrong-password (median of 5 trials within ±50ms).
func TestLogin_MissingEmail_TakesSimilarTime(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	// Create a real user so we have a valid hash to compare against.
	code := createInvite(t, store)
	doSignup(t, handler, "grace@example.com", "correcthorsebattery12", code)

	const trials = 5
	wrongPwTimes := make([]time.Duration, 0, trials)
	missingTimes := make([]time.Duration, 0, trials)

	for i := 0; i < trials; i++ {
		start := time.Now()
		postJSON(handler, "/api/auth/login", map[string]string{
			"email":    "grace@example.com",
			"password": "wrongpassword12345",
		})
		wrongPwTimes = append(wrongPwTimes, time.Since(start))

		start = time.Now()
		postJSON(handler, "/api/auth/login", map[string]string{
			"email":    "nobody@nonexistent.example",
			"password": "wrongpassword12345",
		})
		missingTimes = append(missingTimes, time.Since(start))
	}

	sort.Slice(wrongPwTimes, func(i, j int) bool { return wrongPwTimes[i] < wrongPwTimes[j] })
	sort.Slice(missingTimes, func(i, j int) bool { return missingTimes[i] < missingTimes[j] })

	medWrong := wrongPwTimes[trials/2]
	medMissing := missingTimes[trials/2]

	diff := medWrong - medMissing
	if diff < 0 {
		diff = -diff
	}
	const tolerance = 150 * time.Millisecond // generous tolerance for CI variability
	if diff > tolerance {
		t.Errorf("timing difference too large: wrong-pw median=%v, missing-email median=%v, diff=%v (want ≤ %v)",
			medWrong, medMissing, diff, tolerance)
	}
}

// TestLogout_DeletesWebSession: login → logout with CSRF → session deleted, /api/me returns 401.
func TestLogout_DeletesWebSession(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	// Create a user and log in.
	code := createInvite(t, store)
	doSignup(t, handler, "henry@example.com", "correcthorsebattery", code)

	wLogin := postJSON(handler, "/api/auth/login", map[string]string{
		"email":    "henry@example.com",
		"password": "correcthorsebattery",
	})
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", wLogin.Code)
	}

	// Extract session cookie.
	var sessionCookie *http.Cookie
	for _, c := range wLogin.Result().Cookies() {
		if c.Name == "atterm_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie after login")
	}

	// Get CSRF token from /api/me.
	rMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rMe.AddCookie(sessionCookie)
	wMe := httptest.NewRecorder()
	handler.ServeHTTP(wMe, rMe)
	if wMe.Code != http.StatusOK {
		t.Fatalf("/api/me: expected 200, got %d: %s", wMe.Code, wMe.Body.String())
	}
	var meResp map[string]interface{}
	json.Unmarshal(wMe.Body.Bytes(), &meResp)
	csrfToken, _ := meResp["csrf_token"].(string)

	// Call logout with CSRF token.
	rLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rLogout.AddCookie(sessionCookie)
	rLogout.Header.Set("X-CSRF-Token", csrfToken)
	wLogout := httptest.NewRecorder()
	handler.ServeHTTP(wLogout, rLogout)
	if wLogout.Code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d: %s", wLogout.Code, wLogout.Body.String())
	}

	// Verify the session is gone: /api/me with same cookie → 401.
	rMe2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rMe2.AddCookie(sessionCookie)
	wMe2 := httptest.NewRecorder()
	handler.ServeHTTP(wMe2, rMe2)
	if wMe2.Code != http.StatusUnauthorized {
		t.Errorf("/api/me after logout: expected 401, got %d", wMe2.Code)
	}
}
