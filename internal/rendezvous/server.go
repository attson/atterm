package rendezvous

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

const (
	defaultMailboxTTL             = 120 * time.Second
	defaultChallengeTTL           = 10 * time.Second
	defaultMaxConnections         = 1024
	defaultMaxConnectionsPerIP    = 32
	defaultMaxConnectionsPerTopic = 32
	defaultMaxMailboxPerTopic     = 128
	defaultMaxMailboxGlobal       = 4096
	defaultMaxRecentMessages      = 8192
	defaultMaxMessagesPerMinute   = 240
	clientSendQueueSize           = 64
	maxWireMessageBytes           = 96 << 10
	hardMaxConnections            = 100_000
	hardMaxConnectionsPerTopic    = 1_024
	hardMaxMailboxMessages        = 1 << 20
	hardMaxMessagesPerMinute      = 100_000
)

// Config bounds all in-memory Rendezvous state. Empty AllowedOrigins is only
// appropriate for explicit loopback development; the production command
// refuses to start without an allow-list.
type Config struct {
	AllowedOrigins            []string
	MailboxTTL                time.Duration
	ChallengeTTL              time.Duration
	MaxConnections            int
	MaxConnectionsPerIP       int
	MaxConnectionsPerTopic    int
	MaxMailboxPerTopic        int
	MaxMailboxGlobal          int
	MaxRecentMessages         int
	MaxMessagesPerMinutePerIP int
	Now                       func() time.Time
}

type normalizedConfig struct {
	allowedOrigins            map[string]struct{}
	mailboxTTL                time.Duration
	challengeTTL              time.Duration
	maxConnections            int
	maxConnectionsPerIP       int
	maxConnectionsPerTopic    int
	maxMailboxPerTopic        int
	maxMailboxGlobal          int
	maxRecentMessages         int
	maxMessagesPerMinutePerIP int
	now                       func() time.Time
}

// Server is an in-memory HTTP/WebSocket handler. Constructing a new Server is
// intentionally equivalent to a process restart: no presence or mailbox state
// is retained.
type Server struct {
	cfg     normalizedConfig
	metrics metrics
	broker  *broker

	admissionMu sync.Mutex
	active      int
	activeByIP  map[string]int
	rateByIP    map[string]rateWindow
	connections map[*websocket.Conn]struct{}
	closed      bool
}

type rateWindow struct {
	minute int64
	count  int
}

type peerClient struct {
	ctx        context.Context
	cancel     context.CancelFunc
	conn       *websocket.Conn
	ip         string
	topic      string
	presenceID string
	role       Role
	ready      bool
	send       chan []byte
}

type mailboxMessage struct {
	event     EventMessage
	expiresAt time.Time
}

type recentMessage struct {
	state     string
	expiresAt time.Time
}

type broker struct {
	mu sync.Mutex

	cfg              normalizedConfig
	metrics          *metrics
	topics           map[string]map[string]*peerClient
	mailboxes        map[string]map[string][]mailboxMessage
	mailCountByTopic map[string]int
	recent           map[string]recentMessage
	mailCount        int
}

// New validates configuration and returns an empty stateless service.
func New(cfg Config) (*Server, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg: normalized, activeByIP: make(map[string]int), rateByIP: make(map[string]rateWindow),
		connections: make(map[*websocket.Conn]struct{}),
	}
	s.broker = &broker{
		cfg: normalized, metrics: &s.metrics,
		topics:           make(map[string]map[string]*peerClient),
		mailboxes:        make(map[string]map[string][]mailboxMessage),
		mailCountByTopic: make(map[string]int),
		recent:           make(map[string]recentMessage),
	}
	return s, nil
}

// Close disconnects every active WebSocket. Mailboxes disappear with the
// Server value, preserving the service's stateless restart contract.
func (s *Server) Close() {
	s.admissionMu.Lock()
	s.closed = true
	connections := make([]*websocket.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.admissionMu.Unlock()

	clients := s.broker.clients()
	for _, client := range clients {
		client.cancel()
	}
	for _, conn := range connections {
		_ = conn.CloseNow()
	}
}

// ServeHTTP exposes only the versioned WebSocket, health, and payload-free
// metrics surfaces.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case HealthPath:
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Status          string `json:"status"`
			ProtocolVersion int    `json:"protocol_version"`
		}{Status: "ok", ProtocolVersion: Version})
	case MetricsPath:
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.broker.purgeExpired(s.cfg.now())
		s.metrics.serveHTTP(w)
	case ConnectPath:
		s.serveConnect(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !offersSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), Subprotocol) {
		http.Error(w, "websocket subprotocol required", http.StatusUpgradeRequired)
		return
	}
	if !s.originAllowed(r.Header.Get("Origin")) {
		s.metrics.rejectedTotal.Add(1)
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	ip := remoteIP(r.RemoteAddr)
	if !s.acquire(ip) {
		s.metrics.rejectedTotal.Add(1)
		http.Error(w, "connection capacity reached", http.StatusTooManyRequests)
		return
	}
	defer s.release(ip)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol}, InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	if !s.trackConnection(conn) {
		_ = conn.CloseNow()
		return
	}
	defer s.untrackConnection(conn)
	conn.SetReadLimit(maxWireMessageBytes)
	defer conn.Close(websocket.StatusInternalError, "")
	s.metrics.acceptedTotal.Add(1)

	client, pending, err := s.authenticate(r.Context(), conn, ip)
	if err != nil {
		return
	}
	defer func() {
		client.cancel()
		s.broker.unregister(client)
	}()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		client.writeLoop()
	}()
	for _, event := range pending {
		if !client.enqueueWait(event) {
			break
		}
	}
	client.readLoop(s)
	client.cancel()
	<-writerDone
}

func (s *Server) authenticate(parent context.Context, conn *websocket.Conn, ip string) (*peerClient, []EventMessage, error) {
	challengeBytes := make([]byte, 32)
	if _, err := rand.Read(challengeBytes); err != nil {
		return nil, nil, fmt.Errorf("rendezvous: create challenge: %w", err)
	}
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes)
	expiresAt := s.cfg.now().Add(s.cfg.challengeTTL)
	handshakeCtx, cancel := context.WithTimeout(parent, s.cfg.challengeTTL)
	defer cancel()
	if err := writeEvent(handshakeCtx, conn, EventMessage{
		Version: Version, Kind: KindChallenge, Challenge: challenge, ExpiresAt: expiresAt.Unix(),
	}); err != nil {
		return nil, nil, err
	}
	messageType, payload, err := conn.Read(handshakeCtx)
	if err != nil || messageType != websocket.MessageText {
		s.rejectWebSocket(handshakeCtx, conn, CodeUnauthorized)
		return nil, nil, errors.New("rendezvous: registration required")
	}
	var request RegisterMessage
	if s.cfg.now().After(expiresAt) || decodeStrictJSON(payload, &request) != nil {
		s.rejectWebSocket(handshakeCtx, conn, CodeUnauthorized)
		return nil, nil, errors.New("rendezvous: invalid registration")
	}
	if err := verifyRegistration(request, challenge); err != nil {
		s.rejectWebSocket(handshakeCtx, conn, CodeUnauthorized)
		return nil, nil, err
	}
	ctx, cancelClient := context.WithCancel(parent)
	client := &peerClient{
		ctx: ctx, cancel: cancelClient, conn: conn, ip: ip,
		topic: request.Topic, presenceID: request.PresenceID, role: request.Role,
		send: make(chan []byte, clientSendQueueSize),
	}
	existing, code := s.broker.register(client)
	if code != "" {
		cancelClient()
		s.rejectWebSocket(handshakeCtx, conn, code)
		return nil, nil, errors.New("rendezvous: registration capacity rejected")
	}
	if err := writeEvent(handshakeCtx, conn, EventMessage{
		Version: Version, Kind: KindRegistered, Presence: existing,
	}); err != nil {
		cancelClient()
		s.broker.unregister(client)
		return nil, nil, err
	}
	pending := s.broker.activate(client, existing)
	return client, pending, nil
}

func (s *Server) rejectWebSocket(ctx context.Context, conn *websocket.Conn, code string) {
	s.metrics.rejectedTotal.Add(1)
	_ = writeEvent(ctx, conn, EventMessage{Version: Version, Kind: KindError, Code: code})
	_ = conn.Close(websocket.StatusPolicyViolation, code)
}

func (c *peerClient) readLoop(server *Server) {
	for {
		messageType, payload, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			if !c.enqueue(EventMessage{Version: Version, Kind: KindError, Code: CodeInvalidMessage}) {
				return
			}
			continue
		}
		var header struct {
			Version int    `json:"v"`
			Kind    string `json:"kind"`
		}
		if json.Unmarshal(payload, &header) != nil || header.Version != Version || header.Kind != KindPublish {
			if !c.enqueue(EventMessage{Version: Version, Kind: KindError, Code: CodeInvalidMessage}) {
				return
			}
			continue
		}
		var publish PublishMessage
		if decodeStrictJSON(payload, &publish) != nil {
			if !c.enqueue(EventMessage{Version: Version, Kind: KindError, Code: CodeInvalidMessage}) {
				return
			}
			continue
		}
		if !server.allowMessage(c.ip) {
			if !c.enqueue(EventMessage{Version: Version, Kind: KindError, Code: CodeRateLimited, MessageID: publish.MessageID}) {
				return
			}
			continue
		}
		state, code := server.broker.publish(c, publish, server.cfg.now())
		if code != "" {
			if !c.enqueue(EventMessage{Version: Version, Kind: KindError, Code: code, MessageID: publish.MessageID}) {
				return
			}
			continue
		}
		if !c.enqueue(EventMessage{Version: Version, Kind: KindAck, MessageID: publish.MessageID, State: state}) {
			return
		}
	}
}

func (c *peerClient) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case payload := <-c.send:
			if err := c.conn.Write(c.ctx, websocket.MessageText, payload); err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (c *peerClient) enqueue(event EventMessage) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		return false
	}
	select {
	case <-c.ctx.Done():
		return false
	case c.send <- payload:
		return true
	default:
		return false
	}
}

func (c *peerClient) enqueueWait(event EventMessage) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		return false
	}
	select {
	case <-c.ctx.Done():
		return false
	case c.send <- payload:
		return true
	}
}

func (b *broker) register(client *peerClient) ([]Presence, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.purgeExpiredLocked(b.cfg.now())
	peers := b.topics[client.topic]
	if peers == nil {
		peers = make(map[string]*peerClient)
		b.topics[client.topic] = peers
	}
	if _, exists := peers[client.presenceID]; exists {
		return nil, CodePresenceConflict
	}
	if len(peers) >= b.cfg.maxConnectionsPerTopic {
		return nil, CodeTopicCapacity
	}
	existing := make([]Presence, 0, len(peers))
	for _, peer := range peers {
		if !peer.ready {
			continue
		}
		existing = append(existing, Presence{PresenceID: peer.presenceID, Role: peer.role})
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].PresenceID < existing[j].PresenceID })
	peers[client.presenceID] = client
	return existing, ""
}

func (b *broker) activate(client *peerClient, announced []Presence) []EventMessage {
	b.mu.Lock()
	peers := b.topics[client.topic]
	if peers == nil || peers[client.presenceID] != client || client.ready {
		b.mu.Unlock()
		return nil
	}
	client.ready = true
	mailbox := b.takeMailboxLocked(client.topic, client.presenceID)
	announcedIDs := make(map[string]struct{}, len(announced))
	for _, presence := range announced {
		announcedIDs[presence.PresenceID] = struct{}{}
	}
	pending := make([]EventMessage, 0, len(mailbox)+len(peers))
	recipients := make([]*peerClient, 0, len(peers))
	for _, peer := range peers {
		if peer != client && peer.ready {
			recipients = append(recipients, peer)
			if _, alreadyAnnounced := announcedIDs[peer.presenceID]; !alreadyAnnounced {
				pending = append(pending, EventMessage{
					Version: Version, Kind: KindPresence, Event: PresenceOnline,
					PresenceID: peer.presenceID, Role: peer.role,
				})
			}
		}
	}
	pending = append(pending, mailbox...)
	b.metrics.registeredPeers.Add(1)
	b.mu.Unlock()

	event := EventMessage{
		Version: Version, Kind: KindPresence, Event: PresenceOnline,
		PresenceID: client.presenceID, Role: client.role,
	}
	for _, recipient := range recipients {
		recipient.enqueue(event)
	}
	return pending
}

func (b *broker) unregister(client *peerClient) {
	b.mu.Lock()
	peers := b.topics[client.topic]
	if peers == nil || peers[client.presenceID] != client {
		b.mu.Unlock()
		return
	}
	delete(peers, client.presenceID)
	recipients := make([]*peerClient, 0, len(peers))
	if client.ready {
		for _, peer := range peers {
			if peer.ready {
				recipients = append(recipients, peer)
			}
		}
	}
	if len(peers) == 0 {
		delete(b.topics, client.topic)
	}
	if client.ready {
		b.metrics.registeredPeers.Add(-1)
	}
	b.mu.Unlock()

	event := EventMessage{
		Version: Version, Kind: KindPresence, Event: PresenceOffline,
		PresenceID: client.presenceID, Role: client.role,
	}
	for _, recipient := range recipients {
		recipient.enqueue(event)
	}
}

func (b *broker) publish(sender *peerClient, message PublishMessage, now time.Time) (string, string) {
	if message.Version != Version || message.Kind != KindPublish || message.To == sender.presenceID {
		return "", CodeInvalidMessage
	}
	if _, err := decodeExact(message.MessageID, 16, "message id"); err != nil {
		return "", CodeInvalidMessage
	}
	if _, err := decodeExact(message.To, 32, "target presence id"); err != nil {
		return "", CodeInvalidMessage
	}
	payload, err := base64.RawURLEncoding.DecodeString(message.Payload)
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != message.Payload {
		return "", CodeInvalidMessage
	}
	if len(payload) > MaxPayloadBytes {
		return "", CodeMessageTooLarge
	}
	event := EventMessage{
		Version: Version, Kind: KindSignal, MessageID: message.MessageID,
		From: sender.presenceID, Payload: message.Payload, StoredAt: now.Unix(),
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.purgeExpiredLocked(now)
	recentKey := sender.topic + "\x00" + sender.presenceID + "\x00" + message.MessageID
	if recent, ok := b.recent[recentKey]; ok {
		return recent.state, ""
	}
	if len(b.recent) >= b.cfg.maxRecentMessages {
		return "", CodeServerCapacity
	}
	if recipient := b.topics[sender.topic][message.To]; recipient != nil && recipient.ready && recipient.enqueue(event) {
		b.recent[recentKey] = recentMessage{state: DeliveryDelivered, expiresAt: now.Add(b.cfg.mailboxTTL)}
		b.metrics.forwardedTotal.Add(1)
		return DeliveryDelivered, ""
	}
	if b.mailCountByTopic[sender.topic] >= b.cfg.maxMailboxPerTopic || b.mailCount >= b.cfg.maxMailboxGlobal {
		return "", CodeMailboxCapacity
	}
	byTarget := b.mailboxes[sender.topic]
	if byTarget == nil {
		byTarget = make(map[string][]mailboxMessage)
		b.mailboxes[sender.topic] = byTarget
	}
	byTarget[message.To] = append(byTarget[message.To], mailboxMessage{
		event: event, expiresAt: now.Add(b.cfg.mailboxTTL),
	})
	b.mailCount++
	b.mailCountByTopic[sender.topic]++
	b.recent[recentKey] = recentMessage{state: DeliveryQueued, expiresAt: now.Add(b.cfg.mailboxTTL)}
	b.metrics.mailboxMessages.Add(1)
	b.metrics.queuedTotal.Add(1)
	return DeliveryQueued, ""
}

func (b *broker) takeMailboxLocked(topic, presenceID string) []EventMessage {
	byTarget := b.mailboxes[topic]
	queued := byTarget[presenceID]
	if len(queued) == 0 {
		return nil
	}
	out := make([]EventMessage, 0, len(queued))
	for _, message := range queued {
		out = append(out, message.event)
	}
	delete(byTarget, presenceID)
	if len(byTarget) == 0 {
		delete(b.mailboxes, topic)
	}
	b.mailCount -= len(queued)
	b.mailCountByTopic[topic] -= len(queued)
	if b.mailCountByTopic[topic] == 0 {
		delete(b.mailCountByTopic, topic)
	}
	b.metrics.mailboxMessages.Add(int64(-len(queued)))
	return out
}

func (b *broker) purgeExpired(now time.Time) {
	b.mu.Lock()
	b.purgeExpiredLocked(now)
	b.mu.Unlock()
}

func (b *broker) clients() []*peerClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	var clients []*peerClient
	for _, peers := range b.topics {
		for _, client := range peers {
			clients = append(clients, client)
		}
	}
	return clients
}

func (b *broker) purgeExpiredLocked(now time.Time) {
	for key, recent := range b.recent {
		if !now.Before(recent.expiresAt) {
			delete(b.recent, key)
		}
	}
	for topic, byTarget := range b.mailboxes {
		for target, queued := range byTarget {
			kept := queued[:0]
			for _, message := range queued {
				if now.Before(message.expiresAt) {
					kept = append(kept, message)
					continue
				}
				b.mailCount--
				b.mailCountByTopic[topic]--
				b.metrics.mailboxMessages.Add(-1)
				b.metrics.expiredTotal.Add(1)
			}
			if len(kept) == 0 {
				delete(byTarget, target)
			} else {
				byTarget[target] = kept
			}
		}
		if len(byTarget) == 0 {
			delete(b.mailboxes, topic)
			delete(b.mailCountByTopic, topic)
		}
	}
}

func (s *Server) acquire(ip string) bool {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	if s.closed || s.active >= s.cfg.maxConnections || s.activeByIP[ip] >= s.cfg.maxConnectionsPerIP {
		return false
	}
	s.active++
	s.activeByIP[ip]++
	s.metrics.activeConnections.Add(1)
	return true
}

func (s *Server) trackConnection(conn *websocket.Conn) bool {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	if s.closed {
		return false
	}
	s.connections[conn] = struct{}{}
	return true
}

func (s *Server) untrackConnection(conn *websocket.Conn) {
	s.admissionMu.Lock()
	delete(s.connections, conn)
	s.admissionMu.Unlock()
}

func (s *Server) release(ip string) {
	s.admissionMu.Lock()
	s.active--
	s.activeByIP[ip]--
	if s.activeByIP[ip] == 0 {
		delete(s.activeByIP, ip)
	}
	s.admissionMu.Unlock()
	s.metrics.activeConnections.Add(-1)
}

func (s *Server) allowMessage(ip string) bool {
	nowMinute := s.cfg.now().Unix() / 60
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	window, exists := s.rateByIP[ip]
	if !exists && len(s.rateByIP) >= s.cfg.maxConnections*4 {
		for candidate, candidateWindow := range s.rateByIP {
			if candidateWindow.minute != nowMinute {
				delete(s.rateByIP, candidate)
			}
		}
		if len(s.rateByIP) >= s.cfg.maxConnections*4 {
			return false
		}
	}
	if window.minute != nowMinute {
		window = rateWindow{minute: nowMinute}
	}
	if window.count >= s.cfg.maxMessagesPerMinutePerIP {
		s.rateByIP[ip] = window
		return false
	}
	window.count++
	s.rateByIP[ip] = window
	return true
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" || len(s.cfg.allowedOrigins) == 0 {
		return true
	}
	canonical, err := canonicalOrigin(origin)
	if err != nil {
		return false
	}
	_, ok := s.cfg.allowedOrigins[canonical]
	return ok
}

func normalizeConfig(cfg Config) (normalizedConfig, error) {
	applyDefault := func(value, fallback int) int {
		if value == 0 {
			return fallback
		}
		return value
	}
	if cfg.MailboxTTL == 0 {
		cfg.MailboxTTL = defaultMailboxTTL
	}
	if cfg.ChallengeTTL == 0 {
		cfg.ChallengeTTL = defaultChallengeTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	values := []*int{
		&cfg.MaxConnections, &cfg.MaxConnectionsPerIP, &cfg.MaxConnectionsPerTopic,
		&cfg.MaxMailboxPerTopic, &cfg.MaxMailboxGlobal, &cfg.MaxRecentMessages, &cfg.MaxMessagesPerMinutePerIP,
	}
	for _, value := range values {
		if *value < 0 {
			return normalizedConfig{}, errors.New("rendezvous: limits must be positive")
		}
	}
	if cfg.MailboxTTL <= 0 || cfg.ChallengeTTL <= 0 {
		return normalizedConfig{}, errors.New("rendezvous: timeouts must be positive")
	}
	maxConnections := applyDefault(cfg.MaxConnections, defaultMaxConnections)
	maxConnectionsPerIP := applyDefault(cfg.MaxConnectionsPerIP, defaultMaxConnectionsPerIP)
	maxConnectionsPerTopic := applyDefault(cfg.MaxConnectionsPerTopic, defaultMaxConnectionsPerTopic)
	maxMailboxPerTopic := applyDefault(cfg.MaxMailboxPerTopic, defaultMaxMailboxPerTopic)
	maxMailboxGlobal := applyDefault(cfg.MaxMailboxGlobal, defaultMaxMailboxGlobal)
	maxRecentMessages := applyDefault(cfg.MaxRecentMessages, defaultMaxRecentMessages)
	maxMessagesPerMinutePerIP := applyDefault(cfg.MaxMessagesPerMinutePerIP, defaultMaxMessagesPerMinute)
	if maxConnections > hardMaxConnections ||
		maxConnectionsPerIP > hardMaxConnections ||
		maxConnectionsPerTopic > hardMaxConnectionsPerTopic ||
		maxMailboxPerTopic > hardMaxMailboxMessages ||
		maxMailboxGlobal > hardMaxMailboxMessages ||
		maxRecentMessages > hardMaxMailboxMessages ||
		maxMessagesPerMinutePerIP > hardMaxMessagesPerMinute {
		return normalizedConfig{}, errors.New("rendezvous: configured limit exceeds hard maximum")
	}
	if maxConnectionsPerIP > maxConnections {
		return normalizedConfig{}, errors.New("rendezvous: per-IP connection limit exceeds global connection limit")
	}
	if maxConnectionsPerTopic > maxConnections {
		return normalizedConfig{}, errors.New("rendezvous: per-topic connection limit exceeds global connection limit")
	}
	if maxMailboxPerTopic > maxMailboxGlobal {
		return normalizedConfig{}, errors.New("rendezvous: per-topic mailbox limit exceeds global mailbox limit")
	}
	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		canonical, err := canonicalOrigin(origin)
		if err != nil {
			return normalizedConfig{}, fmt.Errorf("rendezvous: invalid allowed origin %q: %w", origin, err)
		}
		origins[canonical] = struct{}{}
	}
	return normalizedConfig{
		allowedOrigins: origins, mailboxTTL: cfg.MailboxTTL, challengeTTL: cfg.ChallengeTTL,
		maxConnections:            maxConnections,
		maxConnectionsPerIP:       maxConnectionsPerIP,
		maxConnectionsPerTopic:    maxConnectionsPerTopic,
		maxMailboxPerTopic:        maxMailboxPerTopic,
		maxMailboxGlobal:          maxMailboxGlobal,
		maxRecentMessages:         maxRecentMessages,
		maxMessagesPerMinutePerIP: maxMessagesPerMinutePerIP,
		now:                       cfg.Now,
	}, nil
}

func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("origin must contain only scheme and host")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func offersSubprotocol(header, wanted string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	return remoteAddr
}

func decodeStrictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("rendezvous: trailing JSON")
	}
	return nil
}

func writeEvent(ctx context.Context, conn *websocket.Conn, event EventMessage) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("rendezvous: encode event: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("rendezvous: write event: %w", err)
	}
	return nil
}
