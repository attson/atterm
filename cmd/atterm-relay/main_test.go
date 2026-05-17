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
// for use in integration tests. adminToken must be non-empty.
func newTestServer(t *testing.T, adminToken string, origins []string) (*relay.Server, userstore.Store) {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	resolver := relay.NewIdentityResolver(store)
	cfg := relay.Config{
		AdminToken:     adminToken,
		AllowedOrigins: origins,
		Resolver:       resolver,
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
	adminToken := "Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!"
	srv, store := newTestServer(t, adminToken, allowedOriginHosts("https://relay.example.com"))
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create a user and issue an API token.
	if _, err := store.CreateUser(ctx, "test@example.com", "password123!"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	user, err := store.VerifyPassword(ctx, "test@example.com", "password123!")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	apiSecret, _, err := store.CreateAPIToken(ctx, user.ID, "test-token")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	apiToken := apiSecret.Expose()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client-sessions", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{"wails://wails"}},
		Subprotocols: []string{"atterm-token." + apiToken},
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
