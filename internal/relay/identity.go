package relay

import (
	"net/http"

	"github.com/attson/atterm/internal/userstore"
)

// PrincipalKind classifies who is making a request.
type PrincipalKind uint8

const (
	PrincipalNone  PrincipalKind = iota // unauthenticated / unknown
	PrincipalUser                       // authenticated user
	PrincipalAdmin                      // authenticated user with user.is_admin = true
)

// Principal is the resolved identity for a single HTTP request. After the
// session-token migration the only writer is IdentityResolver.Resolve (used
// by the static handler's login redirect) and the requireUser shim in
// auth_http.go that derives one from a UserFromContext call.
type Principal struct {
	Kind   PrincipalKind
	UserID string    // non-empty when Kind is PrincipalUser or PrincipalAdmin
	Scope  authScope // authWrite for every authenticated principal
}

// IdentityResolver resolves the static handler's redirect Principal from a
// bearer token. It is constructed once at relay startup and reused across
// every static GET. Production HTTP / WebSocket routes do NOT use this type
// — they go through requireSession, which writes the user into the request
// context directly.
type IdentityResolver struct {
	store userstore.Store
}

// NewIdentityResolver creates an IdentityResolver backed by store.
func NewIdentityResolver(store userstore.Store) *IdentityResolver {
	return &IdentityResolver{store: store}
}

// Resolve returns the Principal for req. It never returns an error; all
// failure modes collapse to Principal{Kind: PrincipalNone}.
//
// Token sources, in order:
//  1. Authorization: Bearer <token>
//  2. Sec-WebSocket-Protocol: atterm-token.<token> (or -b64.<base64>)
//
// URL query parameters and HTTP cookies are intentionally NOT read.
func (r *IdentityResolver) Resolve(req *http.Request) Principal {
	tok := tokenFromRequest(req)
	if tok == "" {
		return Principal{Kind: PrincipalNone}
	}
	_, user, err := r.store.LookupSession(req.Context(), tok)
	if err != nil || user == nil {
		return Principal{Kind: PrincipalNone}
	}
	kind := PrincipalUser
	if user.IsAdmin {
		kind = PrincipalAdmin
	}
	return Principal{Kind: kind, UserID: user.ID, Scope: authWrite}
}
