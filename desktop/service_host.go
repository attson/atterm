package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/serviceproxy"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	serviceHostDialTimeout = 10 * time.Second
	serviceTargetTimeout   = 5 * time.Second
	serviceHostWriteWait   = 10 * time.Second
	serviceHostQueueDepth  = 128
)

// serviceHostManager owns Remote Web Preview leases for one uplink
// connection. It never subscribes to a PTY session: the session id is used
// only for permission routing and to open the sealed loopback port.
type serviceHostManager struct {
	ctx           context.Context
	relayURL      string
	token         string
	allowInsecure bool
	accountKey    func() []byte
	out           chan<- proto.Frame
	permission    string

	mu       sync.Mutex
	services map[uuid.UUID]*serviceHost
	opening  map[uuid.UUID]struct{}
}

type serviceHost struct {
	id         uuid.UUID
	sessionID  uuid.UUID
	targetAddr string
	ctx        context.Context
	cancel     context.CancelFunc
	ws         *websocket.Conn
	codec      *serviceproxy.Codec
	send       chan serviceHostMessage

	mu          sync.Mutex
	connections map[uint32]net.Conn
	closeOnce   sync.Once
}

type serviceHostMessage struct {
	kind serviceproxy.Kind
	id   uint32
	data []byte
}

type serviceRegistration struct {
	ServiceID string `json:"service_id"`
	Ticket    string `json:"ticket"`
}

type serviceRegistrationAck struct {
	OK bool `json:"ok"`
}

func newServiceHostManager(ctx context.Context, relayURL, token string, allowInsecure bool, accountKey func() []byte, out chan<- proto.Frame, permission string) *serviceHostManager {
	return &serviceHostManager{
		ctx:           ctx,
		relayURL:      relayURL,
		token:         token,
		allowInsecure: allowInsecure,
		accountKey:    accountKey,
		out:           out,
		permission:    permission,
		services:      make(map[uuid.UUID]*serviceHost),
		opening:       make(map[uuid.UUID]struct{}),
	}
}

func (m *serviceHostManager) close() {
	m.mu.Lock()
	services := make([]*serviceHost, 0, len(m.services))
	for _, service := range m.services {
		services = append(services, service)
	}
	m.services = make(map[uuid.UUID]*serviceHost)
	m.opening = make(map[uuid.UUID]struct{})
	m.mu.Unlock()
	for _, service := range services {
		service.close()
	}
}

func (m *serviceHostManager) submit(frame proto.Frame) {
	go m.open(frame)
}

func (m *serviceHostManager) open(frame proto.Frame) {
	var req proto.ServiceOpenPayload
	if err := json.Unmarshal(frame.Payload, &req); err != nil || req.RequestID == "" || req.ServiceID == "" {
		return
	}
	reply := proto.ServiceOpenedPayload{RequestID: req.RequestID, ServiceID: req.ServiceID}
	fail := func(code string) {
		reply.OK = false
		reply.Error = code
		m.sendOpened(frame.SessionID, reply)
	}

	// Deliberately check the raw configured value. An empty legacy value is
	// normalized to full for ordinary terminal control, but exposing a TCP
	// listener is a new capability and therefore fail-closed.
	if m.permission != proto.RemotePermissionFull {
		fail("permission_denied")
		return
	}
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil || serviceID == uuid.Nil || req.HostTicket == "" || len(req.Sealed) == 0 {
		fail("invalid_request")
		return
	}
	if m.accountKey == nil {
		fail("e2ee_required")
		return
	}
	accountKey := m.accountKey()
	if len(accountKey) < e2eecrypto.SessionKeySize {
		fail("e2ee_required")
		return
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, frame.SessionID)
	if err != nil {
		fail("e2ee_required")
		return
	}
	plain, err := e2eecrypto.OpenUnsequenced(sessionKey, frame.SessionID, byte(proto.TypeServiceOpen), req.Sealed)
	if err != nil {
		fail("invalid_request")
		return
	}
	var fields proto.SealedServiceOpenFields
	if err := json.Unmarshal(plain, &fields); err != nil || fields.Port == 0 || fields.Scheme != "http" {
		fail("invalid_request")
		return
	}
	keys, err := e2eecrypto.DeriveServiceKeys(accountKey, serviceID)
	if err != nil {
		fail("e2ee_required")
		return
	}
	codec, err := serviceproxy.NewCodec(serviceID, keys.HostToClient, keys.ClientToHost)
	if err != nil {
		fail("e2ee_required")
		return
	}

	m.mu.Lock()
	_, active := m.services[serviceID]
	_, opening := m.opening[serviceID]
	if active || opening {
		m.mu.Unlock()
		fail("duplicate_service_id")
		return
	}
	m.opening[serviceID] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.opening, serviceID)
		m.mu.Unlock()
	}()

	host, err := m.connect(serviceID, frame.SessionID, fields.Port, req.HostTicket, codec)
	if err != nil {
		logWarn("service-host", "open service=%s failed: %v", serviceID, err)
		fail("upstream_unavailable")
		return
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		host.close()
		fail("upstream_unavailable")
		return
	}
	if _, exists := m.services[serviceID]; exists {
		m.mu.Unlock()
		host.close()
		fail("duplicate_service_id")
		return
	}
	m.services[serviceID] = host
	m.mu.Unlock()

	reply.OK = true
	m.sendOpened(frame.SessionID, reply)
	go func() {
		host.run()
		m.mu.Lock()
		if m.services[serviceID] == host {
			delete(m.services, serviceID)
		}
		m.mu.Unlock()
	}()
}

func (m *serviceHostManager) connect(serviceID, sessionID uuid.UUID, port uint16, ticket string, codec *serviceproxy.Codec) (*serviceHost, error) {
	dialCtx, cancel := context.WithTimeout(m.ctx, serviceHostDialTimeout)
	defer cancel()
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if m.token != "" {
		opts.HTTPHeader.Set("Authorization", "Bearer "+m.token)
	}
	if m.allowInsecure {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	conn, _, err := websocket.Dial(dialCtx, m.relayURL+"/service-host", opts)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(serviceproxy.MaxPacketSize)
	reg, err := json.Marshal(serviceRegistration{ServiceID: serviceID.String(), Ticket: ticket})
	if err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	wctx, wc := context.WithTimeout(m.ctx, serviceHostWriteWait)
	err = conn.Write(wctx, websocket.MessageBinary, reg)
	wc()
	if err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	ackCtx, ackCancel := context.WithTimeout(m.ctx, serviceHostWriteWait)
	_, ackRaw, err := conn.Read(ackCtx)
	ackCancel()
	var ack serviceRegistrationAck
	if err != nil || json.Unmarshal(ackRaw, &ack) != nil || !ack.OK {
		_ = conn.CloseNow()
		if err == nil {
			err = errors.New("service registration rejected")
		}
		return nil, err
	}
	ctx, cancelHost := context.WithCancel(m.ctx)
	return &serviceHost{
		id:          serviceID,
		sessionID:   sessionID,
		targetAddr:  net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)),
		ctx:         ctx,
		cancel:      cancelHost,
		ws:          conn,
		codec:       codec,
		send:        make(chan serviceHostMessage, serviceHostQueueDepth),
		connections: make(map[uint32]net.Conn),
	}, nil
}

func (m *serviceHostManager) sendOpened(sessionID uuid.UUID, payload proto.ServiceOpenedPayload) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case m.out <- proto.Frame{Type: proto.TypeServiceOpened, SessionID: sessionID, Payload: raw}:
	case <-m.ctx.Done():
	}
}

func (h *serviceHost) run() {
	defer h.close()
	errCh := make(chan error, 2)
	go func() { errCh <- h.writeLoop() }()
	go func() { errCh <- h.readLoop() }()
	select {
	case <-h.ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			logDebug("service-host", "service=%s closed: %v", h.id, err)
		}
	}
}

func (h *serviceHost) writeLoop() error {
	for {
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case msg := <-h.send:
			packet, err := h.codec.Seal(msg.kind, msg.id, msg.data)
			if err != nil {
				return err
			}
			wctx, cancel := context.WithTimeout(h.ctx, serviceHostWriteWait)
			err = h.ws.Write(wctx, websocket.MessageBinary, packet)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func (h *serviceHost) readLoop() error {
	for {
		mt, packet, err := h.ws.Read(h.ctx)
		if err != nil {
			return err
		}
		if mt != websocket.MessageBinary {
			return errors.New("service data must be binary")
		}
		header, plain, err := h.codec.Open(packet)
		if err != nil {
			return err
		}
		switch header.Kind {
		case serviceproxy.KindOpen:
			if len(plain) != 0 {
				return errors.New("open packet has payload")
			}
			if err := h.openTarget(header.Connection); err != nil {
				h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: header.Connection})
			}
		case serviceproxy.KindData:
			conn := h.connection(header.Connection)
			if conn == nil {
				return errors.New("data for unopened target")
			}
			if _, err := conn.Write(plain); err != nil {
				h.closeTarget(header.Connection)
				h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: header.Connection})
			}
		case serviceproxy.KindClose:
			if len(plain) != 0 {
				return errors.New("close packet has payload")
			}
			h.closeTarget(header.Connection)
		}
	}
}

func (h *serviceHost) openTarget(id uint32) error {
	h.mu.Lock()
	if _, exists := h.connections[id]; exists || len(h.connections) >= 16 {
		h.mu.Unlock()
		return errors.New("target connection limit")
	}
	h.mu.Unlock()
	dialer := net.Dialer{Timeout: serviceTargetTimeout}
	conn, err := dialer.DialContext(h.ctx, "tcp", h.targetAddr)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()
		_ = conn.Close()
		return h.ctx.Err()
	}
	if _, exists := h.connections[id]; exists || len(h.connections) >= 16 {
		h.mu.Unlock()
		_ = conn.Close()
		return errors.New("target connection limit")
	}
	h.connections[id] = conn
	h.mu.Unlock()
	go h.readTarget(id, conn)
	return nil
}

func (h *serviceHost) readTarget(id uint32, conn net.Conn) {
	buf := make([]byte, serviceproxy.MaxPlaintextSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if !h.enqueue(serviceHostMessage{kind: serviceproxy.KindData, id: id, data: data}) {
				return
			}
		}
		if err != nil {
			h.closeTargetIf(id, conn)
			h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: id})
			return
		}
	}
}

func (h *serviceHost) enqueue(msg serviceHostMessage) bool {
	select {
	case h.send <- msg:
		return true
	case <-h.ctx.Done():
		return false
	default:
		// Packets are never silently dropped. Saturation closes the lease so
		// neither endpoint can mistake a gapped byte stream for valid TCP.
		h.cancel()
		return false
	}
}

func (h *serviceHost) connection(id uint32) net.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connections[id]
}

func (h *serviceHost) closeTarget(id uint32) {
	h.mu.Lock()
	conn := h.connections[id]
	delete(h.connections, id)
	h.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (h *serviceHost) closeTargetIf(id uint32, want net.Conn) {
	h.mu.Lock()
	conn := h.connections[id]
	if conn == want {
		delete(h.connections, id)
	}
	h.mu.Unlock()
	if conn == want {
		_ = conn.Close()
	}
}

func (h *serviceHost) close() {
	h.closeOnce.Do(func() {
		h.cancel()
		_ = h.ws.CloseNow()
		h.mu.Lock()
		connections := make([]net.Conn, 0, len(h.connections))
		for _, conn := range h.connections {
			connections = append(connections, conn)
		}
		h.connections = make(map[uint32]net.Conn)
		h.mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
}
