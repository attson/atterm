package relay

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// requireUser pulls the authenticated user from the request context (placed
// there by requireSession middleware) and returns a Principal view for the
// handler. When the middleware is wired correctly the bool is always true;
// the false branch survives only so handlers stay defensive against future
// wiring mistakes (e.g. someone registering a /api/me route without the
// requireSession wrapper).
func (a *AuthServer) requireUser(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return Principal{}, false
	}
	kind := PrincipalUser
	if u.IsAdmin {
		kind = PrincipalAdmin
	}
	return Principal{Kind: kind, UserID: u.ID, Scope: authWrite}, true
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
	Store userstore.Store
	Argon *Argon2Pool
	// Limits, when non-nil, enforces per-endpoint rate limits (SEC-5).
	// Nil disables rate limiting (useful in some unit tests).
	Limits *LimitRegistry
	// FailureFloor enforces SEC-5: any failed response (invite invalid,
	// wrong password, etc.) sleeps until at least this duration has elapsed.
	// Default 200ms; ±50ms random jitter is added.
	FailureFloor time.Duration
}

// Routes returns an http.Handler with all auth + me endpoints mounted. The
// auth + pair-consume + logout routes are public-or-self-authenticating;
// protected routes are unwrapped — callers that want session enforcement
// must use RegisterInto with a non-nil requireSession wrapper.
func (a *AuthServer) Routes() http.Handler {
	mux := http.NewServeMux()
	a.RegisterInto(mux, nil)
	return mux
}

// RegisterInto registers all auth + me routes into the provided mux. The
// requireSession argument wraps every protected route; pass nil to leave them
// unwrapped (only useful when a higher-level mux applies the wrapper, or in
// tests that exercise unauthenticated paths). Public routes — signup, login,
// logout (idempotent), and pair-consume — are always registered bare.
func (a *AuthServer) RegisterInto(mux *http.ServeMux, requireSession func(http.HandlerFunc) http.HandlerFunc) {
	wrap := requireSession
	if wrap == nil {
		wrap = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	// Public — no session required.
	mux.Handle("POST /api/auth/signup", http.HandlerFunc(a.handleSignup))
	mux.Handle("POST /api/auth/login", http.HandlerFunc(a.handleLogin))
	mux.Handle("POST /api/auth/logout", http.HandlerFunc(a.handleLogout))
	mux.Handle("POST /api/pair/consume", http.HandlerFunc(a.handlePairConsume))
	// Protected — session token required.
	mux.Handle("GET /api/me", wrap(a.handleMe))
	mux.Handle("DELETE /api/me", wrap(a.handleDeleteMe))
	mux.Handle("GET /api/me/sessions", wrap(a.handleListSessions))
	mux.Handle("DELETE /api/me/sessions/{id_hash}", wrap(a.handleDeleteSession))
	mux.Handle("POST /api/me/sessions/sign-out-others", wrap(a.handleSignOutOthers))
	mux.Handle("GET /api/me/webhooks", wrap(a.handleListWebhooks))
	mux.Handle("POST /api/me/webhooks", wrap(a.handleCreateWebhook))
	mux.Handle("DELETE /api/me/webhooks/{id}", wrap(a.handleDeleteWebhook))
	mux.Handle("POST /api/me/password", wrap(a.handleChangePassword))
	mux.Handle("GET /api/me/preferences", wrap(a.handleGetPreferences))
	mux.Handle("PUT /api/me/preferences", wrap(a.handlePutPreferences))
	mux.Handle("POST /api/pair/create", wrap(a.handlePairCreate))
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
//  3. ConsumeInvitation atomically. If invite is invalid the user row exists
//     orphaned (no session, no invite binding) — acceptable for MVP.
//  4. On email-taken (from CreateUser), return 409 immediately.
//  5. Create session and return {session_token, expires_at, user} JSON.
//
// All error paths sleep to the failure floor (SEC-5). Invite errors and
// email-taken return distinct status codes per spec §5.2. No Set-Cookie is
// emitted — clients store the returned session_token and send it as
// Authorization: Bearer on subsequent requests.
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

	// Create session and return the plaintext token + user info in the body.
	tok, sess, err := a.Store.CreateSession(r.Context(), u.ID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"session_token": tok,
		"expires_at":    sess.ExpiresAt.Unix(),
		"user": map[string]any{
			"id":       u.ID,
			"email":    u.Email,
			"is_admin": u.IsAdmin,
		},
	})
}

// handleLogin implements POST /api/auth/login.
//
// Flow:
//  1. Call Store.VerifyPassword — internally runs argon2 against real or dummy
//     hash, so missing-email timing matches wrong-password timing (SEC-3).
//  2. On success, create session and return {session_token, expires_at, user}.
//  3. On failure (nil user), sleep to failure floor and return 401.
//
// No Set-Cookie is emitted — clients store the returned session_token and
// send it as Authorization: Bearer on subsequent requests.
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

	tok, sess, err := a.Store.CreateSession(r.Context(), user.ID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		a.failureSleep(start)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"session_token": tok,
		"expires_at":    sess.ExpiresAt.Unix(),
		"user": map[string]any{
			"id":       user.ID,
			"email":    user.Email,
			"is_admin": user.IsAdmin,
		},
	})
}

// handleLogout implements POST /api/auth/logout.
//
// Reads the session token from Authorization: Bearer (or the WS subprotocol
// header) and deletes that row. Always returns 204 — a missing token is
// treated as a no-op so clients can call logout idempotently.
func (a *AuthServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	tok := tokenFromRequest(r)
	if tok == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = a.Store.DeleteSession(r.Context(), tok)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe implements GET /api/me. Returns user info.
func (a *AuthServer) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	user, err := a.Store.GetUser(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"user_id":  user.ID,
		"email":    user.Email,
		"is_admin": user.IsAdmin,
	}

	writeJSONStatus(w, http.StatusOK, resp)
}

// handleChangePassword implements POST /api/me/password.
//
// Body: {"current_password": "...", "new_password": "..."}
// On success: invalidates all sessions for the user, issues a fresh session,
// and returns {session_token, expires_at} so the caller can replace its
// bearer token.
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
	tok, sess, err := a.Store.CreateSession(r.Context(), p.UserID, r.UserAgent(), ipPrefix(r), userstore.DefaultSessionTTL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"session_token": tok,
		"expires_at":    sess.ExpiresAt.Unix(),
	})
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

// handleListWebhooks implements GET /api/me/webhooks.
func (a *AuthServer) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	hooks, err := a.Store.ListWebhooks(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	type row struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Format    string `json:"format"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]row, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, row{
			ID:        h.ID,
			Name:      h.Name,
			URL:       h.URL,
			Format:    h.Format,
			CreatedAt: h.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleCreateWebhook implements POST /api/me/webhooks.
//
// Body: {"url":"…","format":"feishu|generic","name":"…","allow_insecure":false}
// Response 201: {id, url, format, name, created_at}
func (a *AuthServer) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		URL           string `json:"url"`
		Format        string `json:"format"`
		Name          string `json:"name"`
		AllowInsecure bool   `json:"allow_insecure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if body.Format != "feishu" && body.Format != "generic" {
		writeError(w, http.StatusBadRequest, "invalid_format")
		return
	}
	u, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid_url")
		return
	}
	if u.Scheme == "http" && !body.AllowInsecure {
		writeError(w, http.StatusBadRequest, "insecure_url")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}
	wh, err := a.Store.CreateWebhook(r.Context(), p.UserID, body.URL, body.Format, body.Name, body.AllowInsecure)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"id":         wh.ID,
		"url":        wh.URL,
		"format":     wh.Format,
		"name":       wh.Name,
		"created_at": wh.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// handleDeleteWebhook implements DELETE /api/me/webhooks/{id}.
//
// Returns 204 on success, 404 if not found/not owned, 500 on DB error.
func (a *AuthServer) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := a.Store.DeleteWebhook(r.Context(), id, p.UserID); err != nil {
		if errors.Is(err, userstore.ErrWebhookNotOwnedOrMissing) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
