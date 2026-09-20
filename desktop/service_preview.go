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
	"time"

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

// ServicePreviewRebindRequest re-establishes one mapping of a running preview
// after its pipe died. The renderer re-ran the SERVICE_OPEN control handshake to
// mint a fresh serviceID/ticket/keys; the gateway id + mapping index identify
// which stable listener to reattach beneath (URL/prefix/port are unchanged).
type ServicePreviewRebindRequest struct {
	GatewayID       string `json:"gateway_id"`
	MappingIndex    int    `json:"mapping_index"`
	ServiceID       string `json:"service_id"`
	ClientTicket    string `json:"client_ticket"`
	ClientToHostKey []byte `json:"client_to_host_key"`
	HostToClientKey []byte `json:"host_to_client_key"`
}

type servicePreviewManager struct {
	mu       sync.Mutex
	previews map[uuid.UUID]*servicePreviewGateway
	// emit pushes lifecycle events (pipe-dead / rebound) to the renderer. Set
	// once from the App binding; nil in tests that don't need events.
	emit func(name string, data any)
}

type servicePreviewGateway struct {
	id        uuid.UUID
	ctx       context.Context
	cancel    context.CancelFunc
	listener  net.Listener
	server    *http.Server
	mappings  []*servicePreviewMapping
	emit      func(name string, data any)
	closeOnce sync.Once
}

// servicePreviewMapping is the STABLE half of one preview route. Its loopback
// listener (the reverse-proxy target) and prefix survive across reconnects; only
// the pipe beneath it is rebuilt. Keeping the listener stable means the gateway
// URL the browser holds never changes when a lease is re-established.
type servicePreviewMapping struct {
	index    int
	prefix   string
	port     uint16
	listener net.Listener
	proxy    *httputil.ReverseProxy
	gateway  *servicePreviewGateway

	mu        sync.Mutex
	pipe      *servicePreview // current live pipe; nil while dead
	serviceID uuid.UUID
	dead      bool
}

type servicePreviewRoute struct {
	prefix string
	proxy  *httputil.ReverseProxy
}

type servicePreview struct {
	id     uuid.UUID
	ctx    context.Context
	cancel context.CancelFunc
	ws     *websocket.Conn
	codec  *serviceproxy.Codec
	send   chan serviceHostMessage

	mu          sync.Mutex
	nextID      uint32
	connections map[uint32]net.Conn
	closeOnce   sync.Once
}

// servicePreviewReconnectingBody is returned (503 + Retry-After) while a mapping
// is dead and awaiting rebind, instead of the 502 a closed proxy target yields.
// The renderer overlays a "reconnecting" state over the iframe so users never
// see this body directly.
var servicePreviewReconnectingBody = []byte(`{"reconnecting":true}`)

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
	a.wireServicePreviewEmitter()
	return a.servicePreviews.start(a.ctx, cfg, req)
}

// wireServicePreviewEmitter installs the Wails event emitter onto the preview
// manager. Idempotent; safe to call on every Start/Rebind.
func (a *App) wireServicePreviewEmitter() {
	a.servicePreviews.mu.Lock()
	defer a.servicePreviews.mu.Unlock()
	if a.servicePreviews.emit != nil {
		return
	}
	a.servicePreviews.emit = func(name string, data any) {
		if a.eventsEmitter == nil {
			return
		}
		a.eventsEmitter(a.ctx, name, data)
	}
}

func (a *App) StopServicePreview(id string) {
	parsed, err := uuid.Parse(id)
	if err == nil {
		a.servicePreviews.stop(parsed)
	}
}

// RebindServicePreview reattaches a dead mapping's pipe using a fresh lease the
// renderer just opened. It keeps the gateway (and the browser URL) alive.
func (a *App) RebindServicePreview(req ServicePreviewRebindRequest) error {
	if a.cfgStore == nil {
		return errors.New("relay config unavailable")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" || cfg.RelayPaused {
		return errors.New("relay is not connected")
	}
	cfg.RelayURL = uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	gid, err := uuid.Parse(req.GatewayID)
	if err != nil {
		return errors.New("invalid gateway id")
	}
	a.wireServicePreviewEmitter()
	return a.servicePreviews.rebind(cfg, gid, req)
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
	m.mu.Lock()
	emit := m.emit
	m.mu.Unlock()
	gatewayID := uuid.New()
	gateway := &servicePreviewGateway{id: gatewayID, ctx: ctx, cancel: cancel, emit: emit}
	seenPrefixes := make(map[string]struct{}, len(mappings))
	for i, mapping := range mappings {
		prefix, err := normalizeServicePreviewPrefix(mapping.PathPrefix, i == 0)
		if err != nil {
			gateway.close()
			return ServicePreviewStartResponse{}, err
		}
		if _, exists := seenPrefixes[prefix]; exists {
			gateway.close()
			return ServicePreviewStartResponse{}, errors.New("service preview path prefixes must be distinct")
		}
		seenPrefixes[prefix] = struct{}{}
		mapping.PathPrefix = prefix
		ws, codec, serviceID, err := dialServiceClient(ctx, cfg, mapping)
		if err != nil {
			gateway.close()
			return ServicePreviewStartResponse{}, err
		}
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = ws.CloseNow()
			gateway.close()
			return ServicePreviewStartResponse{}, err
		}
		target, _ := url.Parse("http://" + lis.Addr().String())
		mp := &servicePreviewMapping{
			index:     i,
			prefix:    prefix,
			port:      mapping.Port,
			listener:  lis,
			proxy:     newServicePreviewProxy(target),
			gateway:   gateway,
			serviceID: serviceID,
		}
		pipe := newServicePreview(ctx, ws, codec, serviceID)
		mp.pipe = pipe
		gateway.mappings = append(gateway.mappings, mp)
		go mp.acceptLoop()
		go mp.runPipe(pipe)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		gateway.close()
		return ServicePreviewStartResponse{}, err
	}
	gateway.listener = listener
	gateway.server = &http.Server{Handler: http.HandlerFunc(gateway.serveHTTP)}
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

// rebind rebuilds one mapping's pipe under the existing gateway, reusing the
// mapping's stable listener so the reverse-proxy target and the browser URL are
// unchanged. The fresh serviceID/ticket/keys come from the renderer, which
// re-ran the SERVICE_OPEN control handshake to mint them.
func (m *servicePreviewManager) rebind(cfg appConfig, gid uuid.UUID, req ServicePreviewRebindRequest) error {
	m.mu.Lock()
	gateway := m.previews[gid]
	m.mu.Unlock()
	if gateway == nil || gateway.ctx.Err() != nil {
		return errors.New("preview_gone")
	}
	if req.MappingIndex < 0 || req.MappingIndex >= len(gateway.mappings) {
		return errors.New("invalid mapping index")
	}
	mp := gateway.mappings[req.MappingIndex]
	mapping := ServicePreviewMapping{
		ServiceID:       req.ServiceID,
		ClientTicket:    req.ClientTicket,
		ClientToHostKey: req.ClientToHostKey,
		HostToClientKey: req.HostToClientKey,
		Port:            mp.port,
		PathPrefix:      mp.prefix,
	}
	ws, codec, serviceID, err := dialServiceClient(gateway.ctx, cfg, mapping)
	if err != nil {
		return err
	}
	pipe := newServicePreview(gateway.ctx, ws, codec, serviceID)
	mp.mu.Lock()
	old := mp.pipe
	mp.pipe = pipe
	mp.serviceID = serviceID
	mp.dead = false
	mp.mu.Unlock()
	if old != nil {
		old.close()
	}
	go mp.runPipe(pipe)
	if gateway.emit != nil {
		gateway.emit("service-preview:rebound", map[string]any{
			"gateway_id":    gid.String(),
			"mapping_index": mp.index,
			"service_id":    serviceID.String(),
		})
	}
	return nil
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

// dialServiceClient validates the mapping, connects /service-client, and
// registers the one-time client ticket. It returns the live socket + codec so
// both start (fresh listener) and rebind (reused listener) can assemble a pipe.
func dialServiceClient(ctx context.Context, cfg appConfig, mapping ServicePreviewMapping) (*websocket.Conn, *serviceproxy.Codec, uuid.UUID, error) {
	serviceID, err := uuid.Parse(mapping.ServiceID)
	if err != nil || serviceID == uuid.Nil || mapping.ClientTicket == "" {
		return nil, nil, uuid.Nil, errors.New("invalid service preview request")
	}
	if len(mapping.ClientToHostKey) != e2eecrypto.SessionKeySize || len(mapping.HostToClientKey) != e2eecrypto.SessionKeySize {
		return nil, nil, uuid.Nil, errors.New("invalid service preview keys")
	}
	codec, err := serviceproxy.NewCodec(serviceID, mapping.ClientToHostKey, mapping.HostToClientKey)
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + cfg.RelaySessionToken}}}
	if cfg.AllowInsecureRelay {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, serviceHostDialTimeout)
	ws, _, err := websocket.Dial(dialCtx, stringsTrimRightSlash(cfg.RelayURL)+"/service-client", opts)
	cancelDial()
	if err != nil {
		return nil, nil, uuid.Nil, fmt.Errorf("connect relay service: %w", err)
	}
	ws.SetReadLimit(serviceproxy.MaxPacketSize)
	reg, err := json.Marshal(serviceRegistration{ServiceID: serviceID.String(), Ticket: mapping.ClientTicket})
	if err == nil {
		wctx, wc := context.WithTimeout(ctx, serviceHostWriteWait)
		err = ws.Write(wctx, websocket.MessageBinary, reg)
		wc()
	}
	if err != nil {
		_ = ws.CloseNow()
		return nil, nil, uuid.Nil, fmt.Errorf("register relay service: %w", err)
	}
	ackCtx, ackCancel := context.WithTimeout(ctx, serviceHostWriteWait)
	_, ackRaw, err := ws.Read(ackCtx)
	ackCancel()
	var ack serviceRegistrationAck
	if err != nil || json.Unmarshal(ackRaw, &ack) != nil || !ack.OK {
		_ = ws.CloseNow()
		if err == nil {
			err = errors.New("service registration rejected")
		}
		return nil, nil, uuid.Nil, fmt.Errorf("register relay service: %w", err)
	}
	return ws, codec, serviceID, nil
}

func newServicePreview(parent context.Context, ws *websocket.Conn, codec *serviceproxy.Codec, serviceID uuid.UUID) *servicePreview {
	ctx, cancel := context.WithCancel(parent)
	return &servicePreview{
		id:          serviceID,
		ctx:         ctx,
		cancel:      cancel,
		ws:          ws,
		codec:       codec,
		send:        make(chan serviceHostMessage, serviceHostQueueDepth),
		connections: make(map[uint32]net.Conn),
	}
}

// selectServicePreviewRoute returns the index of the longest-prefix match for
// path, or -1. Root ("") matches everything; a prefix matches only at a segment
// boundary so /api does not swallow /api-v2.
func selectServicePreviewRoute(prefixes []string, path string) int {
	best := -1
	for i, prefix := range prefixes {
		if prefix == "" || (strings.HasPrefix(path, prefix) && (len(path) == len(prefix) || strings.HasPrefix(path[len(prefix):], "/"))) {
			if best == -1 || len(prefix) > len(prefixes[best]) {
				best = i
			}
		}
	}
	return best
}

// newServicePreviewHandler is retained for routing tests; the live gateway uses
// gateway.serveHTTP so it can gate dead mappings before proxying.
func newServicePreviewHandler(routes []servicePreviewRoute) http.Handler {
	prefixes := make([]string, len(routes))
	for i := range routes {
		prefixes[i] = routes[i].prefix
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := selectServicePreviewRoute(prefixes, r.URL.Path)
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		routes[idx].proxy.ServeHTTP(w, r)
	})
}

func newServicePreviewRoute(prefix string, target *url.URL) servicePreviewRoute {
	return servicePreviewRoute{prefix: prefix, proxy: newServicePreviewProxy(target)}
}

func newServicePreviewProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	// Immediate flushing is required for event streams. ReverseProxy also
	// handles WebSocket upgrades, so HTTP, SSE, and WS share the same route.
	proxy.FlushInterval = -1
	return proxy
}

func (g *servicePreviewGateway) serveHTTP(w http.ResponseWriter, r *http.Request) {
	prefixes := make([]string, len(g.mappings))
	for i, mp := range g.mappings {
		prefixes[i] = mp.prefix
	}
	idx := selectServicePreviewRoute(prefixes, r.URL.Path)
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	mp := g.mappings[idx]
	mp.mu.Lock()
	dead := mp.dead
	proxy := mp.proxy
	mp.mu.Unlock()
	if dead {
		// A dead mapping's pipe is being rebuilt. Return a fast, cacheable-never
		// 503 instead of the 502 a closed proxy target would yield; the renderer
		// shows a reconnecting overlay and retries the control handshake.
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(servicePreviewReconnectingBody)
		return
	}
	proxy.ServeHTTP(w, r)
}

func (g *servicePreviewGateway) onPipeDead(mp *servicePreviewMapping) {
	if g.ctx.Err() != nil || g.emit == nil {
		return
	}
	mp.mu.Lock()
	sid := mp.serviceID
	mp.mu.Unlock()
	g.emit("service-preview:pipe-dead", map[string]any{
		"gateway_id":    g.id.String(),
		"mapping_index": mp.index,
		"path_prefix":   mp.prefix,
		"service_id":    sid.String(),
		"port":          int(mp.port),
	})
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
		for _, mp := range g.mappings {
			if mp.listener != nil {
				_ = mp.listener.Close()
			}
			mp.mu.Lock()
			pipe := mp.pipe
			mp.pipe = nil
			mp.mu.Unlock()
			if pipe != nil {
				pipe.close()
			}
		}
	})
}

// acceptLoop runs on the STABLE per-mapping listener and hands each accepted
// browser connection to whatever pipe is currently live. During the dead
// window (pipe torn down, awaiting rebind) the gateway's serveHTTP already
// short-circuits to 503 before the proxy dials, so connections rarely land
// here; any that race in are closed.
func (mp *servicePreviewMapping) acceptLoop() {
	for {
		conn, err := mp.listener.Accept()
		if err != nil {
			return
		}
		mp.mu.Lock()
		pipe := mp.pipe
		dead := mp.dead
		mp.mu.Unlock()
		if dead || pipe == nil || !pipe.acceptConn(conn) {
			_ = conn.Close()
		}
	}
}

// runPipe blocks on the pipe's lifetime, then marks the mapping dead (unless a
// concurrent rebind already replaced the pipe) and fires the pipe-dead event.
func (mp *servicePreviewMapping) runPipe(pipe *servicePreview) {
	pipe.run()
	mp.mu.Lock()
	replaced := mp.pipe != pipe
	if !replaced {
		mp.pipe = nil
		mp.dead = true
	}
	mp.mu.Unlock()
	if replaced {
		return
	}
	mp.gateway.onPipeDead(mp)
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
	errCh := make(chan error, 2)
	go func() { errCh <- p.writeLoop() }()
	go func() { errCh <- p.readLoop() }()
	select {
	case <-p.ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
			logDebug("service-preview", "service=%s closed: %v", p.id, err)
		}
	}
}

func (p *servicePreview) writeLoop() error {
	ticker := time.NewTicker(serviceHostPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(p.ctx, serviceHostWriteWait)
			err := p.ws.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
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

// acceptConn registers a freshly accepted browser connection on this pipe and
// starts pumping it. Returns false (caller closes the conn) if the pipe is at
// capacity, the id space is exhausted, or the send buffer is wedged.
func (p *servicePreview) acceptConn(conn net.Conn) bool {
	p.mu.Lock()
	if len(p.connections) >= 16 {
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
	}
	// Buffer momentarily full: block briefly rather than nuking the pipe on a
	// transient burst. Packets are never silently dropped (a gapped byte stream
	// would corrupt TCP), so only a genuinely stuck writer/peer — enqueue still
	// blocked after serviceEnqueueWait — tears the pipe down.
	timer := time.NewTimer(serviceEnqueueWait)
	defer timer.Stop()
	select {
	case p.send <- msg:
		return true
	case <-p.ctx.Done():
		return false
	case <-timer.C:
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
