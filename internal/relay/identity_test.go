package relay

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// fakeStore implements userstore.Store by embedding the interface (nil field),
// so unimplemented methods panic on nil dereference — acceptable in unit tests.
// Only LookupWebSession, LookupAPIToken, and GetUser are overridden.
type fakeStore struct {
	userstore.Store // nil — unimplemented methods panic

	onLookupWebSession func(ctx context.Context, plaintext string) (string, []byte, error)
	onLookupAPIToken   func(ctx context.Context, plaintext string) (string, string, error)
	apiTokenCalled     int

	// onGetUser, if nil, returns a non-admin user — sufficient for tests that
	// only care about PrincipalUser vs PrincipalNone resolution.
	onGetUser func(ctx context.Context, id string) (*userstore.User, error)
}

func (f *fakeStore) LookupWebSession(ctx context.Context, plaintext string) (string, []byte, error) {
	return f.onLookupWebSession(ctx, plaintext)
}

func (f *fakeStore) LookupAPIToken(ctx context.Context, plaintext string) (string, string, error) {
	f.apiTokenCalled++
	return f.onLookupAPIToken(ctx, plaintext)
}

func (f *fakeStore) GetUser(ctx context.Context, id string) (*userstore.User, error) {
	if f.onGetUser != nil {
		return f.onGetUser(ctx, id)
	}
	return &userstore.User{ID: id, IsAdmin: false}, nil
}

// req builds an *http.Request with the given key/value header pairs.
func req(headers ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

func TestResolveIdentity(t *testing.T) {
	const (
		sessionCookie = "sess_plaintext_abc"
		apiToken      = "atk_plaintexttoken123"
		userID        = "u1"
		tokenID       = "tok1"
	)
	csrfSecret := []byte("csrf-secret-bytes")

	happyWebSession := func(ctx context.Context, plaintext string) (string, []byte, error) {
		if plaintext == sessionCookie {
			return userID, csrfSecret, nil
		}
		return "", nil, userstore.ErrWebSessionInvalid
	}
	expiredWebSession := func(_ context.Context, _ string) (string, []byte, error) {
		return "", nil, userstore.ErrWebSessionInvalid
	}
	happyAPIToken := func(_ context.Context, plaintext string) (string, string, error) {
		if plaintext == apiToken {
			return tokenID, userID, nil
		}
		return "", "", userstore.ErrTokenInvalid
	}
	revokedAPIToken := func(_ context.Context, _ string) (string, string, error) {
		return "", "", userstore.ErrTokenInvalid
	}
	neverCalledAPIToken := func(_ context.Context, _ string) (string, string, error) {
		t.Error("LookupAPIToken must not be called when cookie wins")
		return "", "", userstore.ErrTokenInvalid
	}
	noWebSession := func(_ context.Context, _ string) (string, []byte, error) {
		return "", nil, userstore.ErrWebSessionInvalid
	}
	noAPIToken := func(_ context.Context, _ string) (string, string, error) {
		return "", "", userstore.ErrTokenInvalid
	}

	apiTokenB64Req := func() *http.Request {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(apiToken))
		return req("Sec-WebSocket-Protocol", "atterm-token-b64."+encoded)
	}()

	tests := []struct {
		name          string
		store         *fakeStore
		req           *http.Request
		wantKind      PrincipalKind
		wantUserID    string
		wantTokenID   string
		wantScope     authScope
		wantAPICallN  int // expected apiTokenCalled after Resolve
	}{
		{
			name: "empty",
			store: &fakeStore{
				onLookupWebSession: noWebSession,
				onLookupAPIToken:   noAPIToken,
			},
			req:      req(),
			wantKind: PrincipalNone,
		},
		{
			name: "valid cookie",
			store: &fakeStore{
				onLookupWebSession: happyWebSession,
				onLookupAPIToken:   neverCalledAPIToken,
			},
			req:          req("Cookie", "atterm_session="+sessionCookie),
			wantKind:     PrincipalUser,
			wantUserID:   userID,
			wantScope:    authWrite,
			wantAPICallN: 0,
		},
		{
			name: "expired cookie",
			store: &fakeStore{
				onLookupWebSession: expiredWebSession,
				onLookupAPIToken:   noAPIToken,
			},
			req:      req("Cookie", "atterm_session="+sessionCookie),
			wantKind: PrincipalNone,
		},
		{
			name: "valid Authorization Bearer api token",
			store: &fakeStore{
				onLookupWebSession: noWebSession,
				onLookupAPIToken:   happyAPIToken,
			},
			req:          req("Authorization", "Bearer "+apiToken),
			wantKind:     PrincipalUser,
			wantUserID:   userID,
			wantTokenID:  tokenID,
			wantScope:    authWrite,
			wantAPICallN: 1,
		},
		{
			name: "revoked api token",
			store: &fakeStore{
				onLookupWebSession: noWebSession,
				onLookupAPIToken:   revokedAPIToken,
			},
			req:          req("Authorization", "Bearer "+apiToken),
			wantKind:     PrincipalNone,
			wantAPICallN: 1,
		},
		{
			name: "Sec-WebSocket-Protocol atterm-token.",
			store: &fakeStore{
				onLookupWebSession: noWebSession,
				onLookupAPIToken:   happyAPIToken,
			},
			req:          req("Sec-WebSocket-Protocol", "atterm-token."+apiToken),
			wantKind:     PrincipalUser,
			wantUserID:   userID,
			wantTokenID:  tokenID,
			wantScope:    authWrite,
			wantAPICallN: 1,
		},
		{
			name: "Sec-WebSocket-Protocol atterm-token-b64.",
			store: &fakeStore{
				onLookupWebSession: noWebSession,
				onLookupAPIToken:   happyAPIToken,
			},
			req:          apiTokenB64Req,
			wantKind:     PrincipalUser,
			wantUserID:   userID,
			wantTokenID:  tokenID,
			wantScope:    authWrite,
			wantAPICallN: 1,
		},
		{
			name: "cookie wins over Authorization Bearer",
			store: &fakeStore{
				onLookupWebSession: happyWebSession,
				onLookupAPIToken:   neverCalledAPIToken,
			},
			req: req(
				"Cookie", "atterm_session="+sessionCookie,
				"Authorization", "Bearer "+apiToken,
			),
			wantKind:     PrincipalUser,
			wantUserID:   userID,
			wantScope:    authWrite,
			wantAPICallN: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := NewIdentityResolver(tc.store)
			got := resolver.Resolve(tc.req)

			if got.Kind != tc.wantKind {
				t.Errorf("Kind: got %v, want %v", got.Kind, tc.wantKind)
			}
			if got.UserID != tc.wantUserID {
				t.Errorf("UserID: got %q, want %q", got.UserID, tc.wantUserID)
			}
			if got.TokenID != tc.wantTokenID {
				t.Errorf("TokenID: got %q, want %q", got.TokenID, tc.wantTokenID)
			}
			if got.Scope != tc.wantScope {
				t.Errorf("Scope: got %v, want %v", got.Scope, tc.wantScope)
			}
			if tc.store.apiTokenCalled != tc.wantAPICallN {
				t.Errorf("LookupAPIToken calls: got %d, want %d",
					tc.store.apiTokenCalled, tc.wantAPICallN)
			}
		})
	}
}

func TestResolve_CookieSession_AdminUser_BecomesPrincipalAdmin(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})

	resolver := NewIdentityResolver(store)
	p := resolver.Resolve(r)
	if p.Kind != PrincipalAdmin {
		t.Fatalf("Kind = %v; want PrincipalAdmin", p.Kind)
	}
	if p.UserID != u.ID {
		t.Errorf("UserID = %q; want %q", p.UserID, u.ID)
	}
}

func TestResolve_CookieSession_NonAdminUser_StaysPrincipalUser(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser(ctx, "b@example.com", "passphrase-1234")
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})

	resolver := NewIdentityResolver(store)
	p := resolver.Resolve(r)
	if p.Kind != PrincipalUser {
		t.Fatalf("Kind = %v; want PrincipalUser", p.Kind)
	}
}
