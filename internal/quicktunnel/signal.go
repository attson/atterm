package quicktunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	// PeerConnectPath is the accountless Peer signaling endpoint exposed by a
	// Quick Tunnel gateway.
	PeerConnectPath = "/peer/v1/connect"
	// PeerSubprotocol prevents this endpoint from being confused with another
	// WebSocket protocol on the same origin.
	PeerSubprotocol = "atterm-peer-v1"

	signalVersion          = 1
	openKind               = "open"
	authorizedKind         = "authorized"
	errorKind              = "error"
	defaultMaxConnections  = 32
	openMessageLimit       = 8 * 1024
	websocketReadLimit     = peertransport.MaxSignalMessageSize + 1024
	signalOfferLimit       = 32 * 1024
	signalCandidateLimit   = 8 * 1024
	maxCandidates          = 64
	authorizationLifetime  = 30 * time.Second
	signalHandshakeTimeout = 10 * time.Second
)

var (
	// ErrUnauthorized means Peer membership does not authorize the requested
	// session and effective permission.
	ErrUnauthorized = errors.New("quicktunnel: unauthorized")
	// ErrCapacity means the host has reached its bounded concurrent channel
	// limit.
	ErrCapacity = errors.New("quicktunnel: connection capacity reached")
	// ErrInvalidSignal covers malformed or out-of-policy SDP/ICE messages.
	ErrInvalidSignal = errors.New("quicktunnel: invalid signal")
)

// SignalType identifies one WebRTC negotiation message carried only inside
// the encrypted Peer record layer.
type SignalType string

const (
	SignalOffer        SignalType = "offer"
	SignalAnswer       SignalType = "answer"
	SignalICECandidate SignalType = "ice_candidate"
	SignalICEEnd       SignalType = "ice_end"
)

// Signal is a bounded WebRTC negotiation message.
type Signal struct {
	Version int        `json:"v"`
	Type    SignalType `json:"type"`
	Payload string     `json:"payload"`
}

// OpenRequest is the bounded routing envelope visible to the tunnel
// provider. It contains no membership token, SDP, ICE, terminal, or config
// plaintext.
type OpenRequest struct {
	Version          int       `json:"v"`
	Kind             string    `json:"kind"`
	AttemptID        uuid.UUID `json:"attempt_id"`
	Ticket           []byte    `json:"ticket"`
	SessionID        uuid.UUID `json:"session_id"`
	ClientPeerID     string    `json:"client_peer_id"`
	ClientInstanceID string    `json:"client_instance_id"`
}

// HostAuthorization supplies the local host material selected for one open
// request. The handler verifies every signed document and claim again.
type HostAuthorization struct {
	Identity              *peercrypto.Identity
	GenesisToken          string
	ClientMembershipToken string
	HostMembershipToken   string
	Permission            peertransport.Permission
}

// HostConfig controls an authenticated Peer signaling endpoint.
type HostConfig struct {
	Authorize       func(context.Context, OpenRequest) (HostAuthorization, error)
	OnAuthenticated func(*SignalChannel)
	OnSignal        func(*SignalChannel, Signal) error
	OnClosed        func(*SignalChannel, error)
	MaxConnections  int
}

// ClientConfig contains local membership material and route information for
// one outbound signaling channel.
type ClientConfig struct {
	URL                   string
	AllowInsecure         bool
	Identity              *peercrypto.Identity
	GenesisToken          string
	ClientMembershipToken string
	HostMembershipToken   string
	SessionID             uuid.UUID
	ClientInstanceID      string
	OnSignal              func(*SignalChannel, Signal) error
	OnClosed              func(*SignalChannel, error)
}

type authorizedResponse struct {
	Version             int                      `json:"v"`
	Kind                string                   `json:"kind"`
	UserID              string                   `json:"user_id"`
	HostID              string                   `json:"host_id"`
	Permission          peertransport.Permission `json:"permission"`
	ExpiresAtUnixMillis uint64                   `json:"expires_at_unix_millis"`
}

type errorResponse struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	Code    string `json:"code"`
}

// PeerHandler serves only PeerConnectPath and bounds active channels.
type PeerHandler struct {
	cfg HostConfig

	mu     sync.Mutex
	active int
}

// NewPeerHandler constructs an idle signaling handler.
func NewPeerHandler(cfg HostConfig) (*PeerHandler, error) {
	if cfg.Authorize == nil {
		return nil, errors.New("quicktunnel: missing host authorizer")
	}
	if cfg.MaxConnections < 0 {
		return nil, errors.New("quicktunnel: negative connection limit")
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = defaultMaxConnections
	}
	return &PeerHandler{cfg: cfg}, nil
}

func (h *PeerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != PeerConnectPath || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if !offersSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), PeerSubprotocol) {
		http.Error(w, "websocket subprotocol required", http.StatusUpgradeRequired)
		return
	}
	if !h.acquire() {
		http.Error(w, "connection capacity reached", http.StatusTooManyRequests)
		return
	}
	defer h.release()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{PeerSubprotocol},
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(websocketReadLimit)
	defer conn.Close(websocket.StatusInternalError, "")

	channel, err := h.authenticate(r.Context(), conn)
	if err != nil {
		return
	}
	if h.cfg.OnAuthenticated != nil {
		h.cfg.OnAuthenticated(channel)
	}
	err = channel.readLoop(r.Context())
	channel.finish(normalizeCloseError(err))
}

func (h *PeerHandler) acquire() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active >= h.cfg.MaxConnections {
		return false
	}
	h.active++
	return true
}

func (h *PeerHandler) release() {
	h.mu.Lock()
	h.active--
	h.mu.Unlock()
}

func (h *PeerHandler) authenticate(ctx context.Context, conn *websocket.Conn) (*SignalChannel, error) {
	handshakeCtx, cancel := context.WithTimeout(ctx, signalHandshakeTimeout)
	defer cancel()

	messageType, data, err := conn.Read(handshakeCtx)
	if err != nil || messageType != websocket.MessageText || len(data) > openMessageLimit {
		closePolicy(conn, "expected peer open")
		return nil, ErrUnauthorized
	}
	var request OpenRequest
	if err := decodeStrictJSON(data, &request); err != nil || validateOpenRequest(request) != nil {
		writePeerError(handshakeCtx, conn, "unauthorized")
		closePolicy(conn, "invalid peer open")
		return nil, ErrUnauthorized
	}
	authorization, err := h.cfg.Authorize(handshakeCtx, request)
	if err != nil {
		writePeerError(handshakeCtx, conn, "unauthorized")
		closePolicy(conn, "peer authorization rejected")
		return nil, ErrUnauthorized
	}
	claims, authenticator, remoteMembership, err := validateHostAuthorization(request, authorization, time.Now())
	if err != nil {
		writePeerError(handshakeCtx, conn, "unauthorized")
		closePolicy(conn, "peer authorization rejected")
		return nil, ErrUnauthorized
	}
	encoded, err := json.Marshal(claims)
	if err != nil || conn.Write(handshakeCtx, websocket.MessageText, encoded) != nil {
		return nil, errors.New("quicktunnel: write authorization")
	}

	handshake, err := peertransport.NewHostHandshake(authenticator, authorizationFrom(request, claims))
	if err != nil {
		return nil, err
	}
	clientHello, err := readBinary(handshakeCtx, conn)
	if err != nil {
		return nil, err
	}
	result, err := handshake.Handle(clientHello)
	if err != nil || conn.Write(handshakeCtx, websocket.MessageBinary, result.Response) != nil {
		closePolicy(conn, "peer handshake failed")
		return nil, ErrUnauthorized
	}
	clientFinish, err := readBinary(handshakeCtx, conn)
	if err != nil {
		return nil, err
	}
	result, err = handshake.Handle(clientFinish)
	if err != nil || !result.Authenticated || conn.Write(handshakeCtx, websocket.MessageBinary, result.Response) != nil {
		closePolicy(conn, "peer handshake failed")
		return nil, ErrUnauthorized
	}
	exactAuthorization := authorizationFrom(request, claims)
	return newHostSignalChannel(conn, result, exactAuthorization, authenticator, remoteMembership, h.cfg.OnSignal, h.cfg.OnClosed)
}

// Dial opens and authenticates one accountless Peer signaling channel.
func Dial(ctx context.Context, cfg ClientConfig) (*SignalChannel, error) {
	endpoint, err := peerWebSocketURL(cfg.URL, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	if cfg.Identity == nil || cfg.SessionID == uuid.Nil || !validIdentifier(cfg.ClientInstanceID) {
		return nil, fmt.Errorf("%w: client configuration", ErrUnauthorized)
	}
	genesis, err := peerproto.VerifyGenesis(cfg.GenesisToken)
	if err != nil {
		return nil, fmt.Errorf("%w: genesis", ErrUnauthorized)
	}
	clientMembership, err := peerproto.VerifyGrant(cfg.ClientMembershipToken, genesis, time.Now())
	if err != nil || clientMembership.Document.SubjectPeerID != cfg.Identity.PeerID() || !sessionAllowed(clientMembership, cfg.SessionID) {
		return nil, fmt.Errorf("%w: client membership", ErrUnauthorized)
	}

	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{Subprotocols: []string{PeerSubprotocol}})
	if err != nil {
		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			return nil, ErrCapacity
		}
		return nil, fmt.Errorf("dial Quick Tunnel peer endpoint: %w", err)
	}
	conn.SetReadLimit(websocketReadLimit)
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close(websocket.StatusInternalError, "")
		}
	}()

	request, err := newOpenRequest(cfg)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(request)
	if err != nil || conn.Write(ctx, websocket.MessageText, encoded) != nil {
		return nil, errors.New("quicktunnel: write peer open")
	}
	messageType, data, err := conn.Read(ctx)
	if err != nil || messageType != websocket.MessageText || len(data) > openMessageLimit {
		return nil, fmt.Errorf("%w: peer authorization response", ErrUnauthorized)
	}
	var wire struct {
		Version int    `json:"v"`
		Kind    string `json:"kind"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(data, &wire); err == nil && wire.Kind == errorKind {
		if wire.Code == "capacity" {
			return nil, ErrCapacity
		}
		return nil, ErrUnauthorized
	}
	var claims authorizedResponse
	if err := decodeStrictJSON(data, &claims); err != nil || validateAuthorizedResponse(claims, genesis, cfg, clientMembership) != nil {
		return nil, fmt.Errorf("%w: invalid host claims", ErrUnauthorized)
	}
	authenticator, err := peertransport.NewPeerMembershipAuthenticator(
		cfg.Identity, peertransport.RoleClient, cfg.GenesisToken,
		cfg.ClientMembershipToken, cfg.HostMembershipToken, time.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: client authenticator", ErrUnauthorized)
	}
	handshake, err := peertransport.NewClientHandshake(authenticator, authorizationFrom(request, claims))
	if err != nil {
		return nil, err
	}
	clientHello, err := handshake.ClientHello()
	if err != nil || conn.Write(ctx, websocket.MessageBinary, clientHello) != nil {
		return nil, fmt.Errorf("%w: client hello", ErrUnauthorized)
	}
	hostHello, err := readBinary(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("%w: host hello", ErrUnauthorized)
	}
	result, err := handshake.Handle(hostHello)
	if err != nil || conn.Write(ctx, websocket.MessageBinary, result.Response) != nil {
		return nil, fmt.Errorf("%w: host proof", ErrUnauthorized)
	}
	authOK, err := readBinary(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("%w: auth confirmation", ErrUnauthorized)
	}
	result, err = handshake.Handle(authOK)
	if err != nil || !result.Authenticated {
		return nil, fmt.Errorf("%w: auth confirmation", ErrUnauthorized)
	}
	exactAuthorization := authorizationFrom(request, claims)
	channel, err := newClientSignalChannel(conn, result, exactAuthorization, authenticator, authenticator.RemoteMembershipToken(), cfg.OnSignal, cfg.OnClosed)
	if err != nil {
		return nil, err
	}
	ok = true
	go func() {
		err := channel.readLoop(context.Background())
		channel.finish(normalizeCloseError(err))
	}()
	return channel, nil
}

func newOpenRequest(cfg ClientConfig) (OpenRequest, error) {
	ticket := make([]byte, 32)
	if _, err := rand.Read(ticket); err != nil {
		return OpenRequest{}, fmt.Errorf("generate peer ticket: %w", err)
	}
	return OpenRequest{
		Version: signalVersion, Kind: openKind, AttemptID: uuid.New(), Ticket: ticket,
		SessionID: cfg.SessionID, ClientPeerID: cfg.Identity.PeerID(),
		ClientInstanceID: cfg.ClientInstanceID,
	}, nil
}

func validateOpenRequest(request OpenRequest) error {
	if request.Version != signalVersion || request.Kind != openKind || request.AttemptID == uuid.Nil ||
		request.SessionID == uuid.Nil || len(request.Ticket) != 32 ||
		!validIdentifier(request.ClientPeerID) || !validIdentifier(request.ClientInstanceID) {
		return ErrUnauthorized
	}
	return nil
}

func validateHostAuthorization(request OpenRequest, authorization HostAuthorization, now time.Time) (authorizedResponse, *peertransport.PeerMembershipAuthenticator, string, error) {
	genesis, err := peerproto.VerifyGenesis(authorization.GenesisToken)
	if err != nil {
		return authorizedResponse{}, nil, "", err
	}
	clientMembership, err := peerproto.VerifyGrant(authorization.ClientMembershipToken, genesis, now)
	if err != nil {
		return authorizedResponse{}, nil, "", err
	}
	hostMembership, err := peerproto.VerifyGrant(authorization.HostMembershipToken, genesis, now)
	if err != nil {
		return authorizedResponse{}, nil, "", err
	}
	if authorization.Identity == nil || request.ClientPeerID != clientMembership.Document.SubjectPeerID ||
		hostMembership.Document.SubjectPeerID != authorization.Identity.PeerID() ||
		!sessionAllowed(clientMembership, request.SessionID) || !sessionAllowed(hostMembership, request.SessionID) ||
		!permissionAllowed(authorization.Permission, clientMembership.Document.Permission) ||
		!permissionAllowed(authorization.Permission, hostMembership.Document.Permission) {
		return authorizedResponse{}, nil, "", ErrUnauthorized
	}
	authenticator, err := peertransport.NewPeerMembershipAuthenticator(
		authorization.Identity, peertransport.RoleHost, authorization.GenesisToken,
		authorization.ClientMembershipToken, authorization.HostMembershipToken, now,
	)
	if err != nil {
		return authorizedResponse{}, nil, "", err
	}
	claims := authorizedResponse{
		Version: signalVersion, Kind: authorizedKind, UserID: genesis.Document.SpaceID,
		HostID: hostMembership.Document.SubjectPeerID, Permission: authorization.Permission,
		ExpiresAtUnixMillis: uint64(now.Add(authorizationLifetime).UnixMilli()),
	}
	return claims, authenticator, authenticator.RemoteMembershipToken(), nil
}

func validateAuthorizedResponse(claims authorizedResponse, genesis peerproto.VerifiedGenesis, cfg ClientConfig, clientMembership peerproto.VerifiedGrant) error {
	if claims.Version != signalVersion || claims.Kind != authorizedKind || claims.UserID != genesis.Document.SpaceID ||
		!validIdentifier(claims.HostID) || claims.ExpiresAtUnixMillis <= uint64(time.Now().UnixMilli()) ||
		!permissionAllowed(claims.Permission, clientMembership.Document.Permission) {
		return ErrUnauthorized
	}
	hostMembership, err := peerproto.VerifyGrant(cfg.HostMembershipToken, genesis, time.Now())
	if err != nil || hostMembership.Document.SubjectPeerID != claims.HostID || !sessionAllowed(hostMembership, cfg.SessionID) ||
		!permissionAllowed(claims.Permission, hostMembership.Document.Permission) {
		return ErrUnauthorized
	}
	return nil
}

func authorizationFrom(request OpenRequest, claims authorizedResponse) peertransport.Authorization {
	return peertransport.Authorization{
		AttemptID: request.AttemptID, Ticket: request.Ticket, SessionID: request.SessionID,
		UserID: claims.UserID, HostID: claims.HostID, ClientInstanceID: request.ClientInstanceID,
		Permission: claims.Permission, ExpiresAtUnixMillis: claims.ExpiresAtUnixMillis,
	}
}

func permissionAllowed(effective peertransport.Permission, granted peerproto.Permission) bool {
	var ceiling peertransport.Permission
	switch granted {
	case peerproto.PermissionView:
		ceiling = peertransport.PermissionView
	case peerproto.PermissionControl:
		ceiling = peertransport.PermissionControl
	case peerproto.PermissionFull:
		ceiling = peertransport.PermissionFull
	default:
		return false
	}
	return effective >= peertransport.PermissionView && effective <= ceiling
}

func sessionAllowed(membership peerproto.VerifiedGrant, sessionID uuid.UUID) bool {
	allowed := membership.Document.AllowedSessionIDs
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == sessionID.String() {
			return true
		}
	}
	return false
}

func peerWebSocketURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("quicktunnel: invalid peer URL")
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "wss":
	case "http":
		if !allowInsecure {
			return "", errors.New("quicktunnel: insecure peer URL rejected")
		}
		parsed.Scheme = "ws"
	case "ws":
		if !allowInsecure {
			return "", errors.New("quicktunnel: insecure peer URL rejected")
		}
	default:
		return "", errors.New("quicktunnel: invalid peer URL scheme")
	}
	parsed.Path = PeerConnectPath
	return parsed.String(), nil
}

func validIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value)
}

func offersSubprotocol(header, wanted string) bool {
	for _, offered := range strings.Split(header, ",") {
		if strings.TrimSpace(offered) == wanted {
			return true
		}
	}
	return false
}

func decodeStrictJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func readBinary(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if messageType != websocket.MessageBinary {
		return nil, errors.New("quicktunnel: expected binary handshake")
	}
	return data, nil
}

func writePeerError(ctx context.Context, conn *websocket.Conn, code string) {
	data, _ := json.Marshal(errorResponse{Version: signalVersion, Kind: errorKind, Code: code})
	_ = conn.Write(ctx, websocket.MessageText, data)
}

func closePolicy(conn *websocket.Conn, reason string) {
	_ = conn.Close(websocket.StatusPolicyViolation, reason)
}

func normalizeCloseError(err error) error {
	status := websocket.CloseStatus(err)
	if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return nil
	}
	return err
}
