package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/directsignal"
	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"
)

const (
	directHostSignalQueueDepth = 128
	directHostDialTimeout      = 10 * time.Second
	directHostWriteTimeout     = 10 * time.Second
	directHostHelloTimeout     = 10 * time.Second
	directStatsReportThreshold = 256 << 10
	directStatsReportInterval  = 5 * time.Second
)

type directSignalHost struct {
	relayURL      string
	token         string
	permission    string
	host          *relayHost
	accountKey    func() []byte
	allowInsecure bool
	webrtcConfig  webrtc.Configuration

	mu       sync.Mutex
	attempts map[uuid.UUID]*directHostAttempt
}

type directHostAttempt struct {
	id                  uuid.UUID
	sessionID           uuid.UUID
	sinceSeq            uint64
	permission          string
	clientInstanceID    string
	host                *directSignalHost
	mu                  sync.Mutex
	transport           *peertransport.PionHostAttempt
	channel             *peertransport.PionHostChannel
	sub                 *session.Subscriber
	subscribedSession   *session.Session
	streamCancel        context.CancelFunc
	consumed            bool
	closed              bool
	closeOnce           sync.Once
	reportBytes         func(uint64, uint64) error
	statsReportInterval time.Duration
	pendingBytesSent    uint64
	pendingBytesRecv    uint64
}

func newDirectSignalHost(relayURL, token, permission string, host *relayHost, accountKey func() []byte, allowInsecure bool) *directSignalHost {
	return &directSignalHost{
		relayURL:      strings.TrimRight(relayURL, "/"),
		token:         token,
		permission:    normalizeRemotePermission(permission),
		host:          host,
		accountKey:    accountKey,
		allowInsecure: allowInsecure,
		webrtcConfig: webrtc.Configuration{ICEServers: []webrtc.ICEServer{{
			URLs: []string{"stun:stun.cloudflare.com:3478"},
		}}},
		attempts: make(map[uuid.UUID]*directHostAttempt),
	}
}

func (h *directSignalHost) Run(ctx context.Context) {
	defer h.closeAllAttempts()
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		err := h.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logWarn("direct", "signaling host disconnected: %v (retry in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 8*time.Second {
			backoff = 8 * time.Second
		}
	}
}

func (h *directSignalHost) runOnce(ctx context.Context) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, directHostDialTimeout)
	headers := http.Header{}
	if h.token != "" {
		headers.Set("Authorization", "Bearer "+h.token)
	}
	opts := &websocket.DialOptions{HTTPHeader: headers}
	if h.allowInsecure {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	conn, _, err := websocket.Dial(dialCtx, h.relayURL+"/direct-signal", opts)
	cancelDial()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(64 * 1024)
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	defer h.closePendingAttempts()

	hostID, _, _ := h.host.HostMeta()
	hello := directsignal.Message{
		Version:    directsignal.Version,
		Kind:       "hello",
		Role:       "host",
		HostID:     hostID,
		SessionIDs: h.sessionIDs(),
	}
	if err := writeDirectSignalHost(connCtx, conn, hello); err != nil {
		return err
	}
	helloCtx, cancelHello := context.WithTimeout(connCtx, directHostHelloTimeout)
	response, err := readDirectSignalHost(helloCtx, conn)
	cancelHello()
	if err != nil {
		return err
	}
	if response.Version != directsignal.Version || response.Kind != "hello_ok" {
		return fmt.Errorf("direct signaling rejected host hello")
	}

	out := make(chan directsignal.Message, directHostSignalQueueDepth)
	send := func(message directsignal.Message) error {
		message.Version = directsignal.Version
		select {
		case out <- message:
			return nil
		case <-connCtx.Done():
			return connCtx.Err()
		default:
			cancelConn()
			return errors.New("direct signaling queue full")
		}
	}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-connCtx.Done():
				return
			case message := <-out:
				if err := writeDirectSignalHost(connCtx, conn, message); err != nil {
					cancelConn()
					return
				}
			}
		}
	}()

	changes := h.host.server.Registry().SubscribeChanges()
	defer changes.Close()
	go func() {
		for {
			select {
			case <-connCtx.Done():
				return
			case <-changes.C():
				if err := send(directsignal.Message{
					Kind:       "host_register",
					RequestID:  uuid.NewString(),
					HostID:     hostID,
					SessionIDs: h.sessionIDs(),
				}); err != nil {
					return
				}
			}
		}
	}()

	logInfo("direct", "signaling host registered (%d session(s))", len(hello.SessionIDs))
	for connCtx.Err() == nil {
		message, err := readDirectSignalHost(connCtx, conn)
		if err != nil {
			cancelConn()
			<-writerDone
			return err
		}
		if message.Version != directsignal.Version {
			continue
		}
		switch message.Kind {
		case "direct_attempt":
			if err := h.startAttempt(ctx, message, send); err != nil {
				logWarn("direct", "attempt rejected id=%s: %v", message.AttemptID, err)
				_ = send(directsignal.Message{Kind: "cancel", AttemptID: message.AttemptID, Code: "direct_unauthorized"})
			}
		case "signal":
			if err := h.handleSignal(message); err != nil {
				logWarn("direct", "signal rejected id=%s type=%s: %v", message.AttemptID, message.SignalType, err)
				_ = send(directsignal.Message{Kind: "cancel", AttemptID: message.AttemptID, Code: "ice_failed"})
				h.removeAttemptString(message.AttemptID)
			}
		case "cancel":
			h.removeAttemptString(message.AttemptID)
		case "host_registered", "error":
			// Registration ACKs and per-request errors do not affect the uplink.
		default:
			logWarn("direct", "unexpected signaling message kind=%q", message.Kind)
		}
	}
	<-writerDone
	return connCtx.Err()
}

func (h *directSignalHost) sessionIDs() []string {
	snapshot := h.host.Snapshot()
	ids := make([]string, 0, len(snapshot))
	for _, info := range snapshot {
		if id, err := uuid.Parse(info.ID); err == nil {
			ids = append(ids, id.String())
		}
	}
	sort.Strings(ids)
	return ids
}

func (h *directSignalHost) startAttempt(ctx context.Context, message directsignal.Message, send func(directsignal.Message) error) error {
	attemptID, err := uuid.Parse(message.AttemptID)
	if err != nil {
		return errors.New("invalid attempt id")
	}
	sessionID, err := uuid.Parse(message.SessionID)
	if err != nil {
		return errors.New("invalid session id")
	}
	ticket, err := base64.RawURLEncoding.DecodeString(message.Ticket)
	if err != nil || len(ticket) != 32 {
		return errors.New("invalid direct ticket")
	}
	hostID, _, _ := h.host.HostMeta()
	if message.HostID != hostID || message.Permission != h.permission || message.ExpiresAtUnixM <= time.Now().UnixMilli() {
		return errors.New("direct claims no longer match host policy")
	}
	if sess, ok := h.host.server.Registry().Get(sessionID); !ok || sess.Info().HostID != hostID {
		return errors.New("session is no longer local")
	}
	key := []byte(nil)
	if h.accountKey != nil {
		key = h.accountKey()
	}
	if len(key) != e2eecrypto.SessionKeySize {
		return errors.New("account key is not unlocked")
	}
	auth, err := peertransport.NewAccountKeyAuthenticator(key)
	if err != nil {
		return err
	}
	permission, ok := directTransportPermission(message.Permission)
	if !ok {
		return errors.New("invalid direct permission")
	}
	entry := &directHostAttempt{
		id:                  attemptID,
		sessionID:           sessionID,
		sinceSeq:            message.SinceSeq,
		permission:          message.Permission,
		clientInstanceID:    message.ClientInstanceID,
		host:                h,
		statsReportInterval: directStatsReportInterval,
		reportBytes: func(sent, received uint64) error {
			return send(directsignal.Message{
				Kind:          "direct_stats",
				BytesAvoided:  sent + received,
				BytesSent:     sent,
				BytesReceived: received,
			})
		},
	}
	h.mu.Lock()
	if _, exists := h.attempts[attemptID]; exists || len(h.attempts) >= 8 {
		h.mu.Unlock()
		return errors.New("direct attempt limit or duplicate")
	}
	h.attempts[attemptID] = entry
	h.mu.Unlock()

	transport, err := peertransport.NewPionHostAttempt(ctx, peertransport.PionHostConfig{
		Authorization: peertransport.Authorization{
			AttemptID:           attemptID,
			Ticket:              ticket,
			SessionID:           sessionID,
			UserID:              message.UserID,
			HostID:              message.HostID,
			ClientInstanceID:    message.ClientInstanceID,
			Permission:          permission,
			ExpiresAtUnixMillis: uint64(message.ExpiresAtUnixM),
		},
		Authenticator: auth,
		WebRTC:        h.webrtcConfig,
		SendSignal: func(signalType, payload string) error {
			return send(directsignal.Message{Kind: "signal", AttemptID: attemptID.String(), SignalType: signalType, Payload: payload})
		},
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if !entry.setAuthenticatedChannel(channel) {
				_ = channel.Close()
				return
			}
			if err := send(directsignal.Message{Kind: "consumed", AttemptID: attemptID.String()}); err != nil {
				h.removeAttemptIf(attemptID, entry)
				return
			}
			if err := entry.startStream(ctx, channel); err != nil {
				logWarn("direct", "stream start failed id=%s session=%s: %v", attemptID, sessionID, err)
				h.removeAttemptIf(attemptID, entry)
			}
		},
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			if err := entry.handleRecord(ctx, kind, payload); err != nil {
				logWarn("direct", "record rejected id=%s session=%s: %v", attemptID, sessionID, err)
				go h.removeAttempt(attemptID)
			}
		},
		OnClosed: func(error) {
			h.removeAttemptIf(attemptID, entry)
		},
	})
	if err != nil {
		h.removeAttemptIf(attemptID, entry)
		return err
	}
	if !entry.setTransport(transport) {
		_ = transport.Close()
		return errors.New("direct attempt closed during setup")
	}
	return nil
}

func (h *directSignalHost) handleSignal(message directsignal.Message) error {
	id, err := uuid.Parse(message.AttemptID)
	if err != nil {
		return errors.New("invalid attempt id")
	}
	h.mu.Lock()
	attempt := h.attempts[id]
	h.mu.Unlock()
	if attempt == nil {
		return errors.New("direct attempt unavailable")
	}
	attempt.mu.Lock()
	transport := attempt.transport
	closed := attempt.closed
	attempt.mu.Unlock()
	if transport == nil || closed {
		return errors.New("direct attempt unavailable")
	}
	return transport.HandleSignal(message.SignalType, message.Payload)
}

func (a *directHostAttempt) startStream(parent context.Context, channel *peertransport.PionHostChannel) error {
	sess, ok := a.host.host.server.Registry().Get(a.sessionID)
	if !ok || normalizeRemotePermission(a.permission) != a.host.permission {
		return errors.New("session or permission changed")
	}
	clientID := "direct:" + a.clientInstanceID
	sub, replayToSeq := sess.Subscribe(a.sinceSeq, clientID, a.clientInstanceID, session.WithoutAutoDrive())
	streamCtx, cancel := context.WithCancel(parent)
	a.mu.Lock()
	if a.closed || a.sub != nil {
		a.mu.Unlock()
		cancel()
		sess.Unsubscribe(sub)
		return errors.New("direct attempt closed or already streaming")
	}
	a.sub = sub
	a.subscribedSession = sess
	a.streamCancel = cancel
	a.mu.Unlock()
	go a.reportDirectStats(streamCtx)
	go func() {
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-sub.Done():
				a.host.removeAttempt(a.id)
				return
			case frame, open := <-sub.Out():
				if !open {
					a.host.removeAttempt(a.id)
					return
				}
				if frame.Type == proto.TypeReplayProgress {
					var progress proto.ReplayProgressPayload
					if json.Unmarshal(frame.Payload, &progress) == nil && progress.Phase == proto.ReplayProgressEnd {
						if progress.Seq != replayToSeq {
							logWarn("direct", "replay cursor mismatch session=%s got=%d want=%d", a.sessionID, progress.Seq, replayToSeq)
							a.host.removeAttempt(a.id)
							return
						}
						ready := make([]byte, 8)
						binary.BigEndian.PutUint64(ready, replayToSeq)
						if err := channel.SendRecord(streamCtx, peertransport.RecordDirectReady, ready); err != nil {
							a.host.removeAttempt(a.id)
							return
						}
					}
					continue
				}
				accountKey, ok := a.host.unlockedAccountKey()
				if !ok {
					a.host.removeAttempt(a.id)
					return
				}
				prepared, forward := prepareRemoteSubscriberFrame(frame, func() []byte { return accountKey })
				if !forward {
					continue
				}
				wire := proto.Marshal(prepared)
				if err := channel.SendFrame(streamCtx, wire); err != nil {
					a.host.removeAttempt(a.id)
					return
				}
				a.addDirectBytes(uint64(len(wire)), 0, false)
			}
		}
	}()
	return nil
}

func (a *directHostAttempt) reportDirectStats(ctx context.Context) {
	interval := a.statsReportInterval
	if interval <= 0 {
		interval = directStatsReportInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.addDirectBytes(0, 0, true)
		}
	}
}

func (a *directHostAttempt) handleRecord(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	switch kind {
	case peertransport.RecordPing:
		if len(payload) != 8 {
			return errors.New("invalid direct ping")
		}
		a.mu.Lock()
		channel := a.channel
		a.mu.Unlock()
		if channel == nil {
			return errors.New("direct transport unavailable")
		}
		return channel.SendRecord(ctx, peertransport.RecordPong, payload)
	case peertransport.RecordClose:
		go a.host.removeAttempt(a.id)
		return nil
	case peertransport.RecordFrame:
		frame, err := proto.Unmarshal(payload)
		if err != nil || frame.SessionID != a.sessionID {
			return errors.New("invalid direct terminal frame")
		}
		accountKey, ok := a.host.unlockedAccountKey()
		if !ok {
			return errors.New("account key is not unlocked")
		}
		switch frame.Type {
		case proto.TypeIn, proto.TypeResize:
			if !localFrameAllowedByPermission(a.permission, frame.Type) {
				return errors.New("direct frame exceeds permission")
			}
			a.mu.Lock()
			sub := a.sub
			a.mu.Unlock()
			if sub == nil {
				return errors.New("direct subscriber unavailable")
			}
			sess, ok := a.host.host.server.Registry().Get(a.sessionID)
			if !ok || !sess.IsDriver(sub) {
				return errors.New("direct subscriber is not driver")
			}
			if opened, ok := openInboundFrame(frame, func() []byte { return accountKey }); ok {
				frame = opened
			}
			if err := a.host.host.SendLocalInbound(a.sessionID, frame); err != nil {
				return err
			}
			a.addDirectBytes(0, uint64(len(payload)), false)
			return nil
		case proto.TypeClaimDriver:
			if a.permission == proto.RemotePermissionView {
				return errors.New("driver claim exceeds permission")
			}
			var claim proto.ClaimDriverPayload
			if err := json.Unmarshal(frame.Payload, &claim); err != nil {
				return errors.New("invalid driver claim")
			}
			a.mu.Lock()
			sub := a.sub
			a.mu.Unlock()
			if sub == nil {
				return errors.New("direct subscriber unavailable")
			}
			sess, ok := a.host.host.server.Registry().Get(a.sessionID)
			if !ok {
				return errors.New("session unavailable")
			}
			sess.ClaimDriver(sub, claim.ClientID, claim.ClientName)
			a.addDirectBytes(0, uint64(len(payload)), false)
			return nil
		default:
			return fmt.Errorf("direct frame type 0x%02x is not allowed", frame.Type)
		}
	default:
		return fmt.Errorf("direct record kind %d is not accepted from client", kind)
	}
}

func (h *directSignalHost) unlockedAccountKey() ([]byte, bool) {
	if h.accountKey == nil {
		return nil, false
	}
	key := h.accountKey()
	return key, len(key) == e2eecrypto.SessionKeySize
}

func (h *directSignalHost) removeAttemptString(raw string) {
	if id, err := uuid.Parse(raw); err == nil {
		h.removeAttempt(id)
	}
}

func (h *directSignalHost) removeAttempt(id uuid.UUID) {
	h.mu.Lock()
	attempt := h.attempts[id]
	delete(h.attempts, id)
	h.mu.Unlock()
	if attempt != nil {
		attempt.close()
	}
}

func (h *directSignalHost) removeAttemptIf(id uuid.UUID, expected *directHostAttempt) {
	h.mu.Lock()
	attempt := h.attempts[id]
	if attempt == expected {
		delete(h.attempts, id)
	} else {
		attempt = nil
	}
	h.mu.Unlock()
	if attempt != nil {
		attempt.close()
	}
}

func (h *directSignalHost) closeAllAttempts() {
	h.mu.Lock()
	attempts := make([]*directHostAttempt, 0, len(h.attempts))
	for id, attempt := range h.attempts {
		delete(h.attempts, id)
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	for _, attempt := range attempts {
		attempt.close()
	}
}

func (h *directSignalHost) closePendingAttempts() {
	h.mu.Lock()
	attempts := make([]*directHostAttempt, 0, len(h.attempts))
	for id, attempt := range h.attempts {
		attempt.mu.Lock()
		consumed := attempt.consumed
		attempt.mu.Unlock()
		if !consumed {
			delete(h.attempts, id)
			attempts = append(attempts, attempt)
		}
	}
	h.mu.Unlock()
	for _, attempt := range attempts {
		attempt.close()
	}
}

func (a *directHostAttempt) setTransport(transport *peertransport.PionHostAttempt) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.transport = transport
	return true
}

func (a *directHostAttempt) setAuthenticatedChannel(channel *peertransport.PionHostChannel) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.channel != nil {
		return false
	}
	a.consumed = true
	a.channel = channel
	return true
}

func (a *directHostAttempt) close() {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		cancel := a.streamCancel
		sub := a.sub
		sess := a.subscribedSession
		transport := a.transport
		a.streamCancel = nil
		a.sub = nil
		a.subscribedSession = nil
		a.channel = nil
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if sub != nil && sess != nil {
			sess.Unsubscribe(sub)
		}
		if transport != nil {
			_ = transport.Close()
		}
		a.addDirectBytes(0, 0, true)
	})
}

func (a *directHostAttempt) addDirectBytes(sent, received uint64, force bool) {
	a.mu.Lock()
	a.pendingBytesSent += sent
	a.pendingBytesRecv += received
	if !force && a.pendingBytesSent+a.pendingBytesRecv < directStatsReportThreshold {
		a.mu.Unlock()
		return
	}
	pendingSent := a.pendingBytesSent
	pendingReceived := a.pendingBytesRecv
	a.pendingBytesSent = 0
	a.pendingBytesRecv = 0
	report := a.reportBytes
	a.mu.Unlock()
	if pendingSent+pendingReceived > 0 && report != nil {
		_ = report(pendingSent, pendingReceived)
	}
}

func directTransportPermission(value string) (peertransport.Permission, bool) {
	switch value {
	case proto.RemotePermissionView:
		return peertransport.PermissionView, true
	case proto.RemotePermissionControl:
		return peertransport.PermissionControl, true
	case proto.RemotePermissionFull:
		return peertransport.PermissionFull, true
	default:
		return 0, false
	}
}

func writeDirectSignalHost(ctx context.Context, conn *websocket.Conn, message directsignal.Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, directHostWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func readDirectSignalHost(ctx context.Context, conn *websocket.Conn) (directsignal.Message, error) {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return directsignal.Message{}, err
	}
	if kind != websocket.MessageText || len(data) > 64*1024 {
		return directsignal.Message{}, errors.New("invalid direct signaling message")
	}
	var message directsignal.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return directsignal.Message{}, err
	}
	return message, nil
}
