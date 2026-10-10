package rendezvous

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/attson/atterm/internal/peercrypto"
)

const (
	Version     = 1
	ConnectPath = "/v1/connect"
	HealthPath  = "/healthz"
	MetricsPath = "/metrics"
	Subprotocol = "atterm-rendezvous-v1"

	MaxPayloadBytes = 64 << 10

	KindChallenge  = "challenge"
	KindRegister   = "register"
	KindRegistered = "registered"
	KindPresence   = "presence"
	KindPublish    = "publish"
	KindSignal     = "signal"
	KindAck        = "ack"
	KindError      = "error"

	PresenceOnline  = "online"
	PresenceOffline = "offline"

	DeliveryDelivered = "delivered"
	DeliveryQueued    = "queued"

	CodeUnauthorized     = "unauthorized"
	CodeInvalidMessage   = "invalid_message"
	CodeMessageTooLarge  = "message_too_large"
	CodeTopicCapacity    = "topic_capacity"
	CodePresenceConflict = "presence_conflict"
	CodeMailboxCapacity  = "mailbox_capacity"
	CodeRateLimited      = "rate_limited"
	CodeServerCapacity   = "server_capacity"
)

const registrationDomain = "atterm-rendezvous-register-v1"

// Role describes only how a connection participates in ephemeral discovery.
// It is not an authorization claim; Peer membership is verified end-to-end.
type Role string

const (
	RoleHost   Role = "host"
	RoleMember Role = "member"
)

// RegisterMessage proves possession of a P-256 Peer identity while binding
// that proof to one server challenge and opaque routing identity.
type RegisterMessage struct {
	Version    int    `json:"v"`
	Kind       string `json:"kind"`
	Topic      string `json:"topic"`
	PresenceID string `json:"presence_id"`
	Role       Role   `json:"role"`
	PublicKey  string `json:"public_key"`
	Signature  string `json:"signature"`
}

// PublishMessage carries application-encrypted signaling bytes. The service
// treats Payload as opaque and uses MessageID only for bounded retry dedupe.
type PublishMessage struct {
	Version   int    `json:"v"`
	Kind      string `json:"kind"`
	MessageID string `json:"message_id"`
	To        string `json:"to"`
	Payload   string `json:"payload"`
}

// Presence describes one currently registered opaque routing identity.
type Presence struct {
	PresenceID string `json:"presence_id"`
	Role       Role   `json:"role"`
}

// EventMessage is the bounded server-to-client event envelope.
type EventMessage struct {
	Version    int        `json:"v"`
	Kind       string     `json:"kind"`
	Challenge  string     `json:"challenge,omitempty"`
	ExpiresAt  int64      `json:"expires_at,omitempty"`
	Presence   []Presence `json:"presence,omitempty"`
	Event      string     `json:"event,omitempty"`
	PresenceID string     `json:"presence_id,omitempty"`
	Role       Role       `json:"role,omitempty"`
	MessageID  string     `json:"message_id,omitempty"`
	From       string     `json:"from,omitempty"`
	Payload    string     `json:"payload,omitempty"`
	StoredAt   int64      `json:"stored_at,omitempty"`
	State      string     `json:"state,omitempty"`
	Code       string     `json:"code,omitempty"`
}

// SignRegistration creates the response to one server challenge. Topic and
// presenceID are independent 32-byte opaque values and reveal no Space ID.
func SignRegistration(identity *peercrypto.Identity, challenge, topic, presenceID string, role Role) (RegisterMessage, error) {
	if identity == nil {
		return RegisterMessage{}, errors.New("rendezvous: missing identity")
	}
	transcript, err := registrationTranscript(challenge, topic, presenceID, role)
	if err != nil {
		return RegisterMessage{}, err
	}
	signature, err := identity.Sign(transcript)
	if err != nil {
		return RegisterMessage{}, fmt.Errorf("rendezvous: sign registration: %w", err)
	}
	return RegisterMessage{
		Version: Version, Kind: KindRegister, Topic: topic, PresenceID: presenceID, Role: role,
		PublicKey: base64.RawURLEncoding.EncodeToString(identity.PublicBytes()),
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}

func verifyRegistration(request RegisterMessage, challenge string) error {
	if request.Version != Version || request.Kind != KindRegister {
		return errors.New("rendezvous: unsupported registration")
	}
	transcript, err := registrationTranscript(challenge, request.Topic, request.PresenceID, request.Role)
	if err != nil {
		return err
	}
	publicKey, err := decodeExact(request.PublicKey, peercrypto.PublicKeySize, "public key")
	if err != nil {
		return err
	}
	signature, err := decodeExact(request.Signature, peercrypto.SignatureSize, "signature")
	if err != nil {
		return err
	}
	if err := peercrypto.Verify(publicKey, transcript, signature); err != nil {
		return fmt.Errorf("rendezvous: verify registration: %w", err)
	}
	return nil
}

func registrationTranscript(challenge, topic, presenceID string, role Role) ([]byte, error) {
	challengeBytes, err := decodeExact(challenge, 32, "challenge")
	if err != nil {
		return nil, err
	}
	topicBytes, err := decodeExact(topic, 32, "topic")
	if err != nil {
		return nil, err
	}
	presenceBytes, err := decodeExact(presenceID, 32, "presence id")
	if err != nil {
		return nil, err
	}
	if role != RoleHost && role != RoleMember {
		return nil, errors.New("rendezvous: invalid role")
	}
	var out bytes.Buffer
	writeTranscriptPart(&out, []byte(registrationDomain))
	writeTranscriptPart(&out, challengeBytes)
	writeTranscriptPart(&out, topicBytes)
	writeTranscriptPart(&out, presenceBytes)
	writeTranscriptPart(&out, []byte(role))
	return out.Bytes(), nil
}

func writeTranscriptPart(out *bytes.Buffer, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	out.Write(size[:])
	out.Write(value)
}

func decodeExact(value string, size int, label string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) != size || base64.RawURLEncoding.EncodeToString(raw) != value {
		return nil, fmt.Errorf("rendezvous: invalid %s", label)
	}
	return raw, nil
}
