package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
)

const peerNativeDirectMaxAttempts = 16

type peerNativeDirectClient struct {
	id               string
	app              *App
	host             *peerRendezvousHost
	ctx              context.Context
	cancel           context.CancelFunc
	sessionID        uuid.UUID
	sinceSeq         uint64
	clientInstanceID string
	remote           rendezvousclient.PeerRoute

	mu            sync.Mutex
	attempt       *peertransport.PionClientAttempt
	channel       *peertransport.PionClientChannel
	config        *peerConfigChannel
	authenticated bool
	stopped       bool
	closeOnce     sync.Once
}

// StartPeerNativeDirect starts one accountless Peer terminal attachment. The
// renderer supplies no route or credential; both are resolved from the active
// encrypted Peer store and the latest authenticated catalog.
func (a *App) StartPeerNativeDirect(req NativeDirectStartRequest) error {
	if a == nil || a.ctx == nil {
		return errors.New("Peer direct client unavailable")
	}
	id, err := uuid.Parse(req.ID)
	if err != nil || id == uuid.Nil {
		return errors.New("invalid Peer direct attempt id")
	}
	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil || sessionID == uuid.Nil {
		return errors.New("invalid Peer direct session id")
	}
	if strings.TrimSpace(req.ClientInstanceID) == "" || len(req.ClientInstanceID) > 128 {
		return errors.New("invalid Peer direct client instance id")
	}
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvousMu.Unlock()
	if lifecycle == nil {
		return rendezvousclient.ErrServiceUnavailable
	}
	lifecycle.mu.Lock()
	host := lifecycle.active
	lifecycle.mu.Unlock()
	if host == nil || host.route == nil {
		return rendezvousclient.ErrServiceUnavailable
	}
	discovered, ok := host.discoveredSession(sessionID)
	if !ok {
		return errors.New("Peer session is no longer discoverable")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	client := &peerNativeDirectClient{
		id: id.String(), app: a, host: host, ctx: ctx, cancel: cancel,
		sessionID: sessionID, sinceSeq: req.SinceSeq,
		clientInstanceID: req.ClientInstanceID, remote: discovered.remote,
	}
	a.peerNativeMu.Lock()
	if a.peerNative == nil {
		a.peerNative = make(map[string]*peerNativeDirectClient)
	}
	if _, exists := a.peerNative[client.id]; exists || len(a.peerNative) >= peerNativeDirectMaxAttempts {
		a.peerNativeMu.Unlock()
		cancel()
		return errors.New("Peer direct attempt limit or duplicate")
	}
	a.peerNative[client.id] = client
	a.peerNativeMu.Unlock()
	go func() {
		if err := client.run(); err != nil {
			client.fail(err)
		}
	}()
	return nil
}

func (a *App) SendPeerNativeDirectFrame(id string, frame []byte) error {
	client := a.peerNativeDirectClient(id)
	if client == nil {
		return errors.New("Peer direct attempt unavailable")
	}
	return client.sendFrame(frame)
}

func (a *App) StopPeerNativeDirect(id string) {
	if client := a.peerNativeDirectClient(id); client != nil {
		client.stop()
	}
}

func (a *App) peerNativeDirectClient(id string) *peerNativeDirectClient {
	a.peerNativeMu.Lock()
	defer a.peerNativeMu.Unlock()
	return a.peerNative[id]
}

func (a *App) stopPeerNativeDirectClients() {
	a.peerNativeMu.Lock()
	clients := make([]*peerNativeDirectClient, 0, len(a.peerNative))
	for _, client := range a.peerNative {
		clients = append(clients, client)
	}
	a.peerNativeMu.Unlock()
	for _, client := range clients {
		client.stop()
	}
}

func (c *peerNativeDirectClient) run() error {
	manager, err := c.app.peerManager()
	if err != nil {
		return rendezvousclient.ErrAuthentication
	}
	state, err := manager.store.Load()
	if err != nil {
		return rendezvousclient.ErrAuthentication
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return rendezvousclient.ErrAuthentication
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return rendezvousclient.ErrAuthentication
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return rendezvousclient.ErrAuthentication
	}
	local := membershipForPeerID(active, identity.PeerID())
	remote := membershipForPeerID(active, c.remote.PeerID)
	if local == nil || remote == nil || !peerMembershipAllowsSession(*local, c.sessionID) ||
		!peerMembershipAllowsSession(*remote, c.sessionID) {
		return rendezvousclient.ErrAuthentication
	}
	attempt, err := c.host.route.Dial(c.ctx, rendezvousclient.ClientAttemptConfig{
		Remote: c.remote, Identity: identity, GenesisToken: state.GenesisToken,
		ClientMembershipToken: local.Token, HostMembershipToken: remote.Token,
		SessionID: c.sessionID, ClientInstanceID: c.clientInstanceID,
		OnAuthenticated: c.onAuthenticated, OnRecord: c.handleRecord,
		OnConfigMessage: c.handleConfigMessage,
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
	if c.stopped {
		c.mu.Unlock()
		_ = attempt.Close()
		return nil
	}
	c.attempt = attempt
	c.mu.Unlock()
	return nil
}

func (c *peerNativeDirectClient) onAuthenticated(channel *peertransport.PionClientChannel) {
	config, err := newPeerConfigChannel(c.app, channel)
	if err != nil {
		_ = channel.Close()
		c.fail(err)
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		_ = channel.Close()
		return
	}
	c.channel = channel
	c.config = config
	c.authenticated = true
	c.mu.Unlock()
	c.emit(NativeDirectEvent{Kind: "authenticated"})
	if err := config.Start(c.ctx); err != nil {
		c.fail(fmt.Errorf("start Peer config channel: %w", err))
	}
}

func (c *peerNativeDirectClient) handleRecord(kind peertransport.RecordKind, payload []byte) {
	switch kind {
	case peertransport.RecordFrame:
		c.emit(NativeDirectEvent{Kind: "frame", FrameBase64: base64.StdEncoding.EncodeToString(payload)})
	case peertransport.RecordDirectReady:
		if len(payload) != 8 {
			c.fail(errors.New("invalid Peer direct ready payload"))
			return
		}
		c.emit(NativeDirectEvent{Kind: "ready", LastReplayedSeq: binary.BigEndian.Uint64(payload)})
	case peertransport.RecordPing:
		if len(payload) != 8 {
			c.fail(errors.New("invalid Peer direct ping payload"))
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
			c.fail(errors.New("invalid Peer direct pong payload"))
		}
	case peertransport.RecordClose:
		c.fail(errors.New("Peer direct host closed route"))
	default:
		c.fail(fmt.Errorf("unsupported Peer direct record kind %d", kind))
	}
}

func (c *peerNativeDirectClient) handleConfigMessage(kind peertransport.RecordKind, payload []byte) error {
	c.mu.Lock()
	config := c.config
	c.mu.Unlock()
	if config == nil {
		return errors.New("Peer config channel unavailable")
	}
	return config.Handle(c.ctx, kind, payload)
}

func (c *peerNativeDirectClient) sendFrame(frame []byte) error {
	c.mu.Lock()
	channel := c.channel
	authenticated := c.authenticated
	stopped := c.stopped
	c.mu.Unlock()
	if stopped || !authenticated || channel == nil {
		return errors.New("Peer direct channel unavailable")
	}
	return channel.SendFrame(c.ctx, frame)
}

func (c *peerNativeDirectClient) emit(event NativeDirectEvent) {
	if c.app.eventsEmitter != nil {
		c.app.eventsEmitter(c.app.ctx, "peer-native-direct:event:"+c.id, event)
	}
}

func (c *peerNativeDirectClient) fail(err error) { c.finish(err, true) }
func (c *peerNativeDirectClient) stop()          { c.finish(nil, false) }

func (c *peerNativeDirectClient) finish(err error, emitFailure bool) {
	var attempt *peertransport.PionClientAttempt
	var channel *peertransport.PionClientChannel
	finished := false
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.stopped = true
		attempt = c.attempt
		channel = c.channel
		c.attempt = nil
		c.channel = nil
		c.config = nil
		c.authenticated = false
		c.mu.Unlock()
		c.cancel()
		c.app.peerNativeMu.Lock()
		delete(c.app.peerNative, c.id)
		c.app.peerNativeMu.Unlock()
		finished = true
	})
	if !finished {
		return
	}
	// Pion invokes OnClosed synchronously from Close. Keep that callback
	// outside closeOnce.Do so its recursive finish call observes completion
	// instead of blocking on the same sync.Once.
	if attempt != nil {
		_ = attempt.Close()
	} else if channel != nil {
		_ = channel.Close()
	}
	if emitFailure && err != nil {
		c.emit(NativeDirectEvent{Kind: "failure", Error: err.Error()})
	}
}
