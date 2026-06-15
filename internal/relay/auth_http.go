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

// AuthServer hosts the /api/me, /api/auth/logout, /api/pair/*, and
// /api/me/webhooks routes. The historical /api/auth/signup and
// /api/auth/login routes were password-based and are gone; OPAQUE
// equivalents live at /api/auth/{register,login}/{init,finalize},
// registered separately by OpaqueAuthHandler.Register.
type AuthServer struct {
	Store userstore.Store
	// Limits, when non-nil, enforces per-endpoint rate limits (SEC-5).
	// Currently only invite-failure and signup limits are still relevant
	// for the surviving handlers (pair-consume / future invite use); the
	// login-failure limiter is consumed by the OPAQUE handler.
	Limits *LimitRegistry
	// FailureFloor enforces SEC-5: any failed response (invite invalid,
	// etc.) sleeps until at least this duration has elapsed.
	// Default 200ms; ±50ms random jitter is added.
	FailureFloor time.Duration
}

// Routes returns an http.Handler with all auth + me endpoints mounted. The
// pair-consume and logout routes are public-or-self-authenticating;
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
// tests that exercise unauthenticated paths). Public routes — logout
// (idempotent) and pair-consume — are always registered bare.
func (a *AuthServer) RegisterInto(mux *http.ServeMux, requireSession func(http.HandlerFunc) http.HandlerFunc) {
	wrap := requireSession
	if wrap == nil {
		wrap = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	// Public — no session required.
	mux.Handle("POST /api/auth/logout", http.HandlerFunc(a.handleLogout))
	mux.Handle("POST /api/pair/consume", http.HandlerFunc(a.handlePairConsume))
	// Retired endpoints — kept registered so old clients (bundled
	// internal/relay/web-dist/* + web/src/*) get a structured 410 instead
	// of a bare 404 when they POST password-auth payloads.
	mux.Handle("POST /api/auth/login", http.HandlerFunc(removedRoute))
	mux.Handle("POST /api/auth/signup", http.HandlerFunc(removedRoute))
	mux.Handle("POST /api/me/password", http.HandlerFunc(removedRoute))
	// Protected — session token required.
	mux.Handle("GET /api/me", wrap(a.handleMe))
	mux.Handle("DELETE /api/me", wrap(a.handleDeleteMe))
	mux.Handle("GET /api/me/sessions", wrap(a.handleListSessions))
	mux.Handle("DELETE /api/me/sessions/{id_hash}", wrap(a.handleDeleteSession))
	mux.Handle("POST /api/me/sessions/sign-out-others", wrap(a.handleSignOutOthers))
	mux.Handle("GET /api/me/webhooks", wrap(a.handleListWebhooks))
	mux.Handle("POST /api/me/webhooks", wrap(a.handleCreateWebhook))
	mux.Handle("DELETE /api/me/webhooks/{id}", wrap(a.handleDeleteWebhook))
	mux.Handle("GET /api/me/preferences", wrap(a.handleGetPreferences))
	mux.Handle("PUT /api/me/preferences", wrap(a.handlePutPreferences))
	mux.Handle("GET /api/me/key", wrap(a.handleGetMeKey))
	mux.Handle("PUT /api/me/key", wrap(a.handlePutMeKey))
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

// removedRoute responds 410 Gone for the legacy bcrypt auth endpoints
// that were retired in M1a (see docs/superpowers/specs/2026-06-15-relay-e2ee-design.md).
// Clients posting to these paths must upgrade to the OPAQUE flow at
// /api/auth/register/{init,finalize} and /api/auth/login/{init,finalize}.
func removedRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "endpoint removed; client must use OPAQUE flow at /api/auth/register and /api/auth/login (init/finalize)",
	})
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

// meKeyWrapPayload is the wire shape used by GET /api/me/key and
// PUT /api/me/key. The relay never inspects the encrypted bytes or KDF
// parameters; it only stores and returns them.
type meKeyWrapPayload struct {
	Method    string `json:"method"`
	Wrapped   []byte `json:"wrapped"`
	Nonce     []byte `json:"nonce"`
	Salt      []byte `json:"salt"`
	KDFParams string `json:"kdf_params"`
}

// handleGetMeKey implements GET /api/me/key. Returns the user's current
// password-method account_key wrap so a client that holds a session token
// but lost its in-memory account_key (page refresh, app relaunch with the
// keychain unavailable) can prompt the user for the password and unwrap
// locally. 404 if no wrap exists — this happens for users created outside
// the OPAQUE register flow (e.g. via legacy paths that no longer exist).
func (a *AuthServer) handleGetMeKey(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	wrap, err := a.Store.GetAccountKeyWrap(r.Context(), p.UserID, "password")
	if err != nil {
		if errors.Is(err, userstore.ErrAccountKeyWrapMissing) {
			http.Error(w, "wrap not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, meKeyWrapPayload{
		Method:    wrap.Method,
		Wrapped:   wrap.Wrapped,
		Nonce:     wrap.Nonce,
		Salt:      wrap.Salt,
		KDFParams: wrap.KDFParams,
	})
}

// handlePutMeKey implements PUT /api/me/key. Replaces the user's current
// password-method wrap blob — used by the password-change flow, where the
// client derives a new wrap_key from the new password and re-seals the
// existing account_key, then uploads the new envelope.
//
// The handler does NOT verify password possession on its own; the caller
// must complete OPAQUE step-up first (M1c). For M1b the route is gated by
// session bearer only; documented as a known gap.
//
// Returns 204 on success, 400 on invalid body, 500 on DB error.
func (a *AuthServer) handlePutMeKey(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req meKeyWrapPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Method == "" || len(req.Wrapped) == 0 || len(req.Nonce) == 0 ||
		len(req.Salt) == 0 || req.KDFParams == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	if req.Method != "password" {
		// Future methods (recovery_code, passkey) need their own validation.
		http.Error(w, "unsupported method", http.StatusBadRequest)
		return
	}
	if err := a.Store.StoreAccountKeyWrap(r.Context(), userstore.AccountKeyWrap{
		UserID:    p.UserID,
		Method:    req.Method,
		Wrapped:   req.Wrapped,
		Nonce:     req.Nonce,
		Salt:      req.Salt,
		KDFParams: req.KDFParams,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
