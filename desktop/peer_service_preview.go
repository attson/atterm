package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/serviceproxy"
	"github.com/google/uuid"
)

const (
	maxPeerServicesPerRoute           = 4
	maxPeerServiceBytes               = 512 << 20
	maxPeerServiceControlPayloadBytes = 4 * 1024
)

// peerServicePreview is the requester-side byte pipe beneath the stable local
// HTTP gateway. The Peer record authenticates and encrypts every message, so
// this layer needs neither Relay tickets nor service keys from account_key.
type peerServicePreview struct {
	id      uuid.UUID
	client  *peerNativeDirectClient
	ctx     context.Context
	cancel  context.CancelFunc
	send    chan serviceHostMessage
	receive chan peertransport.ServiceMessage

	mu          sync.Mutex
	nextID      uint32
	connections map[uint32]net.Conn
	transferred uint64
	closeOnce   sync.Once
}

// peerServiceHost owns one owner-side loopback target. It never subscribes to
// the PTY; authority comes from its parent authenticated route lease.
type peerServiceHost struct {
	id         uuid.UUID
	targetAddr string
	attempt    *peerHostAttempt
	ctx        context.Context
	cancel     context.CancelFunc
	send       chan serviceHostMessage
	receive    chan peertransport.ServiceMessage

	mu          sync.Mutex
	connections map[uint32]net.Conn
	transferred uint64
	closeOnce   sync.Once
}

func (a *peerHostAttempt) handleServiceOpen(ctx context.Context, frame proto.Frame) error {
	if len(frame.Payload) == 0 || len(frame.Payload) > maxPeerServiceControlPayloadBytes {
		return errors.New("invalid Peer preview open request")
	}
	var request proto.ServiceOpenPayload
	decoder := json.NewDecoder(bytes.NewReader(frame.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		request.RequestID == "" || len(request.RequestID) > 256 || request.ServiceID == "" ||
		request.HostTicket != "" || len(request.Sealed) != 0 || request.PeerFields == nil {
		return errors.New("invalid Peer preview open request")
	}
	reply := proto.ServiceOpenedPayload{RequestID: request.RequestID, ServiceID: request.ServiceID}
	fail := func(code string) error {
		reply.Error = code
		return a.sendServiceOpened(ctx, reply)
	}
	if !a.serviceAuthorized() {
		return fail("permission_denied")
	}
	serviceID, err := uuid.Parse(request.ServiceID)
	if err != nil || serviceID == uuid.Nil || request.PeerFields.Port == 0 || request.PeerFields.Scheme != "http" {
		return fail("invalid_request")
	}
	targetAddr, err := serviceLoopbackTarget(request.PeerFields.Host, request.PeerFields.Port)
	if err != nil {
		return fail("invalid_request")
	}
	a.mu.Lock()
	if a.services == nil {
		a.services = make(map[uuid.UUID]*peerServiceHost)
	}
	if _, exists := a.services[serviceID]; exists || len(a.services) >= maxPeerServicesPerRoute {
		a.mu.Unlock()
		return fail("service_limit")
	}
	host := newPeerServiceHost(a, serviceID, targetAddr)
	a.services[serviceID] = host
	a.mu.Unlock()
	reply.OK = true
	if err := a.sendServiceOpened(ctx, reply); err != nil {
		host.close()
		return err
	}
	go host.run()
	return nil
}

func (a *peerHostAttempt) handleServiceClose(frame proto.Frame) error {
	if len(frame.Payload) == 0 || len(frame.Payload) > maxPeerServiceControlPayloadBytes {
		return errors.New("invalid Peer preview close request")
	}
	var request proto.ServiceClosePayload
	decoder := json.NewDecoder(bytes.NewReader(frame.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid Peer preview close request")
	}
	serviceID, err := uuid.Parse(request.ServiceID)
	if err != nil || serviceID == uuid.Nil {
		return errors.New("invalid Peer preview close request")
	}
	a.mu.Lock()
	host := a.services[serviceID]
	a.mu.Unlock()
	if host != nil {
		host.close()
	}
	return nil
}

func (a *peerHostAttempt) sendServiceOpened(ctx context.Context, payload proto.ServiceOpenedPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	a.mu.Lock()
	channel := a.channel
	a.mu.Unlock()
	if channel == nil || !a.currentRouteLease() {
		return errors.New("Peer preview route is unavailable")
	}
	return channel.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeServiceOpened, SessionID: a.sessionID, Payload: raw,
	}))
}

func (a *peerHostAttempt) handleServiceMessage(payload []byte) error {
	message, err := peertransport.DecodeServiceMessage(payload)
	if err != nil {
		return err
	}
	a.mu.Lock()
	host := a.services[message.ServiceID]
	a.mu.Unlock()
	if host == nil {
		if message.Kind == peertransport.ServiceClose {
			return nil
		}
		return errors.New("Peer preview message targets an unknown service")
	}
	return host.handle(message)
}

func (a *peerHostAttempt) sendServiceMessage(ctx context.Context, payload []byte) error {
	a.mu.Lock()
	channel := a.channel
	a.mu.Unlock()
	if channel == nil || !a.serviceAuthorized() {
		return errors.New("Peer preview is no longer authorized")
	}
	return channel.SendServiceMessage(ctx, payload)
}

func (a *peerHostAttempt) serviceAuthorized() bool {
	if !a.currentRouteLease() {
		return false
	}
	a.mu.Lock()
	remoteMembership := a.remoteMembership
	grantedPermission := a.permission
	sub := a.sub
	a.mu.Unlock()
	permission, err := a.host.currentPermission(remoteMembership, a.sessionID)
	if err != nil || permission != proto.RemotePermissionFull || grantedPermission != proto.RemotePermissionFull {
		return false
	}
	sess, ok := a.host.host.server.Registry().Get(a.sessionID)
	return sub != nil && ok && sess.IsDriver(sub)
}

func (a *peerHostAttempt) closePeerServices() {
	a.mu.Lock()
	services := make([]*peerServiceHost, 0, len(a.services))
	for _, service := range a.services {
		services = append(services, service)
	}
	a.services = nil
	a.mu.Unlock()
	for _, service := range services {
		service.close()
	}
}

func (a *App) newPeerServicePreview(parent context.Context, attemptID string, serviceID uuid.UUID) (servicePreviewPipe, error) {
	client := a.peerNativeDirectClient(attemptID)
	if client == nil {
		return nil, errors.New("Peer preview route is unavailable")
	}
	return client.registerServicePreview(parent, serviceID)
}

func (c *peerNativeDirectClient) registerServicePreview(parent context.Context, serviceID uuid.UUID) (*peerServicePreview, error) {
	c.mu.Lock()
	if c.stopped || !c.authenticated || c.channel == nil || serviceID == uuid.Nil {
		c.mu.Unlock()
		return nil, errors.New("Peer preview route is unavailable")
	}
	if c.servicePreviews == nil {
		c.servicePreviews = make(map[uuid.UUID]*peerServicePreview)
	}
	if _, exists := c.servicePreviews[serviceID]; exists || len(c.servicePreviews) >= maxPeerServicesPerRoute {
		c.mu.Unlock()
		return nil, errors.New("Peer preview limit or duplicate service")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	preview := &peerServicePreview{
		id: serviceID, client: c, ctx: ctx, cancel: cancel,
		send:        make(chan serviceHostMessage, serviceHostQueueDepth),
		receive:     make(chan peertransport.ServiceMessage, serviceHostQueueDepth),
		connections: make(map[uint32]net.Conn),
	}
	c.servicePreviews[serviceID] = preview
	c.mu.Unlock()
	if parent != nil {
		go func() {
			select {
			case <-parent.Done():
				preview.close()
			case <-ctx.Done():
			}
		}()
	}
	return preview, nil
}

func (p *peerServicePreview) run() {
	defer p.close()
	for {
		select {
		case <-p.ctx.Done():
			return
		case message := <-p.send:
			if !p.reserve(message) {
				return
			}
			payload, err := encodePeerServiceMessage(p.id, message)
			if err != nil || p.client.sendServiceMessage(p.ctx, payload) != nil {
				return
			}
		case message := <-p.receive:
			if err := p.apply(message); err != nil {
				p.client.fail(err)
				return
			}
		}
	}
}

func (p *peerServicePreview) handle(message peertransport.ServiceMessage) error {
	if message.ServiceID != p.id || !p.reservePeerData(message) {
		return errors.New("Peer preview byte limit exceeded")
	}
	return enqueuePeerServiceInbound(p.ctx, p.cancel, p.receive, message)
}

func (p *peerServicePreview) apply(message peertransport.ServiceMessage) error {
	switch message.Kind {
	case peertransport.ServiceData:
		conn := p.connection(message.Connection)
		if conn == nil {
			return errors.New("Peer preview data for unopened connection")
		}
		if err := writePeerServiceData(conn, message.Data); err != nil {
			p.closeConnection(message.Connection)
			p.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: message.Connection})
		}
		return nil
	case peertransport.ServiceClose:
		p.closeConnection(message.Connection)
		return nil
	default:
		return errors.New("Peer preview host sent open")
	}
}

func (p *peerServicePreview) acceptConn(conn net.Conn) bool {
	p.mu.Lock()
	if p.ctx.Err() != nil || len(p.connections) >= 16 {
		p.mu.Unlock()
		return false
	}
	id := p.nextConnectionIDLocked()
	if id == 0 {
		p.mu.Unlock()
		return false
	}
	p.connections[id] = conn
	p.mu.Unlock()
	if !p.enqueue(serviceHostMessage{kind: serviceproxy.KindOpen, id: id}) {
		p.closeConnection(id)
		return false
	}
	go p.readConnection(id, conn)
	return true
}

func (p *peerServicePreview) readConnection(id uint32, conn net.Conn) {
	buf := make([]byte, peertransport.MaxServiceData)
	for {
		n, err := conn.Read(buf)
		if n > 0 && !p.enqueue(serviceHostMessage{kind: serviceproxy.KindData, id: id, data: append([]byte(nil), buf[:n]...)}) {
			return
		}
		if err != nil {
			p.closeConnectionIf(id, conn)
			p.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: id})
			return
		}
	}
}

func (p *peerServicePreview) enqueue(message serviceHostMessage) bool {
	return enqueuePeerServiceMessage(p.ctx, p.cancel, p.send, message)
}

func (p *peerServicePreview) reserve(message serviceHostMessage) bool {
	if message.kind != serviceproxy.KindData {
		return true
	}
	return p.reserveBytes(len(message.data))
}

func (p *peerServicePreview) reservePeerData(message peertransport.ServiceMessage) bool {
	if message.Kind != peertransport.ServiceData {
		return true
	}
	return p.reserveBytes(len(message.Data))
}

func (p *peerServicePreview) reserveBytes(size int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if size < 0 || p.transferred+uint64(size) > maxPeerServiceBytes {
		return false
	}
	p.transferred += uint64(size)
	return true
}

func (p *peerServicePreview) nextConnectionIDLocked() uint32 {
	for attempts := 0; attempts < 32; attempts++ {
		p.nextID++
		if p.nextID == 0 {
			p.nextID++
		}
		if _, exists := p.connections[p.nextID]; !exists {
			return p.nextID
		}
	}
	return 0
}

func (p *peerServicePreview) connection(id uint32) net.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connections[id]
}

func (p *peerServicePreview) closeConnection(id uint32) {
	p.mu.Lock()
	conn := p.connections[id]
	delete(p.connections, id)
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *peerServicePreview) closeConnectionIf(id uint32, want net.Conn) {
	p.mu.Lock()
	conn := p.connections[id]
	if conn == want {
		delete(p.connections, id)
	}
	p.mu.Unlock()
	if conn == want {
		_ = conn.Close()
	}
}

func (p *peerServicePreview) close() {
	p.closeOnce.Do(func() {
		p.cancel()
		p.client.mu.Lock()
		if p.client.servicePreviews[p.id] == p {
			delete(p.client.servicePreviews, p.id)
		}
		p.client.mu.Unlock()
		p.mu.Lock()
		connections := make([]net.Conn, 0, len(p.connections))
		for _, conn := range p.connections {
			connections = append(connections, conn)
		}
		p.connections = make(map[uint32]net.Conn)
		p.mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
}

func newPeerServiceHost(attempt *peerHostAttempt, serviceID uuid.UUID, targetAddr string) *peerServiceHost {
	ctx, cancel := context.WithCancel(attempt.streamCtx)
	return &peerServiceHost{
		id: serviceID, targetAddr: targetAddr, attempt: attempt, ctx: ctx, cancel: cancel,
		send:        make(chan serviceHostMessage, serviceHostQueueDepth),
		receive:     make(chan peertransport.ServiceMessage, serviceHostQueueDepth),
		connections: make(map[uint32]net.Conn),
	}
}

func (h *peerServiceHost) run() {
	defer h.close()
	for {
		select {
		case <-h.ctx.Done():
			return
		case message := <-h.send:
			if !h.reserve(message) || !h.attempt.serviceAuthorized() {
				return
			}
			payload, err := encodePeerServiceMessage(h.id, message)
			if err != nil || h.attempt.sendServiceMessage(h.ctx, payload) != nil {
				return
			}
		case message := <-h.receive:
			if err := h.apply(message); err != nil {
				go h.attempt.removeSelf()
				return
			}
		}
	}
}

func (h *peerServiceHost) handle(message peertransport.ServiceMessage) error {
	if message.ServiceID != h.id || !h.attempt.serviceAuthorized() || !h.reservePeerData(message) {
		return errors.New("Peer preview is no longer authorized")
	}
	return enqueuePeerServiceInbound(h.ctx, h.cancel, h.receive, message)
}

func (h *peerServiceHost) apply(message peertransport.ServiceMessage) error {
	switch message.Kind {
	case peertransport.ServiceOpen:
		if err := h.openTarget(message.Connection); err != nil {
			h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: message.Connection})
		}
		return nil
	case peertransport.ServiceData:
		conn := h.connection(message.Connection)
		if conn == nil {
			return errors.New("Peer preview data for unopened target")
		}
		if err := writePeerServiceData(conn, message.Data); err != nil {
			h.closeTarget(message.Connection)
			h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: message.Connection})
		}
		return nil
	case peertransport.ServiceClose:
		h.closeTarget(message.Connection)
		return nil
	default:
		return errors.New("invalid Peer preview operation")
	}
}

func (h *peerServiceHost) openTarget(id uint32) error {
	h.mu.Lock()
	if h.ctx.Err() != nil || len(h.connections) >= 16 {
		h.mu.Unlock()
		return errors.New("Peer preview target connection limit")
	}
	if _, exists := h.connections[id]; exists {
		h.mu.Unlock()
		return errors.New("duplicate Peer preview connection")
	}
	h.mu.Unlock()
	dialer := net.Dialer{Timeout: serviceTargetTimeout}
	conn, err := dialer.DialContext(h.ctx, "tcp", h.targetAddr)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.ctx.Err() != nil || len(h.connections) >= 16 {
		h.mu.Unlock()
		_ = conn.Close()
		return errors.New("Peer preview target unavailable")
	}
	if _, exists := h.connections[id]; exists {
		h.mu.Unlock()
		_ = conn.Close()
		return errors.New("duplicate Peer preview connection")
	}
	h.connections[id] = conn
	h.mu.Unlock()
	go h.readTarget(id, conn)
	return nil
}

func (h *peerServiceHost) readTarget(id uint32, conn net.Conn) {
	buf := make([]byte, peertransport.MaxServiceData)
	for {
		n, err := conn.Read(buf)
		if n > 0 && !h.enqueue(serviceHostMessage{kind: serviceproxy.KindData, id: id, data: append([]byte(nil), buf[:n]...)}) {
			return
		}
		if err != nil {
			h.closeTargetIf(id, conn)
			h.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: id})
			return
		}
	}
}

func (h *peerServiceHost) enqueue(message serviceHostMessage) bool {
	return enqueuePeerServiceMessage(h.ctx, h.cancel, h.send, message)
}

func (h *peerServiceHost) reserve(message serviceHostMessage) bool {
	if message.kind != serviceproxy.KindData {
		return true
	}
	return h.reserveBytes(len(message.data))
}

func (h *peerServiceHost) reservePeerData(message peertransport.ServiceMessage) bool {
	if message.Kind != peertransport.ServiceData {
		return true
	}
	return h.reserveBytes(len(message.Data))
}

func (h *peerServiceHost) reserveBytes(size int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if size < 0 || h.transferred+uint64(size) > maxPeerServiceBytes {
		return false
	}
	h.transferred += uint64(size)
	return true
}

func (h *peerServiceHost) connection(id uint32) net.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connections[id]
}

func (h *peerServiceHost) closeTarget(id uint32) {
	h.mu.Lock()
	conn := h.connections[id]
	delete(h.connections, id)
	h.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (h *peerServiceHost) closeTargetIf(id uint32, want net.Conn) {
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

func (h *peerServiceHost) close() {
	h.closeOnce.Do(func() {
		h.cancel()
		h.attempt.mu.Lock()
		if h.attempt.services[h.id] == h {
			delete(h.attempt.services, h.id)
		}
		h.attempt.mu.Unlock()
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

func encodePeerServiceMessage(serviceID uuid.UUID, message serviceHostMessage) ([]byte, error) {
	var kind peertransport.ServiceKind
	switch message.kind {
	case serviceproxy.KindOpen:
		kind = peertransport.ServiceOpen
	case serviceproxy.KindData:
		kind = peertransport.ServiceData
	case serviceproxy.KindClose:
		kind = peertransport.ServiceClose
	default:
		return nil, peertransport.ErrInvalidServiceMessage
	}
	return peertransport.EncodeServiceMessage(peertransport.ServiceMessage{
		ServiceID: serviceID, Kind: kind, Connection: message.id, Data: message.data,
	})
}

func enqueuePeerServiceMessage(ctx context.Context, cancel context.CancelFunc, queue chan<- serviceHostMessage, message serviceHostMessage) bool {
	select {
	case queue <- message:
		return true
	case <-ctx.Done():
		return false
	default:
	}
	timer := time.NewTimer(serviceEnqueueWait)
	defer timer.Stop()
	select {
	case queue <- message:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		cancel()
		return false
	}
}

func enqueuePeerServiceInbound(ctx context.Context, cancel context.CancelFunc, queue chan<- peertransport.ServiceMessage, message peertransport.ServiceMessage) error {
	select {
	case queue <- message:
		return nil
	case <-ctx.Done():
		return errors.New("Peer preview is closed")
	default:
	}
	timer := time.NewTimer(serviceEnqueueWait)
	defer timer.Stop()
	select {
	case queue <- message:
		return nil
	case <-ctx.Done():
		return errors.New("Peer preview is closed")
	case <-timer.C:
		cancel()
		return errors.New("Peer preview receive queue is full")
	}
}

func writePeerServiceData(conn net.Conn, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(serviceHostWriteWait)); err != nil {
		return err
	}
	written, err := conn.Write(data)
	_ = conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
