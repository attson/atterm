package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func mustReadFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func dialClientWithToken(ctx context.Context, url, token string) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"atterm-token." + token},
	})
}

func newBearerRequest(method, target, token string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestVersionEndpointReturnsConfiguredVersion(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var got struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("version = %q; want v1.2.3", got.Version)
	}
}

func TestVersionEndpointIsPublic(t *testing.T) {
	// Anonymous web clients (login.html / signup.html) need to read the
	// version before the user has credentials. Anonymous GET must succeed.
	srv := NewServer(Config{Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy header is empty")
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q; want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q; want nosniff", got)
	}
}

func TestSecurityHeadersAllowXtermRuntimeStylesWithoutUnsafeScript(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy = %q; want style-src to allow xterm runtime inline styles", csp)
	}
	// 'unsafe-inline' is forbidden — would defeat the entire CSP. 'unsafe-eval'
	// is permitted because Naive UI's bundled lodash uses Function("return this")
	// to fetch globalThis; see server.go's CSP comment for the trade-off.
	if strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy = %q; script-src must not allow 'unsafe-inline'", csp)
	}
}

// TestAdminConfigUnauthorizedWithoutResolver — when no resolver/store is
// configured, /admin/api/config is registered but the requireSession wrapper
// returns 401 for anonymous callers.
func TestAdminConfigUnauthorizedWithoutResolver(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d; want 401", rec.Code)
	}
}

// TestAdminConfigRequiresAdminPrincipal — an anonymous request (no
// Authorization header) to /admin/api/config is rejected with 401 even when
// a store is wired.
func TestAdminConfigRequiresAdminPrincipal(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s; want 401", rec.Code, rec.Body.String())
	}
}

// TestAdminConfigPersistsRuntimeLimits — an admin (Bearer session token) PUTs
// new runtime limits; the response and the on-disk admin config file both
// reflect the new values.
func TestAdminConfigPersistsRuntimeLimits(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	u, err := store.CreateOpaqueUser(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua", "1.2.3.0/24", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cfgStore := NewAdminConfigStore(store, AdminConfig{})
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver:         resolver,
		Store:            store,
		AdminConfigStore: cfgStore,
	})

	body := strings.NewReader(`{"rate_limit_per_minute":33,"max_connections_per_key":4}`)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/config", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s; want 200", rec.Code, rec.Body.String())
	}
	// Verify the config was persisted to the DB.
	snap := cfgStore.Snapshot()
	if snap.RateLimitPerMinute != 33 || snap.MaxConnectionsPerKey != 4 {
		t.Fatalf("persisted limits=%d/%d; want 33/4", snap.RateLimitPerMinute, snap.MaxConnectionsPerKey)
	}
}

// TestAdminConfigRoundtripsAllowedOrigins verifies the admin UI can read the
// current allow-list and PUT a new one. The hot-apply path (SetAllowedOrigins
// + OriginPatterns) is exercised implicitly: after the PUT, the server's
// accept options carry the normalized patterns (host-only) so that a mobile
// WS from Origin capacitor://localhost survives nhooyr's origin check.
func TestAdminConfigRoundtripsAllowedOrigins(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	u, err := store.CreateOpaqueUser(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua", "1.2.3.0/24", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cfgStore := NewAdminConfigStore(store, AdminConfig{
		AllowedOrigins: []string{"https://relay.example.com"},
	})
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver:         resolver,
		Store:            store,
		AdminConfigStore: cfgStore,
		AllowedOrigins:   []string{"relay.example.com"},
	})

	// GET returns the stored list verbatim (not the normalized host patterns).
	getReq := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	getReq.Header.Set("Authorization", "Bearer "+tok)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s; want 200", getRec.Code, getRec.Body.String())
	}
	var got struct {
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(got.AllowedOrigins) != 1 || got.AllowedOrigins[0] != "https://relay.example.com" {
		t.Fatalf("GET allowed_origins=%#v; want [https://relay.example.com]", got.AllowedOrigins)
	}

	// PUT with a mobile-inclusive list must persist AND hot-apply so that
	// WS from capacitor://localhost (host=localhost) survives the origin check.
	putBody := strings.NewReader(`{
		"rate_limit_per_minute": 0,
		"max_connections_per_key": 0,
		"allowed_origins": ["https://relay.example.com", "capacitor://localhost"]
	}`)
	putReq := httptest.NewRequest(http.MethodPut, "/admin/api/config", putBody)
	putReq.Header.Set("Authorization", "Bearer "+tok)
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	srv.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s; want 200", putRec.Code, putRec.Body.String())
	}

	snap := cfgStore.Snapshot()
	if len(snap.AllowedOrigins) != 2 || snap.AllowedOrigins[1] != "capacitor://localhost" {
		t.Fatalf("persisted allowed_origins=%#v; want [https://relay.example.com capacitor://localhost]", snap.AllowedOrigins)
	}

	// Hot-apply: acceptOptions must now use host-only patterns so nhooyr's
	// authenticateOrigin can match Origin capacitor://localhost (host=localhost).
	patterns := srv.currentAllowedOrigins()
	sawLocalhost := false
	for _, p := range patterns {
		if p == "localhost" {
			sawLocalhost = true
			break
		}
	}
	if !sawLocalhost {
		t.Fatalf("after PUT, allowedOrigins patterns=%#v; want to include host-only 'localhost'", patterns)
	}
}

func TestRateLimitRejectsExcessRequests(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3", RateLimitPerMinute: 1})

	req1 := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req1.RemoteAddr = "203.0.113.10:10000"
	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d; want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req2.RemoteAddr = "203.0.113.10:10001"
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d; want 429", rec2.Code)
	}
}

func TestRequestLimitKeyDoesNotExposeBearerToken(t *testing.T) {
	req := newBearerRequest(http.MethodGet, "/api/version", "secret-token")
	req.RemoteAddr = "203.0.113.10:10000"

	key := requestLimitKey(req)

	if strings.Contains(key, "secret-token") {
		t.Fatalf("limit key exposes token: %q", key)
	}
	if !strings.HasPrefix(key, "203.0.113.10\x00") {
		t.Fatalf("limit key = %q; want remote host prefix", key)
	}
}

func TestClientSessionsAcceptsSubprotocolToken(t *testing.T) {
	srv, tok := serverWithSession(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client-sessions", &websocket.DialOptions{
		Subprotocols: []string{"atterm-token." + tok},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if conn.Subprotocol() != "atterm-token."+tok {
		t.Fatalf("subprotocol = %q; want atterm-token.<tok>", conn.Subprotocol())
	}
}

func TestClientWebSocketRejectsQueryToken(t *testing.T) {
	srv, tok := serverWithSession(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Pass token only as query string — must NOT authorize.
	conn, resp, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client?token="+tok, nil)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("websocket connected with query token; want rejection")
	}
	if resp == nil {
		t.Fatalf("response is nil; err=%v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401; err=%v", resp.StatusCode, err)
	}
}

func TestClientSessionsWebSocketRejectsQueryToken(t *testing.T) {
	srv, tok := serverWithSession(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client-sessions?token="+tok, nil)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("session list websocket connected with query token; want rejection")
	}
	if resp == nil {
		t.Fatalf("response is nil; err=%v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401; err=%v", resp.StatusCode, err)
	}
}

func TestConnectionLimitRejectsExcessWebSockets(t *testing.T) {
	srv, tok := serverWithSession(t)
	srv.conns = newConnectionLimiter(1)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := dialClientWithToken(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", tok)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	second, resp, err := dialClientWithToken(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", tok)
	if err == nil {
		second.Close(websocket.StatusNormalClosure, "")
		t.Fatal("second websocket connected; want connection limit rejection")
	}
	if resp == nil {
		t.Fatalf("second response is nil; err=%v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d; want 429; err=%v", resp.StatusCode, err)
	}
}

func TestSessionPermissionViewDropsInputForWriteToken(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	id := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionView})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := dialClientWithToken(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", tok)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("view permission delivered inbound frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionPermissionControlAllowsInputButDropsPasteImage(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	id := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionControl})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := dialClientWithToken(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", tok)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn {
			t.Fatalf("inbound type=%v; want IN", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allowed IN")
	}
	pastePayload, _ := json.Marshal(proto.PasteImagePayload{ContentType: "image/png", Data: []byte("png")})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypePasteImage, SessionID: id, Payload: pastePayload,
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("control permission delivered paste frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}
