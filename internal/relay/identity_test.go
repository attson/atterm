package relay

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// req builds an *http.Request with the given key/value header pairs.
func req(headers ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

// TestResolveIdentity exercises the post-migration IdentityResolver, which
// recognises a session token via Authorization: Bearer or
// Sec-WebSocket-Protocol: atterm-token[-b64].<token>. Cookies are ignored.
func TestResolveIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	u, err := store.CreateUser(ctx, "u@example.com", "passphrase-1234")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	tokB64 := base64.RawURLEncoding.EncodeToString([]byte(tok))

	tests := []struct {
		name       string
		req        *http.Request
		wantKind   PrincipalKind
		wantUserID string
		wantScope  authScope
	}{
		{
			name:     "empty",
			req:      req(),
			wantKind: PrincipalNone,
		},
		{
			name:       "valid Authorization Bearer session token",
			req:        req("Authorization", "Bearer "+tok),
			wantKind:   PrincipalUser,
			wantUserID: u.ID,
			wantScope:  authWrite,
		},
		{
			name:     "invalid Bearer token",
			req:      req("Authorization", "Bearer ses_does_not_exist"),
			wantKind: PrincipalNone,
		},
		{
			name:       "Sec-WebSocket-Protocol atterm-token.",
			req:        req("Sec-WebSocket-Protocol", "atterm-token."+tok),
			wantKind:   PrincipalUser,
			wantUserID: u.ID,
			wantScope:  authWrite,
		},
		{
			name:       "Sec-WebSocket-Protocol atterm-token-b64.",
			req:        req("Sec-WebSocket-Protocol", "atterm-token-b64."+tokB64),
			wantKind:   PrincipalUser,
			wantUserID: u.ID,
			wantScope:  authWrite,
		},
		{
			name:     "cookie alone is ignored (cookies no longer authorize)",
			req:      req("Cookie", "atterm_session="+tok),
			wantKind: PrincipalNone,
		},
	}

	resolver := NewIdentityResolver(store)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolver.Resolve(tc.req)
			if got.Kind != tc.wantKind {
				t.Errorf("Kind: got %v, want %v", got.Kind, tc.wantKind)
			}
			if got.UserID != tc.wantUserID {
				t.Errorf("UserID: got %q, want %q", got.UserID, tc.wantUserID)
			}
			if got.Scope != tc.wantScope {
				t.Errorf("Scope: got %v, want %v", got.Scope, tc.wantScope)
			}
		})
	}
}

func TestResolve_BearerSession_AdminUser_BecomesPrincipalAdmin(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	tok, _, _ := store.CreateSession(ctx, u.ID, "ua/test", "203.0.113.0/24", 24*time.Hour)

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+tok)

	resolver := NewIdentityResolver(store)
	p := resolver.Resolve(r)
	if p.Kind != PrincipalAdmin {
		t.Fatalf("Kind = %v; want PrincipalAdmin", p.Kind)
	}
	if p.UserID != u.ID {
		t.Errorf("UserID = %q; want %q", p.UserID, u.ID)
	}
}

func TestResolve_BearerSession_NonAdminUser_StaysPrincipalUser(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser(ctx, "b@example.com", "passphrase-1234")
	tok, _, _ := store.CreateSession(ctx, u.ID, "ua/test", "203.0.113.0/24", 24*time.Hour)

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+tok)

	resolver := NewIdentityResolver(store)
	p := resolver.Resolve(r)
	if p.Kind != PrincipalUser {
		t.Fatalf("Kind = %v; want PrincipalUser", p.Kind)
	}
}
