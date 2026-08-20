package relay

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/opaquesuite"
	"github.com/attson/atterm/internal/userstore"
)

// OpaqueAuthHandler owns the OPAQUE-based registration and login HTTP
// endpoints. It is wired by server.go after the OpaqueServer singleton
// has loaded its persisted seed.
//
// loginSessions holds the per-flow OPAQUE state between LoginInit (KE2 sent
// to client) and LoginFinish (KE3 received from client). The map is keyed
// by a freshly minted random session_id — distinct from the user-facing
// session_token issued post-login — so two concurrent in-flight login
// attempts for the same email do not collide. Entries TTL out after 30s
// via a per-entry time.AfterFunc janitor; the short window is fine because
// the client is expected to call finalize immediately. The map is held
// in-memory only; a relay restart cancels every pending flow, which is
// safe (the client will see a 401 on finalize and re-issue init).
type OpaqueAuthHandler struct {
	store *userstore.DBStore
	srv   *OpaqueServer
	// bootstrapEmail is ATTERM_BOOTSTRAP_ADMIN_EMAIL. The first-run setup
	// flow auto-promotes a registration to admin when its email matches this
	// (case-insensitive) AND no admin exists yet — no claim token needed.
	// Empty disables the email-gated path (claim tokens still work).
	bootstrapEmail string
	// realmID is the realm this relay belongs to. It is echoed back in
	// every auth-finalize response so clients can anchor their account_key
	// to a (relay, realm) pair rather than just the origin URL.
	realmID string
	// instancePublicURL is this relay node's public URL (empty for
	// single-instance / dev). It is used in resolveHomeInstanceURL to
	// treat the serving node as live even before its heartbeat row exists.
	instancePublicURL string
	loginSessions     sync.Map // session_id -> *loginPending
	stepUpSessions    sync.Map // session_id -> *stepUpPending (M1i)
}

// loginPending is the in-flight OPAQUE login state for a single (email,
// session_id) pair. server holds the per-request *opaque.Server primed
// with SetKeyMaterial in handleLoginInit — re-using the same instance in
// handleLoginFinish is what lets the library's internal AKE bookkeeping
// (session key derivation, MAC verification) line up across the two HTTP
// round-trips. expiresAt is checked defensively at finalize time in case
// the AfterFunc janitor has not yet fired.
type loginPending struct {
	email     string
	userID    string
	server    *opaque.Server
	expiresAt time.Time
}

// loginSessionTTL bounds how long the relay will hold OPAQUE init state
// before discarding it. The client is expected to call finalize
// immediately after receiving KE2; 30s comfortably covers slow links and
// is short enough that abandoned flows do not accumulate.
const loginSessionTTL = 30 * time.Second

// NewOpaqueAuthHandler constructs the handler. Both store and srv must be
// non-nil; the OpaqueServer is expected to have been initialized via
// LoadOrInitOpaqueServer before this constructor is called.
func NewOpaqueAuthHandler(store *userstore.DBStore, srv *OpaqueServer, bootstrapEmail, realmID, instancePublicURL string) *OpaqueAuthHandler {
	return &OpaqueAuthHandler{store: store, srv: srv, bootstrapEmail: strings.TrimSpace(bootstrapEmail), realmID: realmID, instancePublicURL: instancePublicURL}
}

// ----- Wire types -----
//
// The struct definitions live in internal/opaquesuite/wire.go so the desktop
// SDK (internal/e2eeclient) and the browser WASM client see the same shapes;
// a divergence between the two sides silently deserializes to zero values and
// produces "invalid credentials" bugs the compiler can't catch. Local
// unexported aliases keep the rest of this file (and its tests) unchanged.

type (
	registerInitRequest      = opaquesuite.RegisterInitRequest
	registerInitResponse     = opaquesuite.RegisterInitResponse
	registerFinalizeRequest  = opaquesuite.RegisterFinalizeRequest
	registerFinalizeResponse = opaquesuite.RegisterFinalizeResponse
	loginInitRequest         = opaquesuite.LoginInitRequest
	loginInitResponse        = opaquesuite.LoginInitResponse
	loginFinalizeRequest     = opaquesuite.LoginFinalizeRequest
	loginFinalizeResponse    = opaquesuite.LoginFinalizeResponse
	accountKeyWrapPayload    = opaquesuite.AccountKeyWrap
)

// ----- Handlers (stubs filled in by Tasks 7-10) -----

// Register adds the four OPAQUE routes to mux. Mirrors the style used by
// AuthServer.RegisterInto: method-prefixed patterns (Go 1.22+ ServeMux) with
// mux.Handle + http.HandlerFunc. The OPAQUE endpoints are all public — the
// protocol itself authenticates the client — so no session wrapper is applied.
func (h *OpaqueAuthHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/register/init", http.HandlerFunc(h.handleRegisterInit))
	mux.Handle("POST /api/auth/register/finalize", http.HandlerFunc(h.handleRegisterFinalize))
	mux.Handle("POST /api/auth/login/init", http.HandlerFunc(h.handleLoginInit))
	mux.Handle("POST /api/auth/login/finalize", http.HandlerFunc(h.handleLoginFinalize))
	// M1i: step-up OPAQUE round-trip for sensitive operations. Same
	// protocol bytes as login; emits a short-lived single-use step_up_token.
	mux.Handle("POST /api/auth/stepup/init", http.HandlerFunc(h.handleStepUpInit))
	mux.Handle("POST /api/auth/stepup/finalize", http.HandlerFunc(h.handleStepUpFinalize))
}
