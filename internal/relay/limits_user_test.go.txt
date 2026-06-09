package relay

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// newTestLimits returns a fresh LimitRegistry for use in rate-limit tests.
// Using a fresh registry per test ensures buckets don't leak across tests.
func newTestLimits() *LimitRegistry {
	return NewLimitRegistry()
}

// TestLogin_BruteForceLocked: 11 failed logins from the same (IP, sha256(email)) → 11th returns 429.
func TestLogin_BruteForceLocked(t *testing.T) {
	srv, store := newTestAuthServer(t)
	srv.Limits = newTestLimits()
	handler := srv.Routes()

	// Create a real user so VerifyPassword runs against a real hash (not dummy).
	// The first 10 attempts are wrong-password failures, 11th should be 429.
	code := createInvite(t, store)
	doSignup(t, handler, "brute@example.com", "correcthorsebattery", code)

	const limit = 10 // 10 per 5 min window
	var lastCode int
	for i := 0; i < limit+1; i++ {
		w := postJSON(handler, "/api/auth/login", map[string]string{
			"email":    "brute@example.com",
			"password": "wrongpassword12345",
		})
		lastCode = w.Code
		if i < limit {
			// First 10 must get 401 (invalid_credentials), not 429.
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d: got 429 too early (limit is %d)", i+1, limit)
			}
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("11th failed login: expected 429, got %d", lastCode)
	}
}

// TestSignup_RatePerIP: 6 signups from the same IP within an hour → 6th returns 429.
func TestSignup_RatePerIP(t *testing.T) {
	srv, store := newTestAuthServer(t)
	srv.Limits = newTestLimits()
	handler := srv.Routes()

	const limit = 5 // 5 per hour
	var lastCode int
	for i := 0; i < limit+1; i++ {
		code := createInvite(t, store)
		w := doSignup(t, handler, "signuprate@example.com", "correcthorsebattery", code)
		lastCode = w.Code
		if i < limit {
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("signup attempt %d: got 429 too early (limit is %d)", i+1, limit)
			}
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("6th signup: expected 429, got %d", lastCode)
	}
}

// TestInviteFail_RatePerIP: 11 failed invite consumptions from the same IP → 429.
// We isolate the invite-fail bucket by setting the signup limit very high so it
// does not interfere with reaching the invite-consumption code path.
func TestInviteFail_RatePerIP(t *testing.T) {
	srv, _ := newTestAuthServer(t)
	// signup limit = 1000 so it won't fire; invite-fail limit = 10 per hour.
	srv.Limits = newLimitRegistryForTest(1000, 1000, 10)
	handler := srv.Routes()

	const limit = 10 // 10 per hour
	var lastCode int
	for i := 0; i < limit+1; i++ {
		// Use a unique email per attempt so CreateUser succeeds and we reach
		// ConsumeInvitation, which is where the invite-fail limiter fires.
		email := fmt.Sprintf("invfail%d@example.com", i)
		w := doSignup(t, handler, email, "correcthorsebattery", "inv_BADCODETHATDOESNOTEXIST")
		lastCode = w.Code
		if i < limit {
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("invite-fail attempt %d: got 429 too early (limit is %d)", i+1, limit)
			}
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("11th invite fail: expected 429, got %d", lastCode)
	}
}

// testServerDeps returns a ServerDeps suitable for mux-enumeration tests.
// It wires a real in-memory store, identity resolver, argon pool, and limits.
func testServerDeps(t *testing.T) ServerDeps {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	limits := NewLimitRegistry()
	resolver := NewIdentityResolver(store)
	pool := NewArgon2Pool(1)
	auth := &AuthServer{
		Store:    store,
		Resolver: resolver,
		Argon:    pool,
		Limits:   limits,
	}
	admin := &AdminServer{
		Store:    store,
		Resolver: resolver,
	}
	return ServerDeps{
		Store:    store,
		Resolver: resolver,
		Argon:    pool,
		Limits:   limits,
		Auth:     auth,
		Admin:    admin,
	}
}
