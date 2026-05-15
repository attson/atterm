package relay

// WebSocket close codes for auth + session ownership failures.
// Codes 4000-4999 are reserved for application use.
const (
	CloseCodeAuthInvalid            = 4001 // invalid / revoked api token
	CloseCodeSessionIDOwnerMismatch = 4002 // session id collision across users
	CloseCodeForbidden              = 4003 // principal forbidden at this entry
)

// Reason strings sent in WS close frames; the desktop maps these to
// localized banner text (see Tasks 8.0+).
const (
	CloseReasonAuthInvalidToken       = "auth_invalid_token"
	CloseReasonSessionIDOwnerMismatch = "session_id_owner_mismatch"
	CloseReasonForbidden              = "forbidden"
	CloseReasonAuthUserDisabled       = "auth_user_disabled"
)
