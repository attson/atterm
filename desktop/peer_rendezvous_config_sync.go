package main

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
)

const peerRendezvousConfigSyncInterval = 3 * time.Second

// peerRendezvousConfigHostAttempt owns an authenticated config-only channel.
// It never resolves a terminal session or creates a subscriber.
type peerRendezvousConfigHostAttempt struct {
	host   *peerRendezvousHost
	id     uuid.UUID
	ctx    context.Context
	cancel context.CancelFunc

	mu               sync.Mutex
	channel          peerQuickTunnelChannel
	config           *peerConfigChannel
	remoteMembership string
	closed           bool
	closeOnce        sync.Once
}

type peerRendezvousConfigClient struct {
	host   *peerRendezvousHost
	remote rendezvousclient.PeerRoute
	ctx    context.Context
	cancel context.CancelFunc

	mu               sync.Mutex
	attempt          *peertransport.PionClientAttempt
	channel          peerQuickTunnelChannel
	config           *peerConfigChannel
	remoteMembership string
	closed           bool
	closeOnce        sync.Once
}

func (h *peerRendezvousHost) authorizeConfig(ctx context.Context, request rendezvousclient.PeerOpenRequest) (rendezvousclient.HostAuthorization, rendezvousclient.HostCallbacks, error) {
	authorization, err := h.runtime.authorizeConfig(request.ClientPeerID)
	if err != nil {
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	attempt := &peerRendezvousConfigHostAttempt{host: h, id: request.AttemptID, ctx: attemptCtx, cancel: cancel}
	h.mu.Lock()
	if _, exists := h.configAttempts[request.AttemptID]; exists {
		h.mu.Unlock()
		cancel()
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
	}
	h.configAttempts[request.AttemptID] = attempt
	h.mu.Unlock()
	callbacks := rendezvousclient.HostCallbacks{
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if err := attempt.start(channel); err != nil {
				logWarn("rendezvous", "Peer config-only host start failed: %v", err)
				go h.removeConfigAttempt(request.AttemptID, true)
			}
		},
		OnRecord: func(peertransport.RecordKind, []byte) {
			logWarn("rendezvous", "terminal record rejected on Peer config-only route")
			go h.removeConfigAttempt(request.AttemptID, true)
		},
		OnConfigMessage: attempt.handleConfigMessage,
		OnClosed: func(error) {
			h.removeConfigAttempt(request.AttemptID, false)
		},
	}
	return rendezvousclient.HostAuthorization{
		Identity: authorization.Identity, GenesisToken: authorization.GenesisToken,
		ClientMembershipToken: authorization.ClientMembershipToken,
		HostMembershipToken:   authorization.HostMembershipToken,
		Permission:            peertransport.PermissionView,
	}, callbacks, nil
}

func (a *peerRendezvousConfigHostAttempt) start(channel peerQuickTunnelChannel) error {
	remoteMembership, ok := channel.RemoteMembershipToken()
	if !ok || remoteMembership == "" {
		return errors.New("Peer config-only host membership is unavailable")
	}
	if err := validatePeerConfigMembership(a.host.app, remoteMembership); err != nil {
		return err
	}
	config, err := newPeerConfigChannel(a.host.app, channel)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.closed || a.channel != nil {
		a.mu.Unlock()
		return errors.New("Peer config-only host attempt is unavailable")
	}
	a.channel = channel
	a.config = config
	a.remoteMembership = remoteMembership
	a.mu.Unlock()
	return config.Start(a.ctx)
}

func (a *peerRendezvousConfigHostAttempt) handleConfigMessage(kind peertransport.RecordKind, payload []byte) error {
	a.mu.Lock()
	config := a.config
	remoteMembership := a.remoteMembership
	closed := a.closed
	a.mu.Unlock()
	if closed || config == nil {
		return errors.New("Peer config-only host channel is unavailable")
	}
	if err := validatePeerConfigMembership(a.host.app, remoteMembership); err != nil {
		return err
	}
	return config.Handle(a.ctx, kind, payload)
}

func (a *peerRendezvousConfigHostAttempt) syncConfigNow() (bool, error) {
	a.mu.Lock()
	config := a.config
	remoteMembership := a.remoteMembership
	closed := a.closed
	a.mu.Unlock()
	if closed || config == nil {
		return false, nil
	}
	if err := validatePeerConfigMembership(a.host.app, remoteMembership); err != nil {
		go a.host.removeConfigAttempt(a.id, true)
		return true, err
	}
	return true, config.Start(a.ctx)
}

func (h *peerRendezvousHost) removeConfigAttempt(id uuid.UUID, closeChannel bool) {
	h.mu.Lock()
	attempt := h.configAttempts[id]
	delete(h.configAttempts, id)
	h.mu.Unlock()
	if attempt != nil {
		attempt.close(closeChannel)
	}
}

func (a *peerRendezvousConfigHostAttempt) close(closeChannel bool) {
	var channel peerQuickTunnelChannel
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		channel = a.channel
		a.channel = nil
		a.config = nil
		a.remoteMembership = ""
		a.mu.Unlock()
		a.cancel()
	})
	if closeChannel && channel != nil {
		_ = channel.Close()
	}
}

func (h *peerRendezvousHost) runConfigSyncLoop() {
	if h == nil || h.ctx == nil {
		return
	}
	run := func() {
		if _, err := h.syncPlannedConfigRoutes(time.Now()); err != nil && h.ctx.Err() == nil {
			logWarn("rendezvous", "plan Peer config-only routes: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(peerRendezvousConfigSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case at := <-ticker.C:
			if _, err := h.syncPlannedConfigRoutes(at); err != nil && h.ctx.Err() == nil {
				logWarn("rendezvous", "plan Peer config-only routes: %v", err)
			}
		}
	}
}

func (h *peerRendezvousHost) syncPlannedConfigRoutes(at time.Time) (int, error) {
	if h == nil || h.route == nil || at.IsZero() {
		return 0, nil
	}
	routes := h.route.OnlinePeerRoutes()
	byPeer := make(map[string]rendezvousclient.PeerRoute, len(routes))
	observations := make([]peerdiscovery.Reachability, 0, len(routes))
	for _, route := range routes {
		byPeer[route.PeerID] = route
		observations = append(observations, peerdiscovery.Reachability{
			PeerID: route.PeerID, PresenceID: route.PresenceID, Role: rendezvous.RoleHost,
			ObservedAt: at, ExpiresAt: at.Add(peerdiscovery.RotationInterval),
		})
	}
	manager, err := h.app.peerManager()
	if err != nil {
		return 0, err
	}
	targets, err := manager.planRendezvousSyncTargets(observations, at)
	if err != nil {
		return 0, err
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[target.PeerID] = struct{}{}
	}
	h.mu.Lock()
	stale := make([]*peerRendezvousConfigClient, 0)
	for peerID, client := range h.configClients {
		if _, keep := targetSet[peerID]; !keep {
			stale = append(stale, client)
		}
	}
	h.mu.Unlock()
	for _, client := range stale {
		client.close(true)
	}
	started := 0
	for _, target := range targets {
		if h.startConfigClient(byPeer[target.PeerID]) {
			started++
		}
	}
	return started, nil
}

func (h *peerRendezvousHost) startConfigClient(remote rendezvousclient.PeerRoute) bool {
	if remote.PeerID == "" || remote.PresenceID == "" {
		return false
	}
	ctx, cancel := context.WithCancel(h.ctx)
	client := &peerRendezvousConfigClient{host: h, remote: remote, ctx: ctx, cancel: cancel}
	h.mu.Lock()
	if _, exists := h.configClients[remote.PeerID]; exists {
		h.mu.Unlock()
		cancel()
		return false
	}
	h.configClients[remote.PeerID] = client
	h.mu.Unlock()
	go client.run()
	return true
}

func (c *peerRendezvousConfigClient) run() {
	identity, genesisToken, localMembership, remoteMembership, err := c.host.configClientAuthorization(c.remote)
	if err != nil {
		c.finish(false)
		return
	}
	attempt, err := c.host.route.Dial(c.ctx, rendezvousclient.ClientAttemptConfig{
		Remote: c.remote, Identity: identity, GenesisToken: genesisToken,
		ClientMembershipToken: localMembership, HostMembershipToken: remoteMembership,
		SessionID: rendezvousclient.ConfigSyncSessionID(), ClientInstanceID: "config-sync",
		OnAuthenticated: c.onAuthenticated,
		OnRecord: func(peertransport.RecordKind, []byte) {
			logWarn("rendezvous", "terminal record rejected on Peer config-only route")
			c.finish(true)
		},
		OnConfigMessage: c.handleConfigMessage,
		OnClosed:        func(error) { c.finish(false) },
	})
	if err != nil {
		c.finish(false)
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = attempt.Close()
		return
	}
	c.attempt = attempt
	c.mu.Unlock()
}

func (h *peerRendezvousHost) configClientAuthorization(remote rendezvousclient.PeerRoute) (*peercrypto.Identity, string, string, string, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return nil, "", "", "", err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, "", "", "", err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, "", "", "", err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, "", "", "", err
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return nil, "", "", "", err
	}
	local := membershipForPeerID(active, identity.PeerID())
	remoteMembership := membershipForPeerID(active, remote.PeerID)
	if local == nil || remoteMembership == nil || !bytes.Equal(remoteMembership.WrappingPublicKey, remote.WrappingPublicKey) {
		return nil, "", "", "", rendezvousclient.ErrAuthentication
	}
	return identity, state.GenesisToken, local.Token, remoteMembership.Token, nil
}

func (c *peerRendezvousConfigClient) onAuthenticated(channel *peertransport.PionClientChannel) {
	remoteMembership, ok := channel.RemoteMembershipToken()
	if !ok || remoteMembership == "" || validatePeerConfigMembership(c.host.app, remoteMembership) != nil {
		c.finish(true)
		return
	}
	config, err := newPeerConfigChannel(c.host.app, channel)
	if err != nil {
		c.finish(true)
		return
	}
	c.mu.Lock()
	if c.closed || c.channel != nil {
		c.mu.Unlock()
		_ = channel.Close()
		return
	}
	c.channel = channel
	c.config = config
	c.remoteMembership = remoteMembership
	c.mu.Unlock()
	if err := config.Start(c.ctx); err != nil {
		c.finish(true)
	}
}

func (c *peerRendezvousConfigClient) handleConfigMessage(kind peertransport.RecordKind, payload []byte) error {
	c.mu.Lock()
	config := c.config
	remoteMembership := c.remoteMembership
	closed := c.closed
	c.mu.Unlock()
	if closed || config == nil {
		return errors.New("Peer config-only client channel is unavailable")
	}
	if err := validatePeerConfigMembership(c.host.app, remoteMembership); err != nil {
		return err
	}
	return config.Handle(c.ctx, kind, payload)
}

func (c *peerRendezvousConfigClient) syncConfigNow() (bool, error) {
	c.mu.Lock()
	config := c.config
	remoteMembership := c.remoteMembership
	closed := c.closed
	c.mu.Unlock()
	if closed || config == nil {
		return false, nil
	}
	if err := validatePeerConfigMembership(c.host.app, remoteMembership); err != nil {
		c.finish(true)
		return true, err
	}
	return true, config.Start(c.ctx)
}

func (c *peerRendezvousConfigClient) close(closeTransport bool) {
	c.finish(closeTransport)
}

func (c *peerRendezvousConfigClient) finish(closeTransport bool) {
	var attempt *peertransport.PionClientAttempt
	var channel peerQuickTunnelChannel
	finished := false
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		attempt = c.attempt
		channel = c.channel
		c.attempt = nil
		c.channel = nil
		c.config = nil
		c.remoteMembership = ""
		c.mu.Unlock()
		c.cancel()
		c.host.mu.Lock()
		if c.host.configClients[c.remote.PeerID] == c {
			delete(c.host.configClients, c.remote.PeerID)
		}
		c.host.mu.Unlock()
		finished = true
	})
	if !finished || !closeTransport {
		return
	}
	if attempt != nil {
		_ = attempt.Close()
	} else if channel != nil {
		_ = channel.Close()
	}
}

func validatePeerConfigMembership(app *App, remoteMembership string) error {
	if app == nil || remoteMembership == "" {
		return errPeerConfigSyncDenied
	}
	_, err := app.newPeerConfigSyncReceiver(remoteMembership)
	return err
}
