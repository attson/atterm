// Package relay is the central WebSocket hub. Agents connect on /agent and
// publish PTY output; clients connect on /client to attach, receive scrollback
// + live output, and send IN/RESIZE frames. State is in-memory only.
package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"nhooyr.io/websocket"
)

// Config configures a Server.
type Config struct {
	// Token is the shared bearer token. Empty means no auth (dev only).
	Token string
	// WebDir is the filesystem path to the static web client. Empty disables /.
	WebDir string
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
}

// Server bundles the registry and HTTP handlers.
type Server struct {
	cfg      Config
	registry *session.Registry
	mux      *http.ServeMux
}

// NewServer builds a Server with its routes installed.
func NewServer(cfg Config) *Server {
	if cfg.DebugLog == nil {
		cfg.DebugLog = os.Stderr
	}
	s := &Server{
		cfg:      cfg,
		registry: session.NewRegistry(),
		mux:      http.NewServeMux(),
	}
	s.mux.HandleFunc("/agent", s.handleAgentHTTP)
	s.mux.HandleFunc("/uplink", s.handleUplinkHTTP)
	s.mux.HandleFunc("/client", s.handleClientHTTP)
	s.mux.HandleFunc("/api/sessions", s.handleSessionsHTTP)
	if cfg.WebDir != "" {
		s.mux.Handle("/", http.FileServer(http.Dir(cfg.WebDir)))
	}
	return s
}

// ServeHTTP makes Server an http.Handler. CORS headers are added unconditionally
// — they only matter for cross-origin REST callers (e.g. webviews fetching the
// /api/sessions endpoint), and are harmless to same-origin browser clients.
// WebSocket upgrades are gated separately by Origin validation in nhooyr.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleAgentHTTP(w http.ResponseWriter, r *http.Request) {
	if !authorize(r, s.cfg.Token) {
		s.debugf("http reject path=/agent remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
	if !authorize(r, s.cfg.Token) {
		s.debugf("http reject path=/uplink remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
	if !authorize(r, s.cfg.Token) {
		s.debugf("http reject path=/client remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// browsers need to negotiate the same subprotocol if used for auth
	opts := s.acceptOptions()
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		for _, sp := range strings.Split(p, ",") {
			sp = strings.TrimSpace(sp)
			if strings.HasPrefix(sp, "atterm-token.") {
				opts.Subprotocols = []string{sp}
				break
			}
		}
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.debugf("ws reject path=/client remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
		return
	}
	s.debugf("ws accept path=/client remote=%s origin=%q subprotocol=%q", r.RemoteAddr, r.Header.Get("Origin"), c.Subprotocol())
	defer c.Close(websocket.StatusInternalError, "")
	s.handleClient(r.Context(), c)
}

func (s *Server) handleSessionsHTTP(w http.ResponseWriter, r *http.Request) {
	if !authorize(r, s.cfg.Token) {
		s.debugf("http reject path=/api/sessions remote=%s reason=unauthorized", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessions := s.registry.List()
	s.debugf("http api_sessions remote=%s sessions=%d", r.RemoteAddr, len(sessions))
	infos := make([]proto.SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		infos = append(infos, ss.Info())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(infos)
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
