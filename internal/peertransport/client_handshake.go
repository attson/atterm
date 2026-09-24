package peertransport

import (
	"crypto/ecdh"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type clientHandshakeState byte

const (
	clientHandshakeAwaitHostHello clientHandshakeState = iota + 1
	clientHandshakeAwaitAuthOK
	clientHandshakeComplete
)

// ClientHandshake is the native counterpart of the browser direct handshake.
// It keeps the account-key authenticator and ephemeral private key inside Go.
type ClientHandshake struct {
	auth            HandshakeAuthenticator
	authorization   Authorization
	privateKey      *ecdh.PrivateKey
	clientPublicKey []byte
	trafficKeys     TrafficKeys
	transcriptHash  [sha256Size]byte
	state           clientHandshakeState
	helloSent       bool
	now             func() time.Time
}

// ClientHandshakeResult contains the next client message. Traffic keys become
// available only after the host confirms the transcript with AUTH_OK.
type ClientHandshakeResult struct {
	Response       []byte
	Authenticated  bool
	TrafficKeys    TrafficKeys
	TranscriptHash [sha256Size]byte
}

func NewClientHandshake(auth HandshakeAuthenticator, authorization Authorization) (*ClientHandshake, error) {
	if auth == nil {
		return nil, fmt.Errorf("%w: missing authenticator", ErrInvalidHandshake)
	}
	if authorization.AttemptID == uuid.Nil || authorization.SessionID == uuid.Nil || len(authorization.Ticket) != directTicketSize {
		return nil, fmt.Errorf("%w: malformed authorization", ErrInvalidHandshake)
	}
	privateKey, err := GenerateEphemeralKey()
	if err != nil {
		return nil, err
	}
	copyAuthorization := authorization
	copyAuthorization.Ticket = append([]byte(nil), authorization.Ticket...)
	return &ClientHandshake{
		auth:            auth,
		authorization:   copyAuthorization,
		privateKey:      privateKey,
		clientPublicKey: privateKey.PublicKey().Bytes(),
		state:           clientHandshakeAwaitHostHello,
		now:             time.Now,
	}, nil
}

func (h *ClientHandshake) ClientHello() ([]byte, error) {
	if h == nil || h.auth == nil || h.privateKey == nil || h.state != clientHandshakeAwaitHostHello || h.helloSent {
		return nil, fmt.Errorf("%w: client hello unavailable", ErrInvalidHandshake)
	}
	if h.authorization.ExpiresAtUnixMillis <= uint64(h.now().UnixMilli()) {
		return nil, fmt.Errorf("%w: authorization expired", ErrInvalidHandshake)
	}
	h.helloSent = true
	return EncodeClientHello(h.authorization.AttemptID, h.authorization.Ticket, h.clientPublicKey)
}

func (h *ClientHandshake) Handle(message []byte) (ClientHandshakeResult, error) {
	if h == nil || h.auth == nil || h.privateKey == nil {
		return ClientHandshakeResult{}, fmt.Errorf("%w: nil client state", ErrInvalidHandshake)
	}
	if h.authorization.ExpiresAtUnixMillis <= uint64(h.now().UnixMilli()) {
		return ClientHandshakeResult{}, fmt.Errorf("%w: authorization expired", ErrInvalidHandshake)
	}
	switch h.state {
	case clientHandshakeAwaitHostHello:
		return h.handleHostHello(message)
	case clientHandshakeAwaitAuthOK:
		return h.handleAuthOK(message)
	default:
		return ClientHandshakeResult{}, fmt.Errorf("%w: handshake already complete", ErrInvalidHandshake)
	}
}

func (h *ClientHandshake) handleHostHello(message []byte) (ClientHandshakeResult, error) {
	if !h.helloSent {
		return ClientHandshakeResult{}, fmt.Errorf("%w: client hello not sent", ErrInvalidHandshake)
	}
	hostPublicKey, hostProof, err := DecodeHostHello(message)
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	transcript, err := (Transcript{
		AttemptID:           h.authorization.AttemptID,
		Ticket:              h.authorization.Ticket,
		SessionID:           h.authorization.SessionID,
		UserID:              h.authorization.UserID,
		HostID:              h.authorization.HostID,
		ClientInstanceID:    h.authorization.ClientInstanceID,
		Permission:          h.authorization.Permission,
		ExpiresAtUnixMillis: h.authorization.ExpiresAtUnixMillis,
		ClientPublicKey:     h.clientPublicKey,
		HostPublicKey:       hostPublicKey,
	}).MarshalBinary()
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	if err := h.auth.VerifyProof(transcript, RoleHost, hostProof); err != nil {
		return ClientHandshakeResult{}, fmt.Errorf("%w: host proof", ErrInvalidHandshake)
	}
	h.trafficKeys, err = DeriveTrafficKeys(h.privateKey, hostPublicKey, transcript, h.auth)
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	h.transcriptHash = TranscriptHash(transcript)
	clientProof, err := h.auth.BuildProof(transcript, RoleClient)
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	finishProof, err := BuildFinishProof(h.auth, transcript)
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	response, err := EncodeClientFinish(clientProof, finishProof)
	if err != nil {
		return ClientHandshakeResult{}, err
	}
	h.state = clientHandshakeAwaitAuthOK
	return ClientHandshakeResult{Response: response}, nil
}

func (h *ClientHandshake) handleAuthOK(message []byte) (ClientHandshakeResult, error) {
	if !IsAuthOK(message) {
		return ClientHandshakeResult{}, fmt.Errorf("%w: malformed auth ok", ErrInvalidHandshake)
	}
	h.state = clientHandshakeComplete
	return ClientHandshakeResult{
		Authenticated:  true,
		TrafficKeys:    h.trafficKeys,
		TranscriptHash: h.transcriptHash,
	}, nil
}
