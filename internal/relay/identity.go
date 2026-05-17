package relay

import (
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/userstore"
)

// PrincipalKind classifies who is making a request.
type PrincipalKind uint8

const (
	PrincipalNone  PrincipalKind = iota // unauthenticated / unknown
	PrincipalUser                       // authenticated user (cookie or api token)
	PrincipalAdmin                      // authenticated user with user.is_admin = true
)

// Principal is the resolved identity for a single HTTP request.
// authScope is declared in auth.go and reused here.
type Principal struct {
	Kind       PrincipalKind
	UserID     string    // non-empty when Kind == PrincipalUser
	TokenID    string    // non-empty when source was an API token (not a cookie)
	Scope      authScope // authWrite for all valid principals in this implementation
	CSRFSecret []byte    // set when Kind == PrincipalUser and source was a cookie
}

// IdentityResolver is constructed once at relay startup and reused across
// every handler.
type IdentityResolver struct {
	store userstore.Store
}

// NewIdentityResolver creates an IdentityResolver backed by store.
func NewIdentityResolver(store userstore.Store) *IdentityResolver {
	return &IdentityResolver{store: store}
}

// Resolve returns the Principal for req. It never returns an error; all failure
// modes collapse to Principal{Kind: PrincipalNone}.
//
// Precedence (spec §4):
//  1. Session cookie "atterm_session" — highest priority
//  2. Authorization: Bearer <token> or Sec-WebSocket-Protocol sub-protocol token
//
// URL query parameters are intentionally NOT read (red line: secrets must not
// appear in server logs via query strings).
func (r *IdentityResolver) Resolve(req *http.Request) Principal {
	// 1. Cookie — highest precedence.
	if c, err := req.Cookie("atterm_session"); err == nil && c.Value != "" {
		userID, csrfSecret, err := r.store.LookupWebSession(req.Context(), c.Value)
		if err == nil {
			kind := PrincipalUser
			if u, gerr := r.store.GetUser(req.Context(), userID); gerr == nil && u.IsAdmin {
				kind = PrincipalAdmin
			}
			return Principal{
				Kind:       kind,
				UserID:     userID,
				Scope:      authWrite,
				CSRFSecret: csrfSecret,
			}
		}
		// Invalid/expired cookie — fall through to PrincipalNone (do NOT check
		// bearer token when a cookie was present, to avoid downgrade confusion).
		return Principal{Kind: PrincipalNone}
	}

	// 2. Bearer token or WebSocket subprotocol token.
	if tok := tokenFromIdentityRequest(req); tok != "" {
		tokenID, userID, err := r.store.LookupAPIToken(req.Context(), tok)
		if err == nil {
			kind := PrincipalUser
			if u, gerr := r.store.GetUser(req.Context(), userID); gerr == nil && u.IsAdmin {
				kind = PrincipalAdmin
			}
			return Principal{
				Kind:    kind,
				UserID:  userID,
				TokenID: tokenID,
				Scope:   authWrite,
			}
		}
	}

	return Principal{Kind: PrincipalNone}
}

// tokenFromIdentityRequest extracts a bearer token from Authorization or the
// Sec-WebSocket-Protocol header. It does NOT read URL query parameters.
// It delegates subprotocol parsing to the existing tokenFromSubprotocol helper
// in auth.go so the two implementations stay in sync.
func tokenFromIdentityRequest(req *http.Request) string {
	if h := req.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return tokenFromSubprotocol(req)
}
