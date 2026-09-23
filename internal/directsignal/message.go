// Package directsignal defines the Relay-assisted P2P signaling JSON schema.
// It deliberately contains no WebRTC implementation so Relay and clients can
// share the wire contract without coupling the control plane to Pion.
package directsignal

const Version = 1

type Message struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`

	Role             string   `json:"role,omitempty"`
	HostID           string   `json:"host_id,omitempty"`
	SessionIDs       []string `json:"session_ids,omitempty"`
	ClientInstanceID string   `json:"client_instance_id,omitempty"`

	RequestID string `json:"request_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	SinceSeq  uint64 `json:"since_seq,omitempty"`

	AttemptID      string `json:"attempt_id,omitempty"`
	Ticket         string `json:"ticket,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	Permission     string `json:"permission,omitempty"`
	ExpiresAtUnixM int64  `json:"expires_at_unix_ms,omitempty"`

	SignalType string `json:"signal_type,omitempty"`
	Payload    string `json:"payload,omitempty"`

	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`

	// BytesAvoided is a bounded aggregate delta reported by an authenticated
	// direct host. It contains no session identifier, address, or payload.
	BytesAvoided uint64 `json:"bytes_avoided,omitempty"`
}
