// Package relay is the central WebSocket hub. Agents connect on /agent and
// publish PTY output; clients connect on /client to attach, receive scrollback
// + live output, and send IN/RESIZE frames. State is in-memory only.
package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// Config configures a Server.
type Config struct {
	// Token is the shared bearer token. Empty means no auth (dev only).
	Token string
	// ReadOnlyTokens can list and attach to sessions, but cannot send input,
	// resize, paste images, or register agent/uplink connections.
	ReadOnlyTokens []string
	// ReadOnlyTokenHashes are sha256:base64url token hashes from persistent
	// admin config. They behave like ReadOnlyTokens without storing secrets.
	ReadOnlyTokenHashes []string
	// WebDir is the filesystem path to the static web client. Empty disables /.
	WebDir string
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
	// AdminToken enables /admin routes when non-empty. It is never accepted via
	// query parameters.
	AdminToken string
	// AdminConfigStore persists admin API changes when configured.
	AdminConfigStore *AdminConfigStore
	// WebPush, when non-nil, enables the /api/push/* endpoints and the
	// TypeCommandEvent uplink handler. May be nil to disable the feature.
	WebPush *webpush.Service
}

// Server bundles the registry and HTTP handlers.
type Server struct {
	cfg      Config
	registry *session.Registry
	mux      *http.ServeMux
	rate     *fixedWindowLimiter
	conns    *connectionLimiter
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
		cfg:      cfg,
		registry: session.NewRegistry(),
		mux:      http.NewServeMux(),
		rate:     newFixedWindowLimiter(rateLimit, time.Minute),
		conns:    newConnectionLimiter(connLimit),
	}
	s.mux.HandleFunc("/agent", s.handleAgentHTTP)
	s.mux.HandleFunc("/uplink", s.handleUplinkHTTP)
	s.mux.HandleFunc("/client", s.handleClientHTTP)
	s.mux.HandleFunc("/client-sessions", s.handleClientSessionsHTTP)
	s.mux.HandleFunc("/api/sessions", s.handleSessionsHTTP)
	s.mux.HandleFunc("/api/version", s.handleVersionHTTP)
	s.mux.HandleFunc("/api/push/key", s.handlePushKey)
	s.mux.HandleFunc("/api/push/subscribe", s.handlePushSubscribe)
	s.mux.HandleFunc("/api/push/unsubscribe", s.handlePushUnsubscribe)
	s.mux.HandleFunc("/api/push/test", s.handlePushTest)
	if cfg.WebDir != "" {
		s.mux.Handle("/", http.FileServer(http.Dir(cfg.WebDir)))
	}
	if cfg.AdminToken != "" {
		s.mux.HandleFunc("/admin/", s.handleAdminPage)
		s.mux.HandleFunc("/admin/api/config", s.handleAdminConfigHTTP)
		s.mux.HandleFunc("/admin/api/read-only-tokens", s.handleAdminReadOnlyTokensHTTP)
		s.mux.HandleFunc("/admin/api/read-only-tokens/", s.handleAdminReadOnlyTokenHTTP)
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
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
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
	if authorizeWithScope(r, s.cfg.Token, s.cfg.ReadOnlyTokens) != authWrite {
		s.debugf("http reject path=/agent remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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
	s.handleAgent(r.Context(), c)
}

func (s *Server) handleUplinkHTTP(w http.ResponseWriter, r *http.Request) {
	if authorizeWithScope(r, s.cfg.Token, s.cfg.ReadOnlyTokens) != authWrite {
		s.debugf("http reject path=/uplink remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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
	defer c.Close(websocket.StatusInternalError, "")
	s.handleUplink(r.Context(), c)
}

func (s *Server) handleClientHTTP(w http.ResponseWriter, r *http.Request) {
	scope := authorizeClientWebSocketWithConfig(r, s.cfg)
	if scope == authNone {
		s.debugf("http reject path=/client remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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
	s.handleClient(r.Context(), c, scope)
}

func (s *Server) handleClientSessionsHTTP(w http.ResponseWriter, r *http.Request) {
	scope := authorizeClientWebSocketWithConfig(r, s.cfg)
	if scope == authNone {
		s.debugf("http reject path=/client-sessions remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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
	s.handleClientSessions(r.Context(), c)
}

func (s *Server) handleSessionsHTTP(w http.ResponseWriter, r *http.Request) {
	if authorizeClientWithConfig(r, s.cfg) == authNone {
		s.debugf("http reject path=/api/sessions remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	infos := s.sessionInfoList()
	s.debugf("http api_sessions remote=%s sessions=%d", r.RemoteAddr, len(infos))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(infos)
}

func (s *Server) handleVersionHTTP(w http.ResponseWriter, r *http.Request) {
	if authorizeClientWithConfig(r, s.cfg) == authNone {
		s.debugf("http reject path=/api/version remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	version := s.cfg.Version
	if version == "" {
		version = "dev"
	}
	s.debugf("http api_version remote=%s version=%s", r.RemoteAddr, version)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(versionResponse{Version: version})
}

// WebPushSessionResolver returns the list of token-hashes authorized to
// view a session at the given id. Empty when the session is unknown.
// Used as the SessionResolver injected into webpush.Service at startup.
func (s *Server) WebPushSessionResolver(sessionID uuid.UUID) []string {
	sess, ok := s.registry.Get(sessionID)
	if !ok {
		return nil
	}
	info := sess.Info()
	perm := info.RemotePermission
	if perm == "" {
		perm = proto.RemotePermissionFull
	}
	out := make([]string, 0, 1+len(s.cfg.ReadOnlyTokens))
	if s.cfg.Token != "" {
		out = append(out, tokenHash(s.cfg.Token))
	}
	// All read-only tokens can view at minimum, regardless of perm.
	for _, t := range s.cfg.ReadOnlyTokens {
		out = append(out, tokenHash(t))
	}
	_ = perm // perm is read for future per-permission filtering; v1 includes all tokens that can view.
	return out
}

// tokenHash is the canonical sha256+base64url form used as a key in
// webpush.Service subscription registry.
func tokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
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
