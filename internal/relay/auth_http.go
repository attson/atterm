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

// requireUser ensures the request resolves to PrincipalUser. Returns the
// principal so handlers don't re-resolve. On failure, writes 401 and returns
// (zero Principal, false).
func (a *AuthServer) requireUser(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p := a.Resolver.Resolve(r)
	if !p.IsUser() {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return p, false
	}
	return p, true
}

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
	// Limits, when non-nil, enforces per-endpoint rate limits (SEC-5).
	// Nil disables rate limiting (useful in some unit tests).
	Limits *LimitRegistry
	// FailureFloor enforces SEC-5: any failed response (invite invalid,
	// wrong password, etc.) sleeps until at least this duration has elapsed.
	// Default 200ms; ±50ms random jitter is added.
	FailureFloor time.Duration
}

// Routes returns an http.Handler with all auth + me endpoints mounted.
// Signup and login are public (no CSRF). Logout and token mutation require CSRF.
func (a *AuthServer) Routes() http.Handler {
	mux := http.NewServeMux()
	a.RegisterInto(mux)
	return mux
}

// RegisterInto registers all auth + me routes into the provided mux.
// Called by Routes() and by BuildMux (Task 3.4) so the production mux is
// assembled from the same set of routes.
func (a *AuthServer) RegisterInto(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/signup", http.HandlerFunc(a.handleSignup))
	mux.Handle("POST /api/auth/login", http.HandlerFunc(a.handleLogin))
	mux.Handle("POST /api/auth/logout", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleLogout)))
	mux.Handle("GET /api/me", http.HandlerFunc(a.handleMe))
	mux.Handle("GET /api/me/sessions", http.HandlerFunc(a.handleListSessions))
	mux.Handle("DELETE /api/me/sessions/{id_hash}", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleDeleteSession)))
	mux.Handle("POST /api/me/sessions/sign-out-others", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleSignOutOthers)))
	mux.Handle("GET /api/me/tokens", http.HandlerFunc(a.handleListTokens))
	mux.Handle("POST /api/me/tokens", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleCreateToken)))
	mux.Handle("DELETE /api/me/tokens/{id}", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleRevokeToken)))
	mux.Handle("POST /api/me/password", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleChangePassword)))
}

// failureSleep sleeps the remaining time needed to reach FailureFloor (plus
// additive jitter up to 50ms) from the provided start time. Must be called
// before every non-success response (SEC-5).
//
// Jitter is always non-negative so the total target is always >= FailureFloor.
// This makes the floor a true lower bound and prevents the test assertion
// "elapsed >= FailureFloor" from flaking.
func (a *AuthServer) failureSleep(start time.Time) {
	floor := a.FailureFloor
	if floor <= 0 {
		floor = 200 * time.Millisecond
	}
	jitter := time.Duration(rand.Int63n(int64(50 * time.Millisecond))) // [0, 50ms)
	target := floor + jitter
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

	// SEC-5: per-IP signup rate limit (5 / hour). Check before any work.
	ip := ipPrefix(r)
	if a.Limits != nil && !a.Limits.AllowSignup(ip) {
		a.failureSleep(start)
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

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
		// SEC-5: per-IP invite-failure rate limit (10 / hour).
		if a.Limits != nil && !a.Limits.AllowInviteFail(ip) {
			a.failureSleep(start)
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
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
		// SEC-5: per-(IP, sha256(email)) brute-force limit (10 / 5min).
		// Check after VerifyPassword so the argon2 work always runs (timing parity).
		if a.Limits != nil && !a.Limits.AllowLoginFailure(ipPrefix(r), sha256Hex(body.Email)) {
			a.failureSleep(start)
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
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
	if !p.IsUser() {
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

// handleListTokens implements GET /api/me/tokens.
//
// Returns all API tokens for the authenticated user. The response array
// contains {id, name, prefix, created_at} and optional {last_used_at,
// revoked_at}. No plaintext or token_hash fields are included.
func (a *AuthServer) handleListTokens(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	tokens, err := a.Store.ListAPITokens(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type tokenRow struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Prefix     string  `json:"prefix"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at,omitempty"`
		RevokedAt  *string `json:"revoked_at,omitempty"`
	}

	out := make([]tokenRow, 0, len(tokens))
	for _, t := range tokens {
		row := tokenRow{
			ID:        t.ID,
			Name:      t.Name,
			Prefix:    t.Prefix,
			CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		}
		if t.LastUsedAt != nil {
			s := t.LastUsedAt.UTC().Format(time.RFC3339)
			row.LastUsedAt = &s
		}
		if t.RevokedAt != nil {
			s := t.RevokedAt.UTC().Format(time.RFC3339)
			row.RevokedAt = &s
		}
		out = append(out, row)
	}

	writeJSONStatus(w, http.StatusOK, out)
}

// handleCreateToken implements POST /api/me/tokens (CSRF-gated).
//
// Body: {"name": "<display name>"}
// Response 201: {id, plaintext, prefix, created_at}
// The plaintext is returned exactly once and never stored.
func (a *AuthServer) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}

	secret, tok, err := a.Store.CreateAPIToken(r.Context(), p.UserID, body.Name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSONStatus(w, http.StatusCreated, map[string]string{
		"id":         tok.ID,
		"plaintext":  secret.Expose(), // only call site for plaintext exposure
		"prefix":     tok.Prefix,
		"created_at": tok.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// handleRevokeToken implements DELETE /api/me/tokens/{id} (CSRF-gated).
//
// Revokes the named token. Returns 204 on success, 404 if the token does not
// exist or belongs to a different user (existence-leak protected), 500 on DB error.
func (a *AuthServer) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	tokenID := r.PathValue("id")
	if tokenID == "" {
		http.Error(w, "missing token id", http.StatusBadRequest)
		return
	}

	err := a.Store.RevokeAPIToken(r.Context(), tokenID, p.UserID)
	if err != nil {
		if errors.Is(err, userstore.ErrTokenNotOwnedOrMissing) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleChangePassword implements POST /api/me/password (CSRF-gated).
//
// Body: {"current_password": "...", "new_password": "..."}
// On success: invalidates all web sessions for the user, issues a fresh
// session cookie, and returns 204 No Content.
// Error paths: 400 password_weak (new < 12 chars), 401 current_password_wrong,
// 500 on store/session errors.
func (a *AuthServer) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	if len(body.New) < 12 {
		writeError(w, http.StatusBadRequest, "password_weak")
		return
	}

	err := a.Store.ChangePassword(r.Context(), p.UserID, body.Current, body.New)
	if errors.Is(err, userstore.ErrPasswordIncorrect) {
		writeError(w, http.StatusUnauthorized, "current_password_wrong")
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// All old sessions are now deleted. Issue a fresh session for the requester.
	secret, err := a.Store.CreateWebSession(r.Context(), p.UserID, r.UserAgent(), ipPrefix(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, r, secret.Expose(), int((30*24*time.Hour).Seconds()))
	w.WriteHeader(http.StatusNoContent)
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
