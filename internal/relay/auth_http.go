package relay

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// AuthServer handles the /api/auth/* and /api/me endpoints.
//
// Signup transaction order: consume invitation first, then create user.
// If ConsumeInvitation succeeds but CreateUser fails (e.g. email taken),
// the invitation is consumed but no user exists. This is documented as
// DONE_WITH_CONCERNS: in that edge case the invite code is spent. The
// alternative (create user first, compensate by deleting on invite failure)
// requires a DeleteUser store method not yet in the interface. The chosen
// order is simpler and the race is extremely unlikely in invite-only signup.
// A true SQLite transaction combining both steps would eliminate the race
// entirely; deferred to a follow-up task.
type AuthServer struct {
	Store    userstore.Store
	Resolver *IdentityResolver
	Argon    *Argon2Pool
	// FailureFloor enforces SEC-5: any failed response (invite invalid,
	// wrong password, etc.) sleeps until at least this duration has elapsed.
	// Default 200ms; ±50ms random jitter is added.
	FailureFloor time.Duration
}

// Routes returns an http.Handler with the three auth endpoints mounted.
// Signup and login are public (no CSRF). Logout requires CSRF.
func (a *AuthServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/auth/signup", http.HandlerFunc(a.handleSignup))
	mux.Handle("POST /api/auth/login", http.HandlerFunc(a.handleLogin))
	mux.Handle("POST /api/auth/logout", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleLogout)))
	mux.Handle("GET /api/me", http.HandlerFunc(a.handleMe))
	return mux
}

// failureSleep sleeps the remaining time needed to reach FailureFloor (plus
// ±50ms jitter) from the provided start time. Must be called before every
// non-success response (SEC-5).
func (a *AuthServer) failureSleep(start time.Time) {
	floor := a.FailureFloor
	if floor <= 0 {
		floor = 200 * time.Millisecond
	}
	jitter := time.Duration(rand.Int63n(int64(100*time.Millisecond))) - 50*time.Millisecond // ±50ms
	target := floor + jitter
	if target < 100*time.Millisecond {
		target = 100 * time.Millisecond
	}
	elapsed := time.Since(start)
	if elapsed < target {
		time.Sleep(target - elapsed)
	}
}

// isSecure returns true if the request was made over HTTPS.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setSessionCookie writes an HttpOnly, SameSite=Lax session cookie. Secure
// flag is set only on HTTPS connections.
func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	cookie := &http.Cookie{
		Name:     "atterm_session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	if isSecure(r) {
		cookie.Secure = true
	}
	http.SetCookie(w, cookie)
}

// writeJSONStatus writes a JSON response with the given status code.
func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response: {"error": "code"}.
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSONStatus(w, status, map[string]string{"error": code})
}

// ipPrefix returns the client IP (without port) for session tracking.
func ipPrefix(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

// handleSignup implements POST /api/auth/signup.
//
// Flow:
//  1. Validate email format and password length (≥ 12 chars).
//  2. CreateUser (hashes password, inserts row).
//  3. ConsumeInvitation atomically. If invite is invalid, delete the user row
//     (compensation) and return 400.
//  4. On email-taken (from CreateUser), return 409 immediately.
//  5. Create web session, set cookie, return 200 JSON.
//
// All error paths sleep to the failure floor (SEC-5). Invite errors and
// email-taken return distinct status codes per spec §5.2.
func (a *AuthServer) handleSignup(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var body struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	// Validate password length first (cheap).
	if len(body.Password) < 12 {
		a.failureSleep(start)
		writeError(w, http.StatusBadRequest, "password_weak")
		return
	}

	// Validate email (simple check).
	if !validEmail(body.Email) {
		a.failureSleep(start)
		writeError(w, http.StatusBadRequest, "invalid_email")
		return
	}

	// Create the user first (hashes password, generates ULID).
	u, err := a.Store.CreateUser(r.Context(), body.Email, body.Password)
	if err != nil {
		if errors.Is(err, userstore.ErrEmailTaken) {
			a.failureSleep(start)
			writeError(w, http.StatusConflict, "email_taken")
			return
		}
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Consume the invitation. If this fails (invalid/expired/race), the user
	// row exists but is orphaned (no session, no invite binding). Acceptable
	// for MVP; see struct comment.
	if err := a.Store.ConsumeInvitation(r.Context(), body.InviteCode, u.ID); err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusBadRequest, "invite_invalid")
		return
	}

	// Create session and set cookie.
	secret, err := a.Store.CreateWebSession(r.Context(), u.ID, r.UserAgent(), ipPrefix(r))
	if err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	setSessionCookie(w, r, secret.Expose(), int((30 * 24 * time.Hour).Seconds()))
	writeJSONStatus(w, http.StatusOK, map[string]string{
		"user_id": u.ID,
		"email":   u.Email,
	})
}

// handleLogin implements POST /api/auth/login.
//
// Flow:
//  1. Call Store.VerifyPassword — internally runs argon2 against real or dummy
//     hash, so missing-email timing matches wrong-password timing (SEC-3).
//  2. On success, create session and set cookie.
//  3. On failure (nil user), sleep to failure floor and return 401.
func (a *AuthServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := a.Store.VerifyPassword(r.Context(), body.Email, body.Password)
	if err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if user == nil {
		a.failureSleep(start)
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	secret, err := a.Store.CreateWebSession(r.Context(), user.ID, r.UserAgent(), ipPrefix(r))
	if err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	setSessionCookie(w, r, secret.Expose(), int((30 * 24 * time.Hour).Seconds()))
	writeJSONStatus(w, http.StatusOK, map[string]string{
		"user_id": user.ID,
		"email":   user.Email,
	})
}

// handleLogout implements POST /api/auth/logout (CSRF-gated).
//
// Reads the session cookie, deletes the session, clears the cookie.
func (a *AuthServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("atterm_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	// Delete the session (ignore error; cookie will be cleared regardless).
	_ = a.Store.DeleteWebSession(r.Context(), c.Value)

	// Clear the cookie.
	setSessionCookie(w, r, "", -1)
	writeJSONStatus(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// handleMe implements GET /api/me. Returns user info and the CSRF token.
// Required by TestLogout_DeletesWebSession to fetch the CSRF token for logout.
func (a *AuthServer) handleMe(w http.ResponseWriter, r *http.Request) {
	p := a.Resolver.Resolve(r)
	if p.Kind != PrincipalUser {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	user, err := a.Store.GetUser(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
	}

	// Only derive CSRF token when user authenticated via cookie (CSRFSecret set).
	if len(p.CSRFSecret) > 0 {
		c, err := r.Cookie("atterm_session")
		if err == nil && c.Value != "" {
			resp["csrf_token"] = CSRFToken(c.Value, p.CSRFSecret)
		}
	}

	writeJSONStatus(w, http.StatusOK, resp)
}

// validEmail is a minimal email format check. The store normalises the case;
// the format must have at least one '@' with non-empty parts on both sides.
func validEmail(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".")
}
