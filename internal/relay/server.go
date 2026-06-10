// Package relay is the central WebSocket hub. Agents connect on /agent and
// publish PTY output; clients connect on /client to attach, receive scrollback
// + live output, and send IN/RESIZE frames. State is in-memory only.
package relay

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/attson/atterm/internal/webhook"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// Config configures a Server.
type Config struct {
	// WebFS is the static web client filesystem. Nil disables /.
	// Callers typically pass relay.EmbeddedWebFS() (prod) or os.DirFS(path) (dev).
	WebFS fs.FS
	// Version is the application version exposed to web clients.
	Version string
	// AllowedOrigins, when non-empty, gates browser WS upgrades by Origin host.
	// Empty allows any origin (dev mode).
	AllowedOrigins []string
	// Debug enables verbose relay interaction logs. PTY byte payloads are
	// summarized by default; set DebugPayload to include IN/OUT contents.
	Debug bool
	// DebugPayload includes IN/OUT bytes in debug logs. This may leak command
	// input or terminal output; only enable during local debugging.
	DebugPayload bool
	// DebugLog overrides where debug logs are written. Nil writes to stderr.
	DebugLog io.Writer
	// RateLimitPerMinute limits HTTP requests and WS upgrade attempts per
	// remote IP/token pair. Zero uses a conservative default; negative disables.
	RateLimitPerMinute int
	// MaxConnectionsPerKey limits active WS connections per remote IP/token
	// pair. Zero uses a conservative default; negative disables.
	MaxConnectionsPerKey int
	// AdminConfigStore persists admin API changes when configured.
	AdminConfigStore *AdminConfigStore
	// WebPush, when non-nil, enables the /api/push/* endpoints and web-push
	// fan-out on command-finish. May be nil to disable web push; the
	// TypeCommandEvent uplink handler still runs when Webhook is set.
	WebPush *webpush.Service
	// WebPushIdleTimeout sends an idle-timeout push when a running mirrored
	// session produces no output for this duration. Zero uses the default;
	// negative disables idle-timeout pushes.
	WebPushIdleTimeout time.Duration
	// Webhook, when non-nil, fires per-user outbound webhooks on command-finish.
	Webhook *webhook.Service
	// Resolver, when non-nil, enables Principal-based auth for cookie-bearing
	// HTTP routes (e.g. /admin, /api/me, the static handler). WebSocket and
	// /api/sessions routes are gated by requireSession (session-token only).
	Resolver *IdentityResolver
	// Store, when non-nil alongside Resolver, mounts the user-account HTTP API
	// (/api/auth/*, /api/me/*, /admin/api/invitations, /admin/api/users).
	// If nil the new routes are not registered (legacy mode).
	Store userstore.Store
}

// Server bundles the registry and HTTP handlers.
type Server struct {
	cfg         Config
	registry    *session.Registry
	mux         *http.ServeMux
	rate        *fixedWindowLimiter
	conns       *connectionLimiter
	startTime   time.Time
	uplinkCount int64 // atomic; read via UplinkCount()
}

// ServerDeps holds the constructed subsystems that BuildMux needs to wire
// the production HTTP routes. Tests create this directly; the production
// NewServer constructor builds it internally.
type ServerDeps struct {
	Store    userstore.Store
	Resolver *IdentityResolver
	Argon    *Argon2Pool
	Limits   *LimitRegistry
	Auth     *AuthServer
	Admin    *AdminServer
}

// BuildMux constructs and returns the HTTP mux with all auth and admin routes
// registered. It is exported so tests can build the full production mux for
// route-enumeration checks (Task 3.4 mux-enumerator test). The requireSession
// argument wraps every protected route; pass nil to leave protected routes
// unwrapped (tests only).
func BuildMux(d ServerDeps, requireSession func(http.HandlerFunc) http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	if d.Auth != nil {
		d.Auth.RegisterInto(mux, requireSession)
	}
	if d.Admin != nil {
		d.Admin.RegisterInto(mux, requireSession)
	}
	return mux
}

// NewServer builds a Server with its routes installed.
func NewServer(cfg Config) *Server {
	if cfg.DebugLog == nil {
		cfg.DebugLog = os.Stderr
	}
	rateLimit := cfg.RateLimitPerMinute
	if rateLimit == 0 {
		rateLimit = defaultRateLimitPerMinute
	}
	connLimit := cfg.MaxConnectionsPerKey
	if connLimit == 0 {
		connLimit = defaultMaxConnections
	}
	s := &Server{
		cfg:       cfg,
		registry:  session.NewRegistry(),
		mux:       http.NewServeMux(),
		rate:      newFixedWindowLimiter(rateLimit, time.Minute),
		conns:     newConnectionLimiter(connLimit),
		startTime: time.Now(),
	}
	// WebSocket + session-API routes — gated by requireSession.
	s.mux.HandleFunc("/agent", s.requireSession(s.handleAgentHTTP))
	s.mux.HandleFunc("/uplink", s.requireSession(s.handleUplinkHTTP))
	s.mux.HandleFunc("/client", s.requireSession(s.handleClientHTTP))
	s.mux.HandleFunc("/client-sessions", s.requireSession(s.handleClientSessionsHTTP))
	s.mux.HandleFunc("/api/sessions", s.requireSession(s.handleSessionsHTTP))
	// Public — anonymous traffic allowed.
	s.mux.HandleFunc("/api/version", s.handleVersionHTTP)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	// Admin-only — requireSession + is_admin flag (the latter checked by
	// requireAdminAccess).
	s.mux.HandleFunc("/admin/api/health", s.requireSession(s.requireAdminAccess(s.handleAdminHealthAPI)))
	s.mux.HandleFunc("/admin/health", s.requireSession(s.requireAdminAccess(s.handleAdminHealth)))
	s.mux.HandleFunc("/admin/api/config", s.requireSession(s.handleAdminConfigHTTP))
	// Web-push — all four routes need an authenticated user.
	s.mux.HandleFunc("/api/push/key", s.requireSession(s.handlePushKey))
	s.mux.HandleFunc("/api/push/subscribe", s.requireSession(s.handlePushSubscribe))
	s.mux.HandleFunc("/api/push/unsubscribe", s.requireSession(s.handlePushUnsubscribe))
	s.mux.HandleFunc("/api/push/test", s.requireSession(s.handlePushTest))
	if cfg.WebFS != nil {
		s.mux.Handle("/", newStaticHandler(cfg.Resolver, cfg.WebFS))
	}

	// Mount user-account HTTP API when both resolver and store are wired.
	// The Argon2Pool, LimitRegistry, AuthServer, and AdminServer are constructed
	// here so the same wiring runs in both the production binary and any test
	// that calls NewServer with a non-nil Resolver+Store. Resolver itself is
	// only consumed by newStaticHandler — auth & admin handlers read the user
	// from request context via the requireSession wrapper.
	if cfg.Resolver != nil && cfg.Store != nil {
		argon := NewArgon2Pool(runtime.NumCPU())
		limits := NewLimitRegistry()
		authSrv := &AuthServer{
			Store:        cfg.Store,
			Argon:        argon,
			Limits:       limits,
			FailureFloor: 200 * time.Millisecond,
		}
		adminSrv := &AdminServer{Store: cfg.Store}
		authSrv.RegisterInto(s.mux, s.requireSession)
		adminSrv.RegisterInto(s.mux, s.requireSession)
		s.mux.HandleFunc("POST /api/sessions/seen", s.requireSession(s.handleSessionsSeenHTTP))

		// Background goroutine: purge expired web sessions hourly.
		go func() {
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			// Derive a context tied to the server's lifecycle via a background ctx.
			// We use context.Background() here because NewServer has no ctx parameter;
			// the purge loop is best-effort and exits when the ticker fires after
			// process shutdown (acceptable for cleanup routines).
			ctx := context.Background()
			for {
				select {
				case <-t.C:
					if n, err := cfg.Store.PurgeExpiredSessions(ctx); err != nil {
						log.Printf("relay: PurgeExpiredSessions: %v", err)
					} else if n > 0 {
						log.Printf("relay: purged %d expired sessions", n)
					}
				}
			}
		}()
	}

	return s
}

type versionResponse struct {
	Version string `json:"version"`
}

// ServeHTTP makes Server an http.Handler. CORS headers are added unconditionally
// — they only matter for cross-origin REST callers (e.g. webviews fetching the
// /api/sessions endpoint), and are harmless to same-origin browser clients.
// WebSocket upgrades are gated separately by Origin validation in nhooyr.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	// Cloudflare Web Analytics: static.cloudflareinsights.com serves beacon.min.js;
	// cloudflareinsights.com receives the RUM beacon. Both are required when the
	// site is fronted by Cloudflare with Web Analytics enabled.
	//
	// script-src 'unsafe-eval': Naive UI bundles a lodash chunk whose first line
	// is `Function("return this")()` (the standard "get globalThis" polyfill).
	// Without 'unsafe-eval' the chunk fails to load and every page using Naive
	// UI components (login, settings, admin) breaks. Threat-model impact is
	// minimal: an XSS attacker already needs 'self' capability to inject any
	// script; gaining 'unsafe-eval' on top does not materially widen the gap.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss: https://cloudflareinsights.com; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-eval' https://static.cloudflareinsights.com; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Permissions-Policy", "clipboard-read=(self), clipboard-write=(self)")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.rate.allow(requestIPLimitKey(r)) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// Registry exposes the underlying session registry. The desktop app needs
// this so its uplink can subscribe to local mini-relay sessions by id.
func (s *Server) Registry() *session.Registry { return s.registry }

// removeSession removes a session from the registry and prunes any per-user
// seen rows for it. All session teardown paths funnel through here so the
// session_seen table does not accumulate rows for dead sessions.
func (s *Server) removeSession(id uuid.UUID) {
	s.registry.Remove(id)
	if s.cfg.Store != nil {
		_ = s.cfg.Store.PruneSeenSession(context.Background(), id.String())
	}
}

func (s *Server) acceptOptions() *websocket.AcceptOptions {
	return &websocket.AcceptOptions{
		InsecureSkipVerify: len(s.cfg.AllowedOrigins) == 0,
		OriginPatterns:     s.cfg.AllowedOrigins,
	}
}

func (s *Server) acceptOptionsWithAuthSubprotocol(r *http.Request) *websocket.AcceptOptions {
	opts := s.acceptOptions()
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		for _, sp := range strings.Split(p, ",") {
			sp = strings.TrimSpace(sp)
			if strings.HasPrefix(sp, "atterm-token.") || strings.HasPrefix(sp, "atterm-token-b64.") {
				opts.Subprotocols = []string{sp}
				break
			}
		}
	}
	return opts
}

func (s *Server) allowAuthenticatedRequest(w http.ResponseWriter, r *http.Request) bool {
	if tokenFromRequest(r) == "" {
		return true
	}
	if !s.rate.allow(requestLimitKey(r)) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return false
	}
	return true
}

func (s *Server) handleAgentHTTP(w http.ResponseWriter, r *http.Request) {
	// requireSession (mux wrapper) has already authenticated this request and
	// stashed the user in the request context.
	var ownerUserID string
	if u, ok := UserFromContext(r.Context()); ok {
		ownerUserID = u.ID
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	key := requestLimitKey(r)
	if !s.conns.acquire(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.release(key)
	c, err := websocket.Accept(w, r, s.acceptOptions())
	if err != nil {
		s.debugf("ws reject path=/agent remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
		return
	}
	s.debugf("ws accept path=/agent remote=%s origin=%q", r.RemoteAddr, r.Header.Get("Origin"))
	defer c.Close(websocket.StatusInternalError, "")
	s.handleAgent(r.Context(), c, ownerUserID)
}

// UplinkCount returns the current number of in-progress uplink WebSocket
// connections. Read via atomic load — safe to call from any goroutine.
func (s *Server) UplinkCount() int64 {
	return atomic.LoadInt64(&s.uplinkCount)
}

// StartTime returns the moment NewServer was called. Used by /admin/health
// to expose uptime without exposing a mutable clock to consumers.
func (s *Server) StartTime() time.Time {
	return s.startTime
}

func (s *Server) handleUplinkHTTP(w http.ResponseWriter, r *http.Request) {
	// requireSession (mux wrapper) has already authenticated this request and
	// stashed the user in the request context.
	var ownerUserID string
	if u, ok := UserFromContext(r.Context()); ok {
		ownerUserID = u.ID
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	key := requestLimitKey(r)
	if !s.conns.acquire(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.release(key)
	c, err := websocket.Accept(w, r, s.acceptOptions())
	if err != nil {
		s.debugf("ws reject path=/uplink remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
		return
	}
	s.debugf("ws accept path=/uplink remote=%s origin=%q", r.RemoteAddr, r.Header.Get("Origin"))
	atomic.AddInt64(&s.uplinkCount, 1)
	defer atomic.AddInt64(&s.uplinkCount, -1)
	defer c.Close(websocket.StatusInternalError, "")
	s.handleUplink(r.Context(), c, ownerUserID)
}

func (s *Server) handleClientHTTP(w http.ResponseWriter, r *http.Request) {
	// requireSession (mux wrapper) has already authenticated this request and
	// stashed the user in the request context.
	var (
		scope       = authWrite
		ownerUserID string
	)
	if u, ok := UserFromContext(r.Context()); ok {
		ownerUserID = u.ID
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	key := requestLimitKey(r)
	if !s.conns.acquire(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.release(key)
	// browsers need to negotiate the same subprotocol if used for auth
	opts := s.acceptOptionsWithAuthSubprotocol(r)
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.debugf("ws reject path=/client remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
		return
	}
	s.debugf("ws accept path=/client remote=%s origin=%q subprotocol=%q", r.RemoteAddr, r.Header.Get("Origin"), c.Subprotocol())
	defer c.Close(websocket.StatusInternalError, "")
	s.handleClient(r.Context(), c, scope, ownerUserID)
}

func (s *Server) handleClientSessionsHTTP(w http.ResponseWriter, r *http.Request) {
	// requireSession (mux wrapper) has already authenticated this request and
	// stashed the user in the request context.
	var ownerUserID string
	if u, ok := UserFromContext(r.Context()); ok {
		ownerUserID = u.ID
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	key := requestLimitKey(r)
	if !s.conns.acquire(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.release(key)
	c, err := websocket.Accept(w, r, s.acceptOptionsWithAuthSubprotocol(r))
	if err != nil {
		s.debugf("ws reject path=/client-sessions remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
		return
	}
	s.debugf("ws accept path=/client-sessions remote=%s origin=%q", r.RemoteAddr, r.Header.Get("Origin"))
	defer c.Close(websocket.StatusInternalError, "")
	s.handleClientSessions(r.Context(), c, ownerUserID)
}

func (s *Server) handleSessionsHTTP(w http.ResponseWriter, r *http.Request) {
	// requireSession (mux wrapper) has already authenticated this request and
	// stashed the user in the request context.
	var ownerUserID string
	if u, ok := UserFromContext(r.Context()); ok {
		ownerUserID = u.ID
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	var infos []proto.SessionInfo
	if ownerUserID != "" {
		infos = s.sessionInfoListForOwner(ownerUserID, s.seenForOwner(r.Context(), ownerUserID))
	} else {
		infos = s.sessionInfoList()
	}
	s.debugf("http api_sessions remote=%s sessions=%d", r.RemoteAddr, len(infos))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(infos)
}

func (s *Server) handleVersionHTTP(w http.ResponseWriter, r *http.Request) {
	// /api/version is intentionally public: web clients (login.html,
	// signup.html, etc.) need to display the running relay version before
	// the user has any credentials. The top-level per-IP rate limit in
	// ServeHTTP already bounds anonymous traffic.
	version := s.cfg.Version
	if version == "" {
		version = "dev"
	}
	s.debugf("http api_version remote=%s version=%s", r.RemoteAddr, version)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(versionResponse{Version: version})
}

// allowedStaticPath reports whether p is a known production-asset path the
// static handler is willing to serve. The whitelist is the exhaustive set of
// files vite-plugin-pwa + the multi-entry MPA build emits at the embed root,
// plus the single-segment wildcards content-hashed assets use.
//
// Anything else (source files like package.json when --web mistakenly points
// at web/, dotfiles like .gitkeep / .npmrc, directory listings, nested
// /assets/ paths, traversals normalised down to root, …) is rejected with
// 404. The whitelist must be updated when build output gains a new
// top-level artifact type.
func allowedStaticPath(p string) bool {
	switch p {
	case "/", "/index.html",
		"/login.html", "/signup.html", "/settings.html",
		"/admin/", "/admin/index.html",
		"/sw.js", "/manifest.webmanifest",
		"/icon.svg", "/icon.png":
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/assets/"); ok {
		// Exactly one path segment past /assets/.
		return rest != "" && !strings.ContainsAny(rest, "/")
	}
	if strings.HasPrefix(p, "/workbox-") && strings.HasSuffix(p, ".js") {
		// /workbox-<hash>.js at root, no nested directories.
		return !strings.ContainsAny(strings.TrimPrefix(p, "/"), "/")
	}
	return false
}

// newStaticHandler wraps http.FileServer for the embedded web bundle.
//
// Before the session-token migration the relay relied on an HttpOnly cookie
// to know whether a browser was authenticated, and this handler 302'd
// unauthenticated `/` navigations to /login.html. Cookies are gone now and
// browsers cannot attach the localStorage-resident Bearer token to plain
// navigation requests, so the resolver always reports PrincipalNone for
// page loads — the old server-side gate would redirect every successful
// login back to /login.html (the production bug fixed by this change).
//
// Authorization is now a purely client-side concern: every page boots its
// SPA, which reads the session token from localStorage. If the token is
// missing or rejected, apiFetch's 401 interceptor sends the user to
// /login.html. Admin gating works the same way — the admin SPA queries
// /api/me, checks is_admin, and redirects clients without privileges.
//
// resolver is retained as a parameter for compatibility but is unused; the
// resolver argument may be nil and is ignored at runtime.
func newStaticHandler(resolver *IdentityResolver, webFS fs.FS) http.Handler {
	_ = resolver
	fileSrv := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedStaticPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fileSrv.ServeHTTP(w, r)
	})
}

// readFrame reads one WS binary message and decodes it as a Frame.
func readFrame(ctx context.Context, c *websocket.Conn) (proto.Frame, error) {
	mt, data, err := c.Read(ctx)
	if err != nil {
		return proto.Frame{}, err
	}
	if mt != websocket.MessageBinary {
		return proto.Frame{}, errNonBinary
	}
	return proto.Unmarshal(data)
}
