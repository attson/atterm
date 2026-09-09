package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/serviceproxy"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// ServicePreviewStartRequest contains only per-lease derived keys. The
// account_key and relay credential are deliberately absent: the former stays
// in the renderer, while the latter is read from config by the App binding.
type ServicePreviewStartRequest struct {
	// Mappings is the multi-service form. The first mapping is the preview
	// entry point; later mappings are selected by PathPrefix (for example
	// /api -> a backend port). The legacy fields below remain accepted so old
	// frontends can still open a single-port preview.
	Mappings []ServicePreviewMapping `json:"mappings,omitempty"`

	ServiceID       string `json:"service_id"`
	ClientTicket    string `json:"client_ticket"`
	ClientToHostKey []byte `json:"client_to_host_key"`
	HostToClientKey []byte `json:"host_to_client_key"`
}

type ServicePreviewMapping struct {
	ServiceID       string `json:"service_id"`
	ClientTicket    string `json:"client_ticket"`
	ClientToHostKey []byte `json:"client_to_host_key"`
	HostToClientKey []byte `json:"host_to_client_key"`
	Port            uint16 `json:"port"`
	PathPrefix      string `json:"path_prefix,omitempty"`
}

type ServicePreviewStartResponse struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type servicePreviewManager struct {
	mu       sync.Mutex
	previews map[uuid.UUID]*servicePreviewGateway
}

type servicePreviewGateway struct {
	id        uuid.UUID
	cancel    context.CancelFunc
	listener  net.Listener
	server    *http.Server
	pipes     []*servicePreview
	closeOnce sync.Once
}

type servicePreviewRoute struct {
	prefix string
	proxy  *httputil.ReverseProxy
}

type servicePreview struct {
	id       uuid.UUID
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	ws       *websocket.Conn
	codec    *serviceproxy.Codec
	send     chan serviceHostMessage

	mu          sync.Mutex
	nextID      uint32
	connections map[uint32]net.Conn
	closeOnce   sync.Once
}

func (a *App) StartServicePreview(req ServicePreviewStartRequest) (ServicePreviewStartResponse, error) {
	if a.cfgStore == nil {
		return ServicePreviewStartResponse{}, errors.New("relay config unavailable")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" || cfg.RelayPaused {
		return ServicePreviewStartResponse{}, errors.New("relay is not connected")
	}
	// The control request was routed through the user's home instance. The
	// data socket must land on that same in-memory service hub; dialing the
	// login/bootstrap URL would fail in a multi-instance realm.
	cfg.RelayURL = uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	return a.servicePreviews.start(a.ctx, cfg, req)
}

func (a *App) StopServicePreview(id string) {
	parsed, err := uuid.Parse(id)
	if err == nil {
		a.servicePreviews.stop(parsed)
	}
}

func (m *servicePreviewManager) start(parent context.Context, cfg appConfig, req ServicePreviewStartRequest) (ServicePreviewStartResponse, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	mappings := req.Mappings
	if len(mappings) == 0 {
		mappings = []ServicePreviewMapping{{
			ServiceID: req.ServiceID, ClientTicket: req.ClientTicket,
			ClientToHostKey: req.ClientToHostKey, HostToClientKey: req.HostToClientKey,
		}}
	}
	if len(mappings) == 0 || len(mappings) > 8 {
		cancel()
		return ServicePreviewStartResponse{}, errors.New("invalid service preview mappings")
	}
	pipes := make([]*servicePreview, 0, len(mappings))
	routes := make([]servicePreviewRoute, 0, len(mappings))
	seenPrefixes := make(map[string]struct{}, len(mappings))
	for i, mapping := range mappings {
		prefix, err := normalizeServicePreviewPrefix(mapping.PathPrefix, i == 0)
		if err != nil {
			cancel()
			return ServicePreviewStartResponse{}, err
		}
		if _, exists := seenPrefixes[prefix]; exists {
			cancel()
			return ServicePreviewStartResponse{}, errors.New("service preview path prefixes must be distinct")
		}
		seenPrefixes[prefix] = struct{}{}
		mapping.PathPrefix = prefix
		pipe, err := m.startPipe(ctx, cfg, mapping)
		if err != nil {
			for _, started := range pipes {
				started.close()
			}
			cancel()
			return ServicePreviewStartResponse{}, err
		}
		pipes = append(pipes, pipe)
		target, _ := url.Parse("http://" + pipe.listener.Addr().String())
		routes = append(routes, newServicePreviewRoute(mapping.PathPrefix, target))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		for _, pipe := range pipes {
			pipe.close()
		}
		cancel()
		return ServicePreviewStartResponse{}, err
	}
	gatewayID := uuid.New()
	gateway := &servicePreviewGateway{id: gatewayID, cancel: cancel, listener: listener, pipes: pipes}
	gateway.server = &http.Server{Handler: newServicePreviewHandler(routes)}
	m.mu.Lock()
	if m.previews == nil {
		m.previews = make(map[uuid.UUID]*servicePreviewGateway)
	}
	if _, exists := m.previews[gatewayID]; exists {
		m.mu.Unlock()
		gateway.close()
		return ServicePreviewStartResponse{}, errors.New("service preview already running")
	}
	m.previews[gatewayID] = gateway
	m.mu.Unlock()
	go func() {
		gateway.run()
		m.mu.Lock()
		if m.previews[gatewayID] == gateway {
			delete(m.previews, gatewayID)
		}
		m.mu.Unlock()
	}()
	return ServicePreviewStartResponse{
		ID:  gatewayID.String(),
		URL: "http://" + listener.Addr().String() + "/",
	}, nil
}

func normalizeServicePreviewPrefix(raw string, root bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if root {
		if raw == "" || raw == "/" {
			return "", nil
		}
		return "", errors.New("first service preview mapping must be the root")
	}
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "?#") {
		return "", errors.New("additional service preview mappings require a path prefix")
	}
	cleaned := path.Clean(raw)
	if cleaned == "/" || cleaned == "." || cleaned != strings.TrimRight(raw, "/") {
		return "", errors.New("invalid service preview path prefix")
	}
	return cleaned, nil
}

func (m *servicePreviewManager) startPipe(parent context.Context, cfg appConfig, mapping ServicePreviewMapping) (*servicePreview, error) {
	serviceID, err := uuid.Parse(mapping.ServiceID)
	if err != nil || serviceID == uuid.Nil || mapping.ClientTicket == "" {
		return nil, errors.New("invalid service preview request")
	}
	if len(mapping.ClientToHostKey) != e2eecrypto.SessionKeySize || len(mapping.HostToClientKey) != e2eecrypto.SessionKeySize {
		return nil, errors.New("invalid service preview keys")
	}
	codec, err := serviceproxy.NewCodec(serviceID, mapping.ClientToHostKey, mapping.HostToClientKey)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + cfg.RelaySessionToken}}}
	if cfg.AllowInsecureRelay {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, serviceHostDialTimeout)
	ws, _, err := websocket.Dial(dialCtx, stringsTrimRightSlash(cfg.RelayURL)+"/service-client", opts)
	cancelDial()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect relay service: %w", err)
	}
	ws.SetReadLimit(serviceproxy.MaxPacketSize)
	reg, err := json.Marshal(serviceRegistration{ServiceID: serviceID.String(), Ticket: mapping.ClientTicket})
	if err == nil {
		wctx, wc := context.WithTimeout(ctx, serviceHostWriteWait)
		err = ws.Write(wctx, websocket.MessageBinary, reg)
		wc()
	}
	if err != nil {
		cancel()
		_ = ws.CloseNow()
		return nil, fmt.Errorf("register relay service: %w", err)
	}
	ackCtx, ackCancel := context.WithTimeout(ctx, serviceHostWriteWait)
	_, ackRaw, err := ws.Read(ackCtx)
	ackCancel()
	var ack serviceRegistrationAck
	if err != nil || json.Unmarshal(ackRaw, &ack) != nil || !ack.OK {
		cancel()
		_ = ws.CloseNow()
		if err == nil {
			err = errors.New("service registration rejected")
		}
		return nil, fmt.Errorf("register relay service: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		_ = ws.CloseNow()
		return nil, err
	}
	pipe := &servicePreview{id: serviceID, ctx: ctx, cancel: cancel, listener: listener, ws: ws, codec: codec, send: make(chan serviceHostMessage, serviceHostQueueDepth), connections: make(map[uint32]net.Conn)}
	go pipe.run()
	return pipe, nil
}

func newServicePreviewHandler(routes []servicePreviewRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var selected *servicePreviewRoute
		for i := range routes {
			prefix := routes[i].prefix
			if prefix == "" || (strings.HasPrefix(r.URL.Path, prefix) && (len(r.URL.Path) == len(prefix) || strings.HasPrefix(r.URL.Path[len(prefix):], "/"))) {
				if selected == nil || len(prefix) > len(selected.prefix) {
					selected = &routes[i]
				}
			}
		}
		if selected == nil {
			http.NotFound(w, r)
			return
		}
		selected.proxy.ServeHTTP(w, r)
	})
}

func newServicePreviewRoute(prefix string, target *url.URL) servicePreviewRoute {
	proxy := httputil.NewSingleHostReverseProxy(target)
	// Immediate flushing is required for event streams. ReverseProxy also
	// handles WebSocket upgrades, so HTTP, SSE, and WS share the same route.
	proxy.FlushInterval = -1
	return servicePreviewRoute{prefix: prefix, proxy: proxy}
}

func (g *servicePreviewGateway) run() {
	err := g.server.Serve(g.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		logDebug("service-preview", "gateway=%s closed: %v", g.id, err)
	}
	g.close()
}

func (g *servicePreviewGateway) close() {
	g.closeOnce.Do(func() {
		g.cancel()
		if g.server != nil {
			_ = g.server.Close()
		}
		if g.listener != nil {
			_ = g.listener.Close()
		}
		for _, pipe := range g.pipes {
			pipe.close()
		}
	})
}

func stringsTrimRightSlash(raw string) string {
	for len(raw) > 0 && raw[len(raw)-1] == '/' {
		raw = raw[:len(raw)-1]
	}
	return raw
}

func (m *servicePreviewManager) stop(id uuid.UUID) {
	m.mu.Lock()
	preview := m.previews[id]
	delete(m.previews, id)
	m.mu.Unlock()
	if preview != nil {
		preview.close()
	}
}

func (m *servicePreviewManager) stopAll() {
	m.mu.Lock()
	previews := make([]*servicePreviewGateway, 0, len(m.previews))
	for _, preview := range m.previews {
		previews = append(previews, preview)
	}
	m.previews = make(map[uuid.UUID]*servicePreviewGateway)
	m.mu.Unlock()
	for _, preview := range previews {
		preview.close()
	}
}

func (p *servicePreview) run() {
	defer p.close()
	errCh := make(chan error, 3)
	go func() { errCh <- p.writeLoop() }()
	go func() { errCh <- p.readLoop() }()
	go func() { errCh <- p.acceptLoop() }()
	select {
	case <-p.ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
			logDebug("service-preview", "service=%s closed: %v", p.id, err)
		}
	}
}

func (p *servicePreview) writeLoop() error {
	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case msg := <-p.send:
			packet, err := p.codec.Seal(msg.kind, msg.id, msg.data)
			if err != nil {
				return err
			}
			wctx, cancel := context.WithTimeout(p.ctx, serviceHostWriteWait)
			err = p.ws.Write(wctx, websocket.MessageBinary, packet)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func (p *servicePreview) readLoop() error {
	for {
		mt, packet, err := p.ws.Read(p.ctx)
		if err != nil {
			return err
		}
		if mt != websocket.MessageBinary {
			return errors.New("service data must be binary")
		}
		header, plain, err := p.codec.Open(packet)
		if err != nil {
			return err
		}
		switch header.Kind {
		case serviceproxy.KindData:
			conn := p.connection(header.Connection)
			if conn == nil {
				return errors.New("data for unopened local connection")
			}
			if _, err := conn.Write(plain); err != nil {
				p.closeConnection(header.Connection)
				p.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: header.Connection})
			}
		case serviceproxy.KindClose:
			if len(plain) != 0 {
				return errors.New("close packet has payload")
			}
			p.closeConnection(header.Connection)
		case serviceproxy.KindOpen:
			return errors.New("host sent open packet")
		}
	}
}

func (p *servicePreview) acceptLoop() error {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return err
		}
		p.mu.Lock()
		if len(p.connections) >= 16 {
			p.mu.Unlock()
			_ = conn.Close()
			continue
		}
		id := p.nextConnectionIDLocked()
		if id == 0 {
			p.mu.Unlock()
			_ = conn.Close()
			return errors.New("connection id space exhausted")
		}
		p.connections[id] = conn
		p.mu.Unlock()
		if !p.enqueue(serviceHostMessage{kind: serviceproxy.KindOpen, id: id}) {
			return p.ctx.Err()
		}
		go p.readConnection(id, conn)
	}
}

func (p *servicePreview) nextConnectionIDLocked() uint32 {
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

func (p *servicePreview) readConnection(id uint32, conn net.Conn) {
	buf := make([]byte, serviceproxy.MaxPlaintextSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if !p.enqueue(serviceHostMessage{kind: serviceproxy.KindData, id: id, data: append([]byte(nil), buf[:n]...)}) {
				return
			}
		}
		if err != nil {
			p.closeConnectionIf(id, conn)
			p.enqueue(serviceHostMessage{kind: serviceproxy.KindClose, id: id})
			return
		}
	}
}

func (p *servicePreview) enqueue(msg serviceHostMessage) bool {
	select {
	case p.send <- msg:
		return true
	case <-p.ctx.Done():
		return false
	default:
		p.cancel()
		return false
	}
}

func (p *servicePreview) connection(id uint32) net.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connections[id]
}

func (p *servicePreview) closeConnection(id uint32) {
	p.mu.Lock()
	conn := p.connections[id]
	delete(p.connections, id)
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *servicePreview) closeConnectionIf(id uint32, want net.Conn) {
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

func (p *servicePreview) close() {
	p.closeOnce.Do(func() {
		p.cancel()
		_ = p.listener.Close()
		_ = p.ws.CloseNow()
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
