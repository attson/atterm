package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/directsignal"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	directSignalVersion      = directsignal.Version
	directSignalReadLimit    = 64 * 1024
	directSignalWriteWait    = 10 * time.Second
	directSignalPingPeriod   = 25 * time.Second
	directSignalHelloTimeout = 10 * time.Second
	directSignalQueueDepth   = 64
	directTicketTTL          = 30 * time.Second
	directTicketBytes        = 32
	directMaxSessionsPerHost = 256
	directMaxAttemptsPerPeer = 8
	directMaxAttempts        = 1024
	directMaxIdentifierBytes = 128
	directMaxSDPBytes        = 32 * 1024
	directMaxICEBytes        = 8 * 1024
	directMaxICEPerSide      = 64
)

type directSignalMessage = directsignal.Message

type directSignalPeer struct {
	ownerUserID      string
	role             string
	hostID           string
	clientInstanceID string
	out              chan directSignalMessage
	cancel           context.CancelFunc
}

func (p *directSignalPeer) send(message directSignalMessage) bool {
	select {
	case p.out <- message:
		return true
	default:
		p.cancel()
		return false
	}
}

type directSignalAttempt struct {
	id               uuid.UUID
	requestID        string
	sessionID        uuid.UUID
	ticketHash       [sha256.Size]byte
	ownerUserID      string
	hostID           string
	clientInstanceID string
	permission       string
	expiresAt        time.Time
	client           *directSignalPeer
	host             *directSignalPeer
	offerSeen        bool
	answerSeen       bool
	clientICE        int
	hostICE          int
	clientICEEnd     bool
	hostICEEnd       bool
}

type directSignalHub struct {
	mu       sync.Mutex
	hosts    map[uuid.UUID]*directSignalPeer
	attempts map[uuid.UUID]*directSignalAttempt
	now      func() time.Time
}

func newDirectSignalHub() *directSignalHub {
	return &directSignalHub{
		hosts:    make(map[uuid.UUID]*directSignalPeer),
		attempts: make(map[uuid.UUID]*directSignalAttempt),
		now:      time.Now,
	}
}

func (s *Server) handleDirectSignalHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DirectSignalEnabled {
		http.NotFound(w, r)
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok || user.ID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.allowAuthenticatedRequest(w, r) {
		return
	}
	key := requestLimitKey(r)
	if !s.conns.acquire(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.release(key)

	conn, err := websocket.Accept(w, r, s.acceptOptionsWithAuthSubprotocol(r))
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "")
	conn.SetReadLimit(directSignalReadLimit)
	s.handleDirectSignal(r.Context(), conn, user.ID)
}

func (s *Server) handleDirectSignal(ctx context.Context, conn *websocket.Conn, ownerUserID string) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	peer := &directSignalPeer{
		ownerUserID: ownerUserID,
		out:         make(chan directSignalMessage, directSignalQueueDepth),
		cancel:      cancel,
	}
	defer s.direct.unregister(peer)

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(directSignalPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case message := <-peer.out:
				writeCtx, writeCancel := context.WithTimeout(connCtx, directSignalWriteWait)
				data, _ := json.Marshal(message)
				err := conn.Write(writeCtx, websocket.MessageText, data)
				writeCancel()
				if err != nil {
					cancel()
					return
				}
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(connCtx, directSignalWriteWait)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	helloCtx, helloCancel := context.WithTimeout(connCtx, directSignalHelloTimeout)
	message, err := readDirectSignal(helloCtx, conn)
	helloCancel()
	if err != nil || message.Kind != "hello" || message.Version != directSignalVersion {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected direct hello")
		cancel()
		<-writerDone
		return
	}
	switch message.Role {
	case "client":
		if !validDirectIdentifier(message.ClientInstanceID) {
			_ = conn.Close(websocket.StatusPolicyViolation, "bad client instance id")
			cancel()
			<-writerDone
			return
		}
		peer.role = "client"
		peer.clientInstanceID = message.ClientInstanceID
	case "host":
		peer.role = "host"
		if err := s.registerDirectHost(peer, message.HostID, message.SessionIDs); err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
			cancel()
			<-writerDone
			return
		}
	default:
		_ = conn.Close(websocket.StatusPolicyViolation, "bad direct role")
		cancel()
		<-writerDone
		return
	}
	peer.send(directSignalMessage{Version: directSignalVersion, Kind: "hello_ok"})

	for connCtx.Err() == nil {
		message, err = readDirectSignal(connCtx, conn)
		if err != nil {
			break
		}
		if message.Version != directSignalVersion {
			peer.send(directSignalError(message.RequestID, "unsupported_version", "unsupported signaling version"))
			continue
		}
		switch message.Kind {
		case "host_register":
			if peer.role != "host" {
				peer.send(directSignalError(message.RequestID, "invalid_role", "host registration requires host role"))
				continue
			}
			if err := s.registerDirectHost(peer, message.HostID, message.SessionIDs); err != nil {
				peer.send(directSignalError(message.RequestID, "invalid_registration", err.Error()))
				continue
			}
			peer.send(directSignalMessage{Version: directSignalVersion, Kind: "host_registered", RequestID: message.RequestID})
		case "direct_request":
			if peer.role != "client" {
				peer.send(directSignalError(message.RequestID, "invalid_role", "direct request requires client role"))
				continue
			}
			if err := s.beginDirectAttempt(peer, message); err != nil {
				peer.send(directSignalError(message.RequestID, directSignalErrorCode(err), err.Error()))
			}
		case "signal", "cancel", "consumed":
			if err := s.direct.route(peer, message); err != nil {
				peer.send(directSignalError(message.RequestID, directSignalErrorCode(err), err.Error()))
			}
		default:
			peer.send(directSignalError(message.RequestID, "unknown_kind", "unknown signaling message"))
		}
	}
	cancel()
	<-writerDone
}

func readDirectSignal(ctx context.Context, conn *websocket.Conn) (directSignalMessage, error) {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return directSignalMessage{}, err
	}
	if kind != websocket.MessageText || len(data) > directSignalReadLimit {
		return directSignalMessage{}, errors.New("direct signaling requires bounded text messages")
	}
	var message directSignalMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return directSignalMessage{}, err
	}
	return message, nil
}

func (s *Server) registerDirectHost(peer *directSignalPeer, hostID string, rawSessionIDs []string) error {
	if !validDirectIdentifier(hostID) || len(rawSessionIDs) > directMaxSessionsPerHost {
		return errors.New("invalid host registration")
	}
	if _, ok := s.sessionCreateRoutes().lookupHost(hostID, peer.ownerUserID); !ok {
		return errors.New("host uplink is not active")
	}
	sessionIDs := make([]uuid.UUID, 0, len(rawSessionIDs))
	seen := make(map[uuid.UUID]struct{}, len(rawSessionIDs))
	for _, raw := range rawSessionIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return errors.New("invalid session id")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		sess, ok := s.registry.Get(id)
		if !ok || sess.OwnerUserID != peer.ownerUserID || sess.Info().HostID != hostID {
			return errors.New("session is not owned by this host")
		}
		seen[id] = struct{}{}
		sessionIDs = append(sessionIDs, id)
	}
	s.direct.registerHost(peer, hostID, sessionIDs)
	return nil
}

func (s *Server) beginDirectAttempt(client *directSignalPeer, request directSignalMessage) error {
	if !validDirectIdentifier(request.RequestID) {
		return directSignalCodeError{"invalid_request", "invalid direct request id"}
	}
	sessionID, err := uuid.Parse(request.SessionID)
	if err != nil {
		return directSignalCodeError{"invalid_request", "invalid session id"}
	}
	sess, ok := s.registry.Get(sessionID)
	if !ok || sess.OwnerUserID != client.ownerUserID {
		return directSignalCodeError{"direct_unauthorized", "session unavailable"}
	}
	info := sess.Info()
	if _, ok := s.sessionCreateRoutes().lookupHost(info.HostID, client.ownerUserID); !ok {
		return directSignalCodeError{"host_offline", "host uplink is not active"}
	}
	ticket := make([]byte, directTicketBytes)
	if _, err := rand.Read(ticket); err != nil {
		return directSignalCodeError{"internal_error", "ticket generation failed"}
	}
	permission := directPermissionString(parseRemotePermission(info.RemotePermission))
	return s.direct.begin(client, sessionID, info.HostID, permission, request.RequestID, request.SinceSeq, ticket)
}

func (h *directSignalHub) registerHost(peer *directSignalPeer, hostID string, sessionIDs []uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	peer.hostID = hostID
	for sessionID, existing := range h.hosts {
		if existing == peer {
			delete(h.hosts, sessionID)
		}
	}
	for _, sessionID := range sessionIDs {
		h.hosts[sessionID] = peer
	}
}

func (h *directSignalHub) unregister(peer *directSignalPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sessionID, existing := range h.hosts {
		if existing == peer {
			delete(h.hosts, sessionID)
		}
	}
	for id, attempt := range h.attempts {
		if attempt.client == peer || attempt.host == peer {
			other := attempt.client
			if other == peer {
				other = attempt.host
			}
			other.send(directSignalMessage{Version: directSignalVersion, Kind: "cancel", AttemptID: id.String(), Code: "peer_disconnected"})
			delete(h.attempts, id)
		}
	}
}

func (h *directSignalHub) begin(client *directSignalPeer, sessionID uuid.UUID, hostID, permission, requestID string, sinceSeq uint64, ticket []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reapExpiredLocked()
	host := h.hosts[sessionID]
	if host == nil || host.ownerUserID != client.ownerUserID || host.hostID != hostID {
		return directSignalCodeError{"host_offline", "direct host is not registered"}
	}
	if len(h.attempts) >= directMaxAttempts || h.attemptCountLocked(client) >= directMaxAttemptsPerPeer || h.attemptCountLocked(host) >= directMaxAttemptsPerPeer {
		return directSignalCodeError{"attempt_limit", "too many direct attempts"}
	}
	id := uuid.New()
	expiresAt := h.now().Add(directTicketTTL)
	attempt := &directSignalAttempt{
		id:               id,
		requestID:        requestID,
		sessionID:        sessionID,
		ticketHash:       sha256.Sum256(ticket),
		ownerUserID:      client.ownerUserID,
		hostID:           hostID,
		clientInstanceID: client.clientInstanceID,
		permission:       permission,
		expiresAt:        expiresAt,
		client:           client,
		host:             host,
	}
	ticketText := base64.RawURLEncoding.EncodeToString(ticket)
	base := directSignalMessage{
		Version:          directSignalVersion,
		Kind:             "direct_attempt",
		RequestID:        requestID,
		SessionID:        sessionID.String(),
		SinceSeq:         sinceSeq,
		AttemptID:        id.String(),
		Ticket:           ticketText,
		UserID:           client.ownerUserID,
		HostID:           hostID,
		ClientInstanceID: client.clientInstanceID,
		Permission:       permission,
		ExpiresAtUnixM:   expiresAt.UnixMilli(),
	}
	if !host.send(base) {
		return directSignalCodeError{"host_offline", "direct host signaling is unavailable"}
	}
	if !client.send(base) {
		host.send(directSignalMessage{Version: directSignalVersion, Kind: "cancel", AttemptID: id.String(), Code: "client_disconnected"})
		return directSignalCodeError{"client_unavailable", "direct client signaling is unavailable"}
	}
	h.attempts[id] = attempt
	return nil
}

func (h *directSignalHub) route(sender *directSignalPeer, message directSignalMessage) error {
	id, err := uuid.Parse(message.AttemptID)
	if err != nil {
		return directSignalCodeError{"invalid_request", "invalid attempt id"}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reapExpiredLocked()
	attempt := h.attempts[id]
	if attempt == nil || (attempt.client != sender && attempt.host != sender) {
		return directSignalCodeError{"attempt_unavailable", "direct attempt unavailable"}
	}
	receiver := attempt.host
	isClient := sender == attempt.client
	if !isClient {
		receiver = attempt.client
	}
	switch message.Kind {
	case "cancel":
		code := message.Code
		if code == "" {
			code = "peer_cancelled"
		}
		if !validDirectIdentifier(code) {
			return directSignalCodeError{"invalid_request", "invalid direct cancel code"}
		}
		receiver.send(directSignalMessage{Version: directSignalVersion, Kind: "cancel", AttemptID: id.String(), Code: code})
		delete(h.attempts, id)
		return nil
	case "consumed":
		if isClient {
			return directSignalCodeError{"invalid_role", "only host may consume a direct ticket"}
		}
		receiver.send(directSignalMessage{Version: directSignalVersion, Kind: "consumed", AttemptID: id.String()})
		delete(h.attempts, id)
		return nil
	case "signal":
		if err := validateDirectSignalMessage(attempt, isClient, message); err != nil {
			return err
		}
		routed := directSignalMessage{
			Version:    directSignalVersion,
			Kind:       "signal",
			AttemptID:  id.String(),
			SignalType: message.SignalType,
			Payload:    message.Payload,
		}
		if !receiver.send(routed) {
			delete(h.attempts, id)
			return directSignalCodeError{"peer_disconnected", "signaling peer unavailable"}
		}
		return nil
	default:
		return directSignalCodeError{"unknown_kind", "unknown attempt message"}
	}
}

func validateDirectSignalMessage(attempt *directSignalAttempt, fromClient bool, message directSignalMessage) error {
	switch message.SignalType {
	case "offer":
		if !fromClient || attempt.offerSeen || len(message.Payload) == 0 || len(message.Payload) > directMaxSDPBytes {
			return directSignalCodeError{"invalid_signal", "invalid direct offer"}
		}
		attempt.offerSeen = true
	case "answer":
		if fromClient || !attempt.offerSeen || attempt.answerSeen || len(message.Payload) == 0 || len(message.Payload) > directMaxSDPBytes {
			return directSignalCodeError{"invalid_signal", "invalid direct answer"}
		}
		attempt.answerSeen = true
	case "ice_candidate":
		if len(message.Payload) == 0 || len(message.Payload) > directMaxICEBytes {
			return directSignalCodeError{"invalid_signal", "invalid ICE candidate"}
		}
		if fromClient {
			attempt.clientICE++
			if attempt.clientICE > directMaxICEPerSide {
				return directSignalCodeError{"signal_limit", "too many ICE candidates"}
			}
		} else {
			attempt.hostICE++
			if attempt.hostICE > directMaxICEPerSide {
				return directSignalCodeError{"signal_limit", "too many ICE candidates"}
			}
		}
	case "ice_end":
		if message.Payload != "" {
			return directSignalCodeError{"invalid_signal", "ICE end payload must be empty"}
		}
		if fromClient {
			if attempt.clientICEEnd {
				return directSignalCodeError{"invalid_signal", "duplicate ICE end"}
			}
			attempt.clientICEEnd = true
		} else {
			if attempt.hostICEEnd {
				return directSignalCodeError{"invalid_signal", "duplicate ICE end"}
			}
			attempt.hostICEEnd = true
		}
	default:
		return directSignalCodeError{"invalid_signal", "unknown signaling payload"}
	}
	return nil
}

func (h *directSignalHub) attemptCountLocked(peer *directSignalPeer) int {
	count := 0
	for _, attempt := range h.attempts {
		if attempt.client == peer || attempt.host == peer {
			count++
		}
	}
	return count
}

func (h *directSignalHub) reapExpiredLocked() {
	now := h.now()
	for id, attempt := range h.attempts {
		if !now.Before(attempt.expiresAt) {
			attempt.client.send(directSignalMessage{Version: directSignalVersion, Kind: "cancel", AttemptID: id.String(), Code: "ticket_expired"})
			attempt.host.send(directSignalMessage{Version: directSignalVersion, Kind: "cancel", AttemptID: id.String(), Code: "ticket_expired"})
			delete(h.attempts, id)
		}
	}
}

func validDirectIdentifier(value string) bool {
	return value != "" && len(value) <= directMaxIdentifierBytes && strings.TrimSpace(value) == value
}

func directPermissionString(permission remotePermission) string {
	switch permission {
	case permFull:
		return proto.RemotePermissionFull
	case permControl:
		return proto.RemotePermissionControl
	default:
		return proto.RemotePermissionView
	}
}

type directSignalCodeError struct {
	code    string
	message string
}

func (e directSignalCodeError) Error() string { return e.message }

func directSignalErrorCode(err error) string {
	var coded directSignalCodeError
	if errors.As(err, &coded) {
		return coded.code
	}
	return "internal_error"
}

func directSignalError(requestID, code, message string) directSignalMessage {
	return directSignalMessage{Version: directSignalVersion, Kind: "error", RequestID: requestID, Code: code, Message: message}
}
