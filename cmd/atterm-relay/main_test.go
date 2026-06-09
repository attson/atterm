package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
	"nhooyr.io/websocket"
)

// newTestServer builds a minimal relay Server backed by an in-memory userstore
// for use in integration tests.
func newTestServer(t *testing.T, origins []string) (*relay.Server, userstore.Store) {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	resolver := relay.NewIdentityResolver(store)
	cfg := relay.Config{
		AllowedOrigins: origins,
		Resolver:       resolver,
		Store:          store,
	}
	return relay.NewServer(cfg), store
}

// TestStartup_LoopbackDevAcceptsAnyToken verifies that the loopback check
// (isPublicListenAddr) returns false for 127.0.0.1 and ::1, so the
// public-listen safety guard is skipped for local-only relays.
func TestStartup_LoopbackDevAcceptsAnyToken(t *testing.T) {
	if isPublicListenAddr("127.0.0.1:8080") {
		t.Error("127.0.0.1:8080 reported as public; want loopback")
	}
	if isPublicListenAddr("[::1]:8080") {
		t.Error("[::1]:8080 reported as public; want loopback")
	}
	if isPublicListenAddr("localhost:8080") {
		t.Error("localhost:8080 reported as public; want loopback")
	}
	if !isPublicListenAddr(":8080") {
		t.Error(":8080 reported as loopback; want public")
	}
}

// TestRelaySecurityNormalizesOriginsAndAllowsDesktopWebviews verifies that the
// origins helper appends Wails desktop webview hosts and normalizes https:// URLs.
func TestRelaySecurityNormalizesOriginsAndAllowsDesktopWebviews(t *testing.T) {
	origins := allowedOriginHosts("https://relay.example.com,*.trusted.example.com")
	want := []string{"relay.example.com", "*.trusted.example.com", "wails", "wails.localhost", "wails.localhost:*"}
	for _, origin := range want {
		if !containsString(origins, origin) {
			t.Fatalf("AllowedOrigins = %#v; want %q", origins, origin)
		}
	}
}

// TestRelaySecurityAcceptsDesktopWebviewSessionListWS verifies that the Wails
// desktop webview origin is allowed and the /client-sessions WS handshake works.
// It creates a user + API token so the IdentityResolver resolves PrincipalUser.
func TestRelaySecurityAcceptsDesktopWebviewSessionListWS(t *testing.T) {
	srv, store := newTestServer(t, allowedOriginHosts("https://relay.example.com"))
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create a user and mint a session token.
	user, err := store.CreateUser(ctx, "test@example.com", "password123!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sessTok, _, err := store.CreateSession(ctx, user.ID, "wails-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client-sessions", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{"wails://wails"}},
		Subprotocols: []string{"atterm-token." + sessTok},
	})
	if err != nil {
		t.Fatalf("desktop webview /client-sessions dial err: %v", err)
	}
	defer conn.CloseNow()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read initial session list: %v", err)
	}
	frame, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal initial session list: %v", err)
	}
	if frame.Type != proto.TypeListResp {
		t.Fatalf("frame.Type = 0x%02x; want LIST_RESP", frame.Type)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
