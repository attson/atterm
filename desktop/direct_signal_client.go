package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/directsignal"
	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"
)

const (
	nativeDirectMaxAttempts = 16
	nativeDirectTimeout     = 30 * time.Second
)

// NativeDirectStartRequest identifies one frontend SessionConnection. Relay
// credentials and account_key are intentionally read from App state in Go.
type NativeDirectStartRequest struct {
	ID               string `json:"id"`
	SessionID        string `json:"session_id"`
	SinceSeq         uint64 `json:"since_seq"`
	ClientInstanceID string `json:"client_instance_id"`
}

// NativeDirectEvent is the small event surface consumed by the Wails adapter.
// Frames use base64 so Wails JSON cannot reinterpret arbitrary bytes as text.
type NativeDirectEvent struct {
	Kind            string `json:"kind"`
	FrameBase64     string `json:"frame_base64,omitempty"`
	LastReplayedSeq uint64 `json:"last_replayed_seq,omitempty"`
	ICEState        string `json:"ice_state,omitempty"`
	CandidateType   string `json:"candidate_type,omitempty"`
	Error           string `json:"error,omitempty"`
}

type nativeDirectClient struct {
	id               string
	app              *App
	ctx              context.Context
	cancel           context.CancelFunc
	relayURL         string
	token            string
	allowInsecure    bool
	sessionID        uuid.UUID
	sinceSeq         uint64
	clientInstanceID string
	accountKey       []byte
	webrtcConfig     webrtc.Configuration

	mu            sync.Mutex
	writeMu       sync.Mutex
	conn          *websocket.Conn
	attempt       *peertransport.PionClientAttempt
	channel       *peertransport.PionClientChannel
	authenticated bool
	stopped       bool
	closeOnce     sync.Once
}

func (a *App) StartNativeDirect(req NativeDirectStartRequest) error {
	if a == nil || a.cfgStore == nil || a.ctx == nil {
		return errors.New("native direct client unavailable")
	}
	id, err := uuid.Parse(req.ID)
	if err != nil || id == uuid.Nil {
		return errors.New("invalid native direct attempt id")
	}
	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil || sessionID == uuid.Nil {
		return errors.New("invalid native direct session id")
	}
	if strings.TrimSpace(req.ClientInstanceID) == "" || len(req.ClientInstanceID) > 128 {
		return errors.New("invalid native direct client instance id")
	}
	cfg := a.cfgStore.Get()
	if !(cfg.DirectP2PEnabled || envEnabled("ATTERM_DIRECT_P2P")) {
		return errors.New("native direct connection preference disabled")
	}
	if cfg.RelaySessionToken == "" || cfg.RelayPaused {
		return errors.New("native direct relay session unavailable")
	}
	relayURL := strings.TrimRight(uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL), "/")
	if err := validateRelayEndpoint(relayURL, cfg.AllowInsecureRelay); err != nil {
		return err
	}
	accountKey := a.accountKeyForSync()
	if len(accountKey) != e2eecrypto.SessionKeySize {
		return errors.New("native direct account key unavailable")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	client := &nativeDirectClient{
		id:               id.String(),
		app:              a,
		ctx:              ctx,
		cancel:           cancel,
		relayURL:         relayURL,
		token:            cfg.RelaySessionToken,
		allowInsecure:    cfg.AllowInsecureRelay,
		sessionID:        sessionID,
		sinceSeq:         req.SinceSeq,
		clientInstanceID: req.ClientInstanceID,
		accountKey:       accountKey,
		webrtcConfig: webrtc.Configuration{ICEServers: []webrtc.ICEServer{{
			URLs: []string{"stun:stun.cloudflare.com:3478"},
		}}},
	}
	a.nativeDirectMu.Lock()
	if a.nativeDirect == nil {
		a.nativeDirect = make(map[string]*nativeDirectClient)
	}
	if _, exists := a.nativeDirect[client.id]; exists || len(a.nativeDirect) >= nativeDirectMaxAttempts {
		a.nativeDirectMu.Unlock()
		cancel()
		clearBytes(accountKey)
		return errors.New("native direct attempt limit or duplicate")
	}
	a.nativeDirect[client.id] = client
	a.nativeDirectMu.Unlock()
	go func() {
		if err := client.run(); err != nil {
			client.fail(err)
		}
	}()
	return nil
}

func (a *App) SendNativeDirectFrame(id string, frame []byte) error {
	client := a.nativeDirectClient(id)
	if client == nil {
		return errors.New("native direct attempt unavailable")
	}
	return client.sendFrame(frame)
}

func (a *App) StopNativeDirect(id string) {
	if client := a.nativeDirectClient(id); client != nil {
		client.stop()
	}
}

func (a *App) nativeDirectClient(id string) *nativeDirectClient {
	a.nativeDirectMu.Lock()
	defer a.nativeDirectMu.Unlock()
	return a.nativeDirect[id]
}

func (a *App) stopNativeDirectClients() {
	a.nativeDirectMu.Lock()
	clients := make([]*nativeDirectClient, 0, len(a.nativeDirect))
	for _, client := range a.nativeDirect {
		clients = append(clients, client)
	}
	a.nativeDirectMu.Unlock()
	for _, client := range clients {
		client.stop()
	}
}

func (c *nativeDirectClient) run() error {
	dialCtx, cancelDial := context.WithTimeout(c.ctx, directHostDialTimeout)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.token)
	opts := &websocket.DialOptions{HTTPHeader: headers}
	if c.allowInsecure {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	conn, _, err := websocket.Dial(dialCtx, c.relayURL+"/direct-signal", opts)
	cancelDial()
	if err != nil {
		return fmt.Errorf("native direct signaling dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(64 * 1024)
	if err := c.sendSignal(directsignal.Message{
		Kind:             "hello",
		Role:             "client",
		ClientInstanceID: c.clientInstanceID,
	}); err != nil {
		return err
	}
	helloCtx, cancelHello := context.WithTimeout(c.ctx, directHostHelloTimeout)
	hello, err := readDirectSignalHost(helloCtx, conn)
	cancelHello()
	if err != nil {
		return err
	}
	if hello.Version != directsignal.Version || hello.Kind != "hello_ok" {
		return errors.New("native direct signaling rejected client hello")
	}
	requestID := uuid.NewString()
	if err := c.sendSignal(directsignal.Message{
		Kind:             "direct_request",
		RequestID:        requestID,
		SessionID:        c.sessionID.String(),
		ClientInstanceID: c.clientInstanceID,
		SinceSeq:         c.sinceSeq,
	}); err != nil {
		return err
	}
	timer := time.AfterFunc(nativeDirectTimeout, func() {
		c.fail(errors.New("native direct attempt timed out"))
	})
	defer timer.Stop()
	for c.ctx.Err() == nil {
		message, err := readDirectSignalHost(c.ctx, conn)
		if err != nil {
			return err
		}
		if message.Version != directsignal.Version {
			continue
		}
		switch message.Kind {
		case "direct_attempt":
			if message.RequestID != requestID {
				return errors.New("native direct request id mismatch")
			}
			if err := c.startAttempt(message); err != nil {
				return err
			}
		case "signal":
			c.mu.Lock()
			attempt := c.attempt
			c.mu.Unlock()
			if attempt == nil {
				return errors.New("native direct signal before authorization")
			}
			if err := attempt.HandleSignal(message.SignalType, message.Payload); err != nil {
				return err
			}
		case "consumed", "host_registered":
		case "cancel":
			return fmt.Errorf("native direct attempt cancelled: %s", message.Code)
		case "error":
			return fmt.Errorf("native direct signaling rejected: %s", message.Code)
		default:
			return fmt.Errorf("unexpected native direct signal: %s", message.Kind)
		}
	}
	return c.ctx.Err()
}

func (c *nativeDirectClient) startAttempt(message directsignal.Message) error {
	c.mu.Lock()
	if c.attempt != nil {
		c.mu.Unlock()
		return errors.New("duplicate native direct attempt")
	}
	c.mu.Unlock()
	attemptID, err := uuid.Parse(message.AttemptID)
	if err != nil {
		return errors.New("invalid native direct attempt authorization")
	}
	ticket, err := base64.RawURLEncoding.DecodeString(message.Ticket)
	if err != nil || len(ticket) != 32 || message.SessionID != c.sessionID.String() ||
		message.ClientInstanceID != c.clientInstanceID || message.ExpiresAtUnixM <= time.Now().UnixMilli() {
		return errors.New("invalid native direct authorization claims")
	}
	permission, ok := directTransportPermission(message.Permission)
	if !ok {
		return errors.New("invalid native direct permission")
	}
	auth, err := peertransport.NewAccountKeyAuthenticator(c.accountKey)
	if err != nil {
		return err
	}
	attempt, err := peertransport.NewPionClientAttempt(c.ctx, peertransport.PionClientConfig{
		Authorization: peertransport.Authorization{
			AttemptID:           attemptID,
			Ticket:              ticket,
			SessionID:           c.sessionID,
			UserID:              message.UserID,
			HostID:              message.HostID,
			ClientInstanceID:    c.clientInstanceID,
			Permission:          permission,
			ExpiresAtUnixMillis: uint64(message.ExpiresAtUnixM),
		},
		Authenticator: auth,
		WebRTC:        c.webrtcConfig,
		SendSignal: func(signalType, payload string) error {
			return c.sendSignal(directsignal.Message{
				Kind:       "signal",
				AttemptID:  attemptID.String(),
				SignalType: signalType,
				Payload:    payload,
			})
		},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			c.mu.Lock()
			if c.stopped {
				c.mu.Unlock()
				_ = channel.Close()
				return
			}
			c.channel = channel
			c.authenticated = true
			c.mu.Unlock()
			clearBytes(c.accountKey)
			c.emit(NativeDirectEvent{Kind: "authenticated"})
		},
		OnRecord: c.handleRecord,
		OnDiagnostics: func(iceState, candidateType string) {
			c.emit(NativeDirectEvent{Kind: "diagnostics", ICEState: iceState, CandidateType: candidateType})
		},
		OnClosed: func(closeErr error) {
			if closeErr != nil && c.ctx.Err() == nil {
				c.fail(closeErr)
			}
		},
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.attempt = attempt
	c.mu.Unlock()
	if err := attempt.Start(); err != nil {
		return err
	}
	return nil
}

func (c *nativeDirectClient) handleRecord(kind peertransport.RecordKind, payload []byte) {
	switch kind {
	case peertransport.RecordFrame:
		c.emit(NativeDirectEvent{Kind: "frame", FrameBase64: base64.StdEncoding.EncodeToString(payload)})
	case peertransport.RecordDirectReady:
		if len(payload) != 8 {
			c.fail(errors.New("invalid native direct ready payload"))
			return
		}
		c.emit(NativeDirectEvent{Kind: "ready", LastReplayedSeq: binary.BigEndian.Uint64(payload)})
	case peertransport.RecordPing:
		if len(payload) != 8 {
			c.fail(errors.New("invalid native direct ping payload"))
			return
		}
		c.mu.Lock()
		channel := c.channel
		c.mu.Unlock()
		if channel != nil {
			if err := channel.SendRecord(c.ctx, peertransport.RecordPong, payload); err != nil {
				c.fail(err)
			}
		}
	case peertransport.RecordPong:
		if len(payload) != 8 {
			c.fail(errors.New("invalid native direct pong payload"))
		}
	case peertransport.RecordClose:
		c.fail(errors.New("native direct peer closed route"))
	default:
		c.fail(fmt.Errorf("unsupported native direct record kind %d", kind))
	}
}

func (c *nativeDirectClient) sendFrame(frame []byte) error {
	c.mu.Lock()
	channel := c.channel
	authenticated := c.authenticated
	stopped := c.stopped
	c.mu.Unlock()
	if stopped || !authenticated || channel == nil {
		return errors.New("native direct channel unavailable")
	}
	return channel.SendFrame(c.ctx, frame)
}

func (c *nativeDirectClient) sendSignal(message directsignal.Message) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("native direct signaling unavailable")
	}
	message.Version = directsignal.Version
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(c.ctx, directHostWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func (c *nativeDirectClient) emit(event NativeDirectEvent) {
	if c.app.eventsEmitter != nil {
		c.app.eventsEmitter(c.app.ctx, "native-direct:event:"+c.id, event)
	}
}

func (c *nativeDirectClient) fail(err error) {
	c.finish(err, true)
}

func (c *nativeDirectClient) stop() {
	c.finish(nil, false)
}

func (c *nativeDirectClient) finish(err error, emitFailure bool) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.stopped = true
		conn := c.conn
		attempt := c.attempt
		authenticated := c.authenticated
		c.channel = nil
		c.mu.Unlock()
		if emitFailure && authenticated && conn != nil {
			_ = c.sendSignal(directsignal.Message{Kind: "direct_result", Code: "route_lost"})
		}
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		c.cancel()
		if attempt != nil {
			_ = attempt.Close()
		}
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}
		clearBytes(c.accountKey)
		c.app.nativeDirectMu.Lock()
		delete(c.app.nativeDirect, c.id)
		c.app.nativeDirectMu.Unlock()
		if emitFailure && err != nil {
			c.emit(NativeDirectEvent{Kind: "failure", Error: err.Error()})
		}
	})
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
