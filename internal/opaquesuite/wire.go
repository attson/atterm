package opaquesuite

// Wire types shared between the atterm-relay server (internal/relay) and the
// Go client SDK (internal/e2eeclient). Both sides speak JSON to the same
// /api/auth/{register,login}/{init,finalize} endpoints, so the two type
// definitions MUST stay byte-for-byte identical — a field-name typo on one
// side just deserializes to the zero value and silently corrupts the flow.
// Keeping them in one file so the compiler enforces alignment.
//
// This file has no external imports beyond the crypto config next to it, so
// it stays compatible with GOOS=js GOARCH=wasm builds of the browser client.

// AccountKeyWrap is the on-wire envelope for a password-wrapped account_key.
// It matches userstore.AccountKeyWrap field-for-field on the storage side.
type AccountKeyWrap struct {
	Method    string `json:"method"`
	Wrapped   []byte `json:"wrapped"`
	Nonce     []byte `json:"nonce"`
	Salt      []byte `json:"salt"`
	KDFParams string `json:"kdf_params"`
}

// ---- register ----

type RegisterInitRequest struct {
	Email          string `json:"email"`
	RegistrationKE []byte `json:"registration_ke"` // KE1 bytes from client
}

type RegisterInitResponse struct {
	RegistrationResponse []byte `json:"registration_response"` // KE2 bytes
}

type RegisterFinalizeRequest struct {
	Email              string         `json:"email"`
	RegistrationRecord []byte         `json:"registration_record"`
	AccountKeyWrap     AccountKeyWrap `json:"account_key_wrap"`
	// ClaimToken is the optional bootstrap / operator-issued one-time
	// token that promotes the new account to the role baked into the
	// token (e.g. "admin"). Empty for a normal self-service registration.
	ClaimToken string `json:"claim_token,omitempty"`
}

type RegisterFinalizeResponse struct {
	UserID       string `json:"user_id"`
	SessionToken string `json:"session_token"`
	IsAdmin      bool   `json:"is_admin"`
	RealmID      string `json:"realm_id"`
}

// ---- login ----

type LoginInitRequest struct {
	Email   string `json:"email"`
	LoginKE []byte `json:"login_ke"`
}

type LoginInitResponse struct {
	LoginResponse []byte `json:"login_response"`
	SessionID     string `json:"session_id"`
}

type LoginFinalizeRequest struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	LoginKE3  []byte `json:"login_ke3"`
}

type LoginFinalizeResponse struct {
	UserID          string         `json:"user_id"`
	SessionToken    string         `json:"session_token"`
	AccountKeyWrap  AccountKeyWrap `json:"account_key_wrap"`
	RealmID         string         `json:"realm_id"`
	HomeInstanceURL string         `json:"home_instance_url"`
}
