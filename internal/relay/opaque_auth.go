package relay

import (
	"encoding/json"
	"net/http"

	"github.com/attson/atterm/internal/userstore"
)

// OpaqueAuthHandler owns the OPAQUE-based registration and login HTTP
// endpoints. It is wired by server.go after the OpaqueServer singleton
// has loaded its persisted seed.
//
// The four endpoints (/api/auth/register/{init,finalize}, /api/auth/login/{init,finalize})
// are stubs at this stage — handler bodies are filled in by Tasks 7-10. All
// stubs return 501 Not Implemented so that an accidentally-wired server cannot
// silently appear to "work".
type OpaqueAuthHandler struct {
	store *userstore.SQLiteStore
	srv   *OpaqueServer
}

// NewOpaqueAuthHandler constructs the handler. Both store and srv must be
// non-nil; the OpaqueServer is expected to have been initialized via
// LoadOrInitOpaqueServer before this constructor is called.
func NewOpaqueAuthHandler(store *userstore.SQLiteStore, srv *OpaqueServer) *OpaqueAuthHandler {
	return &OpaqueAuthHandler{store: store, srv: srv}
}

// ----- Wire types -----

type registerInitRequest struct {
	Email          string `json:"email"`
	RegistrationKE []byte `json:"registration_ke"` // KE1 bytes from client
}

type registerInitResponse struct {
	RegistrationResponse []byte `json:"registration_response"` // KE2 bytes
}

type registerFinalizeRequest struct {
	Email              string                `json:"email"`
	RegistrationRecord []byte                `json:"registration_record"`
	AccountKeyWrap     accountKeyWrapPayload `json:"account_key_wrap"`
}

type accountKeyWrapPayload struct {
	Method    string `json:"method"`
	Wrapped   []byte `json:"wrapped"`
	Nonce     []byte `json:"nonce"`
	Salt      []byte `json:"salt"`
	KDFParams string `json:"kdf_params"`
}

type registerFinalizeResponse struct {
	UserID       string `json:"user_id"`
	SessionToken string `json:"session_token"`
}

type loginInitRequest struct {
	Email   string `json:"email"`
	LoginKE []byte `json:"login_ke"`
}

type loginInitResponse struct {
	LoginResponse []byte `json:"login_response"`
	SessionID     string `json:"session_id"`
}

type loginFinalizeRequest struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	LoginKE3  []byte `json:"login_ke3"`
}

type loginFinalizeResponse struct {
	UserID         string                `json:"user_id"`
	SessionToken   string                `json:"session_token"`
	AccountKeyWrap accountKeyWrapPayload `json:"account_key_wrap"`
}

// ----- Handlers (stubs filled in by Tasks 7-10) -----

// handleRegisterInit consumes the client's KE1 (RegistrationRequest) and
// returns the server's KE2 (RegistrationResponse). No user row is created at
// this step — finalize is what writes to the DB. The credential identifier
// we hand to the OPAQUE library is the email bytes; this must stay stable
// across init/finalize/login for a given user, which is naturally true
// because the email is the lookup key on the client side too.
func (h *OpaqueAuthHandler) handleRegisterInit(w http.ResponseWriter, r *http.Request) {
	var req registerInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || len(req.RegistrationKE) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	sv, err := h.srv.newServer()
	if err != nil {
		http.Error(w, "internal: opaque server", http.StatusInternalServerError)
		return
	}

	// bytemare/opaque v0.10 exposes deserialization via the Server's
	// Deserialize field (not on Configuration directly). Same goes for
	// turning the stored AKE public-key bytes back into a *group.Element
	// — RegistrationResponse takes the typed element, not raw bytes.
	regReq, err := sv.Deserialize.RegistrationRequest(req.RegistrationKE)
	if err != nil {
		http.Error(w, "bad registration_ke", http.StatusBadRequest)
		return
	}
	pks, err := sv.Deserialize.DecodeAkePublicKey(h.srv.akePublic)
	if err != nil {
		http.Error(w, "internal: decode server public key", http.StatusInternalServerError)
		return
	}

	ke2 := sv.RegistrationResponse(regReq, pks, []byte(req.Email), h.srv.oprfSeed)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(registerInitResponse{
		RegistrationResponse: ke2.Serialize(),
	})
}

func (h *OpaqueAuthHandler) handleRegisterFinalize(w http.ResponseWriter, r *http.Request) {
	writeNotImpl(w)
}

func (h *OpaqueAuthHandler) handleLoginInit(w http.ResponseWriter, r *http.Request) {
	writeNotImpl(w)
}

func (h *OpaqueAuthHandler) handleLoginFinalize(w http.ResponseWriter, r *http.Request) {
	writeNotImpl(w)
}

func writeNotImpl(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

// Register adds the four OPAQUE routes to mux. Mirrors the style used by
// AuthServer.RegisterInto: method-prefixed patterns (Go 1.22+ ServeMux) with
// mux.Handle + http.HandlerFunc. The OPAQUE endpoints are all public — the
// protocol itself authenticates the client — so no session wrapper is applied.
func (h *OpaqueAuthHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/register/init", http.HandlerFunc(h.handleRegisterInit))
	mux.Handle("POST /api/auth/register/finalize", http.HandlerFunc(h.handleRegisterFinalize))
	mux.Handle("POST /api/auth/login/init", http.HandlerFunc(h.handleLoginInit))
	mux.Handle("POST /api/auth/login/finalize", http.HandlerFunc(h.handleLoginFinalize))
}
