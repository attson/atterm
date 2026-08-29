package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/serviceproxy"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	servicePendingTTL      = 30 * time.Second
	serviceIdleTTL         = 10 * time.Minute
	serviceSweepPeriod     = 15 * time.Second
	serviceWriteWait       = 10 * time.Second
	serviceRegisterLimit   = 4096
	serviceMaxPerUser      = 4
	serviceMaxConnections  = 16
	serviceMaxForwardBytes = 512 << 20
)

type serviceRole uint8

const (
	serviceRoleClient serviceRole = iota + 1
	serviceRoleHost
)

type serviceRegistration struct {
	ServiceID string `json:"service_id"`
	Ticket    string `json:"ticket"`
}

type serviceRegistrationAck struct {
	OK bool `json:"ok"`
}

type serviceLease struct {
	id           uuid.UUID
	sessionID    uuid.UUID
	ownerUserID  string
	requestID    string
	requester    chan<- proto.Frame
	uplink       chan<- proto.Frame
	onOverflow   func()
	hostTicket   [32]byte
	clientTicket [32]byte
	createdAt    time.Time
	lastActive   time.Time
	ready        bool

	clientConn  *websocket.Conn
	hostConn    *websocket.Conn
	clientUsed  bool
	hostUsed    bool
	paired      chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	bytes       int64
	connections map[uint32]struct{}
}

type serviceHub struct {
	mu       sync.Mutex
	services map[uuid.UUID]*serviceLease
	requests map[string]*serviceLease
	sessions map[uuid.UUID]serviceUplinkRoute
}

type serviceUplinkRoute struct {
	owner string
	out   chan<- proto.Frame
}

func newServiceHub() *serviceHub {
	h := &serviceHub{
		services: make(map[uuid.UUID]*serviceLease),
		requests: make(map[string]*serviceLease),
		sessions: make(map[uuid.UUID]serviceUplinkRoute),
	}
	go h.sweepLoop()
	return h
}

func randomServiceTicket() ([32]byte, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return raw, "", err
	}
	return raw, base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func parseServiceTicket(ticket string) ([32]byte, error) {
	var out [32]byte
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(raw) != len(out) {
		return out, errors.New("invalid ticket")
	}
	copy(out[:], raw)
	return out, nil
}

// begin reserves a lease before SERVICE_OPEN is forwarded. This lets the
// desktop authenticate /service-host immediately, before its OPENED answer is
// routed back to the client.
func (h *serviceHub) begin(owner string, sessionID uuid.UUID, req proto.ServiceOpenPayload, requester chan<- proto.Frame, onOverflow func()) (proto.ServiceOpenPayload, string, bool) {
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil || req.RequestID == "" || requester == nil || len(req.Sealed) == 0 {
		return req, "invalid_request", false
	}
	hostRaw, hostText, err := randomServiceTicket()
	if err != nil {
		return req, "upstream_unavailable", false
	}
	clientRaw, clientText, err := randomServiceTicket()
	if err != nil {
		return req, "upstream_unavailable", false
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	uplinkRoute, ok := h.sessions[sessionID]
	if !ok || uplinkRoute.owner != owner || uplinkRoute.out == nil {
		return req, "upstream_unavailable", false
	}
	if _, exists := h.services[serviceID]; exists {
		return req, "duplicate_service_id", false
	}
	if _, exists := h.requests[req.RequestID]; exists {
		return req, "duplicate_request_id", false
	}
	count := 0
	for _, existing := range h.services {
		if existing.ownerUserID == owner {
			count++
		}
	}
	if count >= serviceMaxPerUser {
		return req, "service_limit", false
	}
	lease := &serviceLease{
		id:           serviceID,
		sessionID:    sessionID,
		ownerUserID:  owner,
		requestID:    req.RequestID,
		requester:    requester,
		uplink:       uplinkRoute.out,
		onOverflow:   onOverflow,
		hostTicket:   hostRaw,
		clientTicket: clientRaw,
		createdAt:    now,
		lastActive:   now,
		paired:       make(chan struct{}),
		done:         make(chan struct{}),
		connections:  make(map[uint32]struct{}),
	}
	h.services[serviceID] = lease
	h.requests[req.RequestID] = lease
	req.HostTicket = hostText
	return req, clientText, true
}

// finish validates that OPENED came from the same uplink the request was sent
// to, injects the requester-only client ticket, and returns its targeted route.
func (h *serviceHub) finish(f proto.Frame, fromUplink chan<- proto.Frame) (proto.Frame, chan<- proto.Frame, func(), bool) {
	var resp proto.ServiceOpenedPayload
	if err := json.Unmarshal(f.Payload, &resp); err != nil || resp.RequestID == "" || resp.ServiceID == "" {
		return proto.Frame{}, nil, nil, false
	}
	serviceID, err := uuid.Parse(resp.ServiceID)
	if err != nil {
		return proto.Frame{}, nil, nil, false
	}
	h.mu.Lock()
	lease := h.requests[resp.RequestID]
	if lease == nil || lease.id != serviceID || lease.uplink != fromUplink {
		h.mu.Unlock()
		return proto.Frame{}, nil, nil, false
	}
	delete(h.requests, resp.RequestID)
	if resp.OK {
		lease.ready = true
		lease.lastActive = time.Now()
		resp.ClientTicket = base64.RawURLEncoding.EncodeToString(lease.clientTicket[:])
	} else {
		delete(h.services, lease.id)
	}
	out := lease.requester
	onOverflow := lease.onOverflow
	h.mu.Unlock()
	if !resp.OK {
		lease.close()
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return proto.Frame{}, nil, nil, false
	}
	f.Payload = payload
	return f, out, onOverflow, true
}

func (h *serviceHub) closeByRequester(serviceID uuid.UUID, requester chan<- proto.Frame) bool {
	h.mu.Lock()
	lease := h.services[serviceID]
	if lease == nil || lease.requester != requester {
		h.mu.Unlock()
		return false
	}
	h.removeLocked(lease)
	h.mu.Unlock()
	lease.close()
	return true
}

func (h *serviceHub) closeRequester(requester chan<- proto.Frame) {
	h.closeMatching(func(l *serviceLease) bool { return l.requester == requester })
}

func (h *serviceHub) closeUplink(uplink chan<- proto.Frame) {
	h.mu.Lock()
	for id, route := range h.sessions {
		if route.out == uplink {
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	h.closeMatching(func(l *serviceLease) bool { return l.uplink == uplink })
}

func (h *serviceHub) closeSession(sessionID uuid.UUID) {
	h.mu.Lock()
	delete(h.sessions, sessionID)
	h.mu.Unlock()
	h.closeMatching(func(l *serviceLease) bool { return l.sessionID == sessionID })
}

func (h *serviceHub) registerSession(sessionID uuid.UUID, owner string, uplink chan<- proto.Frame) {
	if sessionID == uuid.Nil || uplink == nil {
		return
	}
	h.mu.Lock()
	h.sessions[sessionID] = serviceUplinkRoute{owner: owner, out: uplink}
	h.mu.Unlock()
}

func (h *serviceHub) closeLease(lease *serviceLease) {
	if lease == nil {
		return
	}
	h.mu.Lock()
	if h.services[lease.id] == lease {
		h.removeLocked(lease)
	}
	h.mu.Unlock()
	lease.close()
}

func (h *serviceHub) closeMatching(match func(*serviceLease) bool) {
	h.mu.Lock()
	var closing []*serviceLease
	for _, lease := range h.services {
		if match(lease) {
			h.removeLocked(lease)
			closing = append(closing, lease)
		}
	}
	h.mu.Unlock()
	for _, lease := range closing {
		lease.close()
	}
}

func (h *serviceHub) removeLocked(lease *serviceLease) {
	delete(h.services, lease.id)
	delete(h.requests, lease.requestID)
}

func (l *serviceLease) close() {
	l.closeOnce.Do(func() {
		close(l.done)
		if l.clientConn != nil {
			_ = l.clientConn.CloseNow()
		}
		if l.hostConn != nil {
			_ = l.hostConn.CloseNow()
		}
	})
}

func ticketMatches(want [32]byte, got string) bool {
	parsed, err := parseServiceTicket(got)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(want[:], parsed[:]) == 1
}

func (h *serviceHub) attach(owner string, role serviceRole, reg serviceRegistration, conn *websocket.Conn) (*serviceLease, error) {
	id, err := uuid.Parse(reg.ServiceID)
	if err != nil || reg.Ticket == "" {
		return nil, errors.New("invalid registration")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	lease := h.services[id]
	if lease == nil || lease.ownerUserID != owner {
		return nil, errors.New("unknown service")
	}
	switch role {
	case serviceRoleHost:
		if lease.hostUsed || !ticketMatches(lease.hostTicket, reg.Ticket) {
			return nil, errors.New("invalid host ticket")
		}
		lease.hostUsed = true
		lease.hostConn = conn
	case serviceRoleClient:
		if !lease.ready || lease.clientUsed || !ticketMatches(lease.clientTicket, reg.Ticket) {
			return nil, errors.New("invalid client ticket")
		}
		lease.clientUsed = true
		lease.clientConn = conn
	default:
		return nil, errors.New("invalid role")
	}
	lease.lastActive = time.Now()
	if lease.clientConn != nil && lease.hostConn != nil {
		select {
		case <-lease.paired:
		default:
			close(lease.paired)
		}
	}
	return lease, nil
}

func (h *serviceHub) observePacket(lease *serviceLease, role serviceRole, packet []byte) error {
	header, err := serviceproxy.ParseHeader(packet)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.services[lease.id] != lease {
		return errors.New("service closed")
	}
	lease.bytes += int64(len(packet))
	if lease.bytes > serviceMaxForwardBytes {
		return errors.New("service byte limit exceeded")
	}
	switch header.Kind {
	case serviceproxy.KindOpen:
		if role != serviceRoleClient {
			return errors.New("only the client may open connections")
		}
		if _, exists := lease.connections[header.Connection]; !exists {
			if len(lease.connections) >= serviceMaxConnections {
				return errors.New("service connection limit exceeded")
			}
			lease.connections[header.Connection] = struct{}{}
		}
	case serviceproxy.KindClose:
		delete(lease.connections, header.Connection)
	case serviceproxy.KindData:
		if _, exists := lease.connections[header.Connection]; !exists {
			return errors.New("data for unopened connection")
		}
	}
	lease.lastActive = time.Now()
	return nil
}

func (h *serviceHub) sweepLoop() {
	ticker := time.NewTicker(serviceSweepPeriod)
	defer ticker.Stop()
	for now := range ticker.C {
		h.mu.Lock()
		closing := h.reapExpiredLocked(now)
		h.mu.Unlock()
		for _, lease := range closing {
			lease.close()
		}
	}
}

func (h *serviceHub) reapExpiredLocked(now time.Time) []*serviceLease {
	var closing []*serviceLease
	for _, lease := range h.services {
		ttl := serviceIdleTTL
		if !lease.ready || lease.clientConn == nil || lease.hostConn == nil {
			ttl = servicePendingTTL
		}
		if now.Sub(lease.lastActive) >= ttl {
			h.removeLocked(lease)
			closing = append(closing, lease)
		}
	}
	return closing
}

func (s *Server) handleServiceClientHTTP(w http.ResponseWriter, r *http.Request) {
	s.handleServiceHTTP(w, r, serviceRoleClient)
}

func (s *Server) handleServiceHostHTTP(w http.ResponseWriter, r *http.Request) {
	s.handleServiceHTTP(w, r, serviceRoleHost)
}

func (s *Server) handleServiceHTTP(w http.ResponseWriter, r *http.Request, role serviceRole) {
	user, ok := UserFromContext(r.Context())
	if !ok || !s.allowAuthenticatedRequest(w, r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	conn.SetReadLimit(serviceproxy.MaxPacketSize)
	defer conn.Close(websocket.StatusInternalError, "")

	regCtx, cancel := context.WithTimeout(r.Context(), serviceWriteWait)
	mt, raw, err := conn.Read(regCtx)
	cancel()
	if err != nil || len(raw) > serviceRegisterLimit || (mt != websocket.MessageBinary && mt != websocket.MessageText) {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad service registration")
		return
	}
	var reg serviceRegistration
	if err := json.Unmarshal(raw, &reg); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "bad service registration")
		return
	}
	lease, err := s.services.attach(user.ID, role, reg, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "service registration rejected")
		return
	}
	ack, _ := json.Marshal(serviceRegistrationAck{OK: true})
	ackCtx, ackCancel := context.WithTimeout(r.Context(), serviceWriteWait)
	err = conn.Write(ackCtx, websocket.MessageBinary, ack)
	ackCancel()
	if err != nil {
		s.services.closeLease(lease)
		return
	}

	select {
	case <-r.Context().Done():
		s.services.closeLease(lease)
		return
	case <-lease.done:
		return
	case <-lease.paired:
	}
	if err := s.forwardService(r.Context(), lease, role, conn); err != nil && !errors.Is(err, context.Canceled) {
		logging.Debug("relay-service", "service %s closed: %v", lease.id, err)
	}
	s.services.closeLease(lease)
}

func (s *Server) forwardService(ctx context.Context, lease *serviceLease, role serviceRole, src *websocket.Conn) error {
	var dst *websocket.Conn
	if role == serviceRoleClient {
		dst = lease.hostConn
	} else {
		dst = lease.clientConn
	}
	if dst == nil {
		return errors.New("service peer missing")
	}
	for {
		mt, packet, err := src.Read(ctx)
		if err != nil {
			return err
		}
		if mt != websocket.MessageBinary {
			return errors.New("service data must be binary")
		}
		if err := s.services.observePacket(lease, role, packet); err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, serviceWriteWait)
		err = dst.Write(wctx, websocket.MessageBinary, packet)
		cancel()
		if err != nil {
			return err
		}
	}
}

func serviceTicketDigest(ticket [32]byte) string {
	// Test/debug helper intentionally returns only a short digest; raw tickets
	// must never appear in logs.
	sum := sha256.Sum256(ticket[:])
	return fmt.Sprintf("%x", sum[:4])
}
