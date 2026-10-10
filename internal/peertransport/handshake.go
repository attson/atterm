package peertransport

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	HandshakeVersion = 1

	handshakeClientHello  byte = 1
	handshakeHostHello    byte = 2
	handshakeClientFinish byte = 3
	handshakeAuthOK       byte = 4

	handshakeHeaderSize     = 2
	clientHelloMessageSize  = handshakeHeaderSize + 16 + directTicketSize + p256PublicKeySize
	hostHelloMessageSize    = handshakeHeaderSize + p256PublicKeySize + sha256Size
	clientFinishMessageSize = handshakeHeaderSize + 2*sha256Size
	authOKMessageSize       = handshakeHeaderSize
	directHandshakeTimeout  = 10 * time.Second
	sha256Size              = 32
	maxAuthProofSize        = 1024
)

var ErrInvalidHandshake = errors.New("peertransport: invalid handshake")

// Authorization is the exact pending route authorization bound into a direct
// handshake. Relay mode fills it from a direct ticket; Peer mode will fill it
// from its authenticated gateway attempt. Ticket is always copied.
type Authorization struct {
	AttemptID           uuid.UUID
	Ticket              []byte
	SessionID           uuid.UUID
	UserID              string
	HostID              string
	ClientInstanceID    string
	Permission          Permission
	ExpiresAtUnixMillis uint64
}

type hostHandshakeState byte

const (
	hostHandshakeAwaitClientHello hostHandshakeState = iota + 1
	hostHandshakeAwaitClientFinish
	hostHandshakeComplete
)

// HostHandshake authenticates one DataChannel against a route authorization.
// It is deliberately independent of Pion so the same state machine can be
// tested without network timing and reused by later transports.
type HostHandshake struct {
	auth            HandshakeAuthenticator
	authorization   Authorization
	privateKey      *ecdh.PrivateKey
	clientPublicKey []byte
	hostPublicKey   []byte
	transcript      []byte
	proofSize       int
	state           hostHandshakeState
	now             func() time.Time
}

// HostHandshakeResult contains the only response valid for the processed
// message. TrafficKeys are populated only alongside Authenticated=true.
type HostHandshakeResult struct {
	Response       []byte
	Authenticated  bool
	TrafficKeys    TrafficKeys
	TranscriptHash [sha256Size]byte
}

func NewHostHandshake(auth HandshakeAuthenticator, authorization Authorization) (*HostHandshake, error) {
	if auth == nil {
		return nil, fmt.Errorf("%w: missing authenticator", ErrInvalidHandshake)
	}
	if authorization.AttemptID == uuid.Nil || authorization.SessionID == uuid.Nil || len(authorization.Ticket) != directTicketSize {
		return nil, fmt.Errorf("%w: malformed authorization", ErrInvalidHandshake)
	}
	proofSize, err := validateProofSize(auth)
	if err != nil {
		return nil, err
	}
	privateKey, err := GenerateEphemeralKey()
	if err != nil {
		return nil, err
	}
	copyAuthorization := authorization
	copyAuthorization.Ticket = append([]byte(nil), authorization.Ticket...)
	return &HostHandshake{
		auth:          auth,
		authorization: copyAuthorization,
		proofSize:     proofSize,
		privateKey:    privateKey,
		hostPublicKey: privateKey.PublicKey().Bytes(),
		state:         hostHandshakeAwaitClientHello,
		now:           time.Now,
	}, nil
}

// Handle consumes the next binary handshake message. Any error is terminal for
// the direct attempt; callers close the DataChannel and consume the ticket.
func (h *HostHandshake) Handle(message []byte) (HostHandshakeResult, error) {
	if h == nil || h.auth == nil || h.privateKey == nil {
		return HostHandshakeResult{}, fmt.Errorf("%w: nil host state", ErrInvalidHandshake)
	}
	if h.authorization.ExpiresAtUnixMillis <= uint64(h.now().UnixMilli()) {
		return HostHandshakeResult{}, fmt.Errorf("%w: authorization expired", ErrInvalidHandshake)
	}
	switch h.state {
	case hostHandshakeAwaitClientHello:
		return h.handleClientHello(message)
	case hostHandshakeAwaitClientFinish:
		return h.handleClientFinish(message)
	default:
		return HostHandshakeResult{}, fmt.Errorf("%w: handshake already complete", ErrInvalidHandshake)
	}
}

func (h *HostHandshake) handleClientHello(message []byte) (HostHandshakeResult, error) {
	if len(message) != clientHelloMessageSize || message[0] != HandshakeVersion || message[1] != handshakeClientHello {
		return HostHandshakeResult{}, fmt.Errorf("%w: expected client hello", ErrInvalidHandshake)
	}
	attemptID, err := uuid.FromBytes(message[2:18])
	if err != nil || attemptID != h.authorization.AttemptID {
		return HostHandshakeResult{}, fmt.Errorf("%w: attempt mismatch", ErrInvalidHandshake)
	}
	ticket := message[18 : 18+directTicketSize]
	if subtle.ConstantTimeCompare(ticket, h.authorization.Ticket) != 1 {
		return HostHandshakeResult{}, fmt.Errorf("%w: ticket mismatch", ErrInvalidHandshake)
	}
	clientPublicKey := append([]byte(nil), message[18+directTicketSize:]...)
	transcript, err := (Transcript{
		AttemptID:           h.authorization.AttemptID,
		Ticket:              h.authorization.Ticket,
		SessionID:           h.authorization.SessionID,
		UserID:              h.authorization.UserID,
		HostID:              h.authorization.HostID,
		ClientInstanceID:    h.authorization.ClientInstanceID,
		Permission:          h.authorization.Permission,
		ExpiresAtUnixMillis: h.authorization.ExpiresAtUnixMillis,
		ClientPublicKey:     clientPublicKey,
		HostPublicKey:       h.hostPublicKey,
	}).MarshalBinary()
	if err != nil {
		return HostHandshakeResult{}, err
	}
	hostProof, err := h.auth.BuildProof(transcript, RoleHost)
	if err != nil || len(hostProof) != h.proofSize {
		return HostHandshakeResult{}, fmt.Errorf("%w: build host proof", ErrInvalidHandshake)
	}
	h.transcript = transcript
	h.clientPublicKey = clientPublicKey
	h.state = hostHandshakeAwaitClientFinish
	response := make([]byte, handshakeHeaderSize+p256PublicKeySize+h.proofSize)
	response[0] = HandshakeVersion
	response[1] = handshakeHostHello
	copy(response[2:2+p256PublicKeySize], h.hostPublicKey)
	copy(response[2+p256PublicKeySize:], hostProof)
	return HostHandshakeResult{Response: response}, nil
}

func (h *HostHandshake) handleClientFinish(message []byte) (HostHandshakeResult, error) {
	if len(message) != handshakeHeaderSize+h.proofSize+sha256Size || message[0] != HandshakeVersion || message[1] != handshakeClientFinish {
		return HostHandshakeResult{}, fmt.Errorf("%w: expected client finish", ErrInvalidHandshake)
	}
	clientProof := message[2 : 2+h.proofSize]
	if err := h.auth.VerifyProof(h.transcript, RoleClient, clientProof); err != nil {
		return HostHandshakeResult{}, fmt.Errorf("%w: client proof", ErrInvalidHandshake)
	}
	wantFinish, err := BuildFinishProof(h.auth, h.transcript)
	if err != nil || !hmac.Equal(wantFinish, message[2+h.proofSize:]) {
		return HostHandshakeResult{}, fmt.Errorf("%w: finish proof", ErrInvalidHandshake)
	}
	keys, err := DeriveTrafficKeys(h.privateKey, h.clientPublicKey, h.transcript, h.auth)
	if err != nil {
		return HostHandshakeResult{}, err
	}
	h.state = hostHandshakeComplete
	return HostHandshakeResult{
		Response:       []byte{HandshakeVersion, handshakeAuthOK},
		Authenticated:  true,
		TrafficKeys:    keys,
		TranscriptHash: TranscriptHash(h.transcript),
	}, nil
}

// EncodeClientHello builds the fixed-size first DataChannel message.
func EncodeClientHello(attemptID uuid.UUID, ticket, clientPublicKey []byte) ([]byte, error) {
	if attemptID == uuid.Nil || len(ticket) != directTicketSize || len(clientPublicKey) != p256PublicKeySize {
		return nil, fmt.Errorf("%w: malformed client hello", ErrInvalidHandshake)
	}
	if _, err := ecdh.P256().NewPublicKey(clientPublicKey); err != nil {
		return nil, fmt.Errorf("%w: client public key", ErrInvalidHandshake)
	}
	message := make([]byte, clientHelloMessageSize)
	message[0] = HandshakeVersion
	message[1] = handshakeClientHello
	copy(message[2:18], attemptID[:])
	copy(message[18:18+directTicketSize], ticket)
	copy(message[18+directTicketSize:], clientPublicKey)
	return message, nil
}

// DecodeHostHello returns copies of the host public key and proof.
func DecodeHostHello(message []byte) (hostPublicKey, hostProof []byte, err error) {
	return decodeHostHello(message, sha256Size)
}

func decodeHostHello(message []byte, proofSize int) (hostPublicKey, hostProof []byte, err error) {
	if len(message) != handshakeHeaderSize+p256PublicKeySize+proofSize || message[0] != HandshakeVersion || message[1] != handshakeHostHello {
		return nil, nil, fmt.Errorf("%w: malformed host hello", ErrInvalidHandshake)
	}
	publicKey := message[2 : 2+p256PublicKeySize]
	if _, err := ecdh.P256().NewPublicKey(publicKey); err != nil {
		return nil, nil, fmt.Errorf("%w: host public key", ErrInvalidHandshake)
	}
	return append([]byte(nil), publicKey...), append([]byte(nil), message[2+p256PublicKeySize:]...), nil
}

func EncodeClientFinish(clientProof, finishProof []byte) ([]byte, error) {
	if len(clientProof) != sha256Size {
		return nil, fmt.Errorf("%w: malformed client finish", ErrInvalidHandshake)
	}
	return encodeClientFinish(clientProof, finishProof, sha256Size)
}

func encodeClientFinish(clientProof, finishProof []byte, proofSize int) ([]byte, error) {
	if proofSize <= 0 || proofSize > maxAuthProofSize || len(clientProof) != proofSize || len(finishProof) != sha256Size {
		return nil, fmt.Errorf("%w: malformed client finish", ErrInvalidHandshake)
	}
	message := make([]byte, handshakeHeaderSize+proofSize+sha256Size)
	message[0] = HandshakeVersion
	message[1] = handshakeClientFinish
	copy(message[2:2+proofSize], clientProof)
	copy(message[2+proofSize:], finishProof)
	return message, nil
}

func validateProofSize(auth HandshakeAuthenticator) (int, error) {
	size := auth.ProofSize()
	if size <= 0 || size > maxAuthProofSize {
		return 0, fmt.Errorf("%w: authenticator proof size %d", ErrInvalidHandshake, size)
	}
	return size, nil
}

func IsAuthOK(message []byte) bool {
	return len(message) == authOKMessageSize && message[0] == HandshakeVersion && message[1] == handshakeAuthOK
}

// DirectHandshakeTimeout is the maximum time from DataChannel open to AUTH_OK.
func DirectHandshakeTimeout() time.Duration { return directHandshakeTimeout }
