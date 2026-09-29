package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// peerRendezvousHost adapts encrypted Rendezvous signaling to the same local
// Peer session runtime used by Quick Tunnel. It owns no trust or PTY state.
type peerRendezvousHost struct {
	app        *App
	runtime    *peerHostRuntime
	route      *rendezvousclient.Route
	serviceURL string
	topic      string

	mu       sync.Mutex
	attempts map[uuid.UUID]*peerHostAttempt
}

func newPeerRendezvousHost(ctx context.Context, app *App, host *relayHost, presence rendezvousclient.PresenceConfig, webRTC webrtc.Configuration) (*peerRendezvousHost, error) {
	return newPeerRendezvousHostWithRegistrationContext(ctx, ctx, app, host, presence, webRTC)
}

func newPeerRendezvousHostWithRegistrationContext(ctx, registrationCtx context.Context, app *App, host *relayHost, presence rendezvousclient.PresenceConfig, webRTC webrtc.Configuration) (*peerRendezvousHost, error) {
	if ctx == nil || app == nil || host == nil || host.server == nil {
		return nil, errors.New("Rendezvous Peer host is unavailable")
	}
	if registrationCtx == nil {
		return nil, errors.New("Rendezvous registration context is unavailable")
	}
	manager, err := app.peerManager()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	discovery, err := manager.rendezvousDiscoveryState(now)
	if err != nil {
		return nil, err
	}
	coordinates, err := manager.rendezvousCoordinates(now)
	if err != nil {
		return nil, err
	}
	if presence.Topic == "" {
		presence.Topic = coordinates.Topic
	}
	if presence.PresenceID == "" {
		presence.PresenceID = coordinates.PresenceID
	}
	if presence.Role == "" {
		presence.Role = rendezvous.RoleHost
	}
	connection, snapshot, err := rendezvousclient.DialPresence(registrationCtx, presence)
	if err != nil {
		return nil, err
	}
	wrapping, err := manager.loadWrappingIdentity()
	if err != nil {
		connection.CloseNow()
		return nil, err
	}
	peerHost := &peerRendezvousHost{
		app: app, runtime: &peerHostRuntime{app: app, host: host},
		serviceURL: presence.ServiceURL, topic: presence.Topic,
		attempts: make(map[uuid.UUID]*peerHostAttempt),
	}
	route, err := rendezvousclient.NewRoute(ctx, rendezvousclient.RouteConfig{
		Connection: connection, InitialPresence: snapshot,
		SpaceID: discovery.spaceID, EpochKey: discovery.syncKey,
		LocalPeerID: discovery.localPeerID, LocalWrappingIdentity: wrapping,
		WebRTC: webRTC,
		ResolvePeer: func(presenceID string) (rendezvousclient.PeerRoute, bool) {
			return manager.resolveRendezvousPeerRoute(presenceID, time.Now())
		},
		AuthorizeHost: peerHost.authorize,
	})
	if err != nil {
		connection.CloseNow()
		return nil, err
	}
	peerHost.route = route
	return peerHost, nil
}

func (h *peerRendezvousHost) reachablePeers() int {
	if h == nil || h.route == nil {
		return 0
	}
	return h.route.OnlinePeerCount()
}

func (h *peerRendezvousHost) authorize(ctx context.Context, request rendezvousclient.PeerOpenRequest) (rendezvousclient.HostAuthorization, rendezvousclient.HostCallbacks, error) {
	if err := ctx.Err(); err != nil {
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, err
	}
	authorization, err := h.runtime.authorize(request.ClientPeerID, request.SessionID)
	if err != nil {
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
	}
	permission := peerPermissionName(authorization.Permission)
	if permission == "" {
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
	}
	attempt := &peerHostAttempt{
		host: h.runtime, sessionID: request.SessionID, permission: permission,
		clientInstanceID: request.ClientInstanceID,
	}
	attempt.remove = func() { h.removeAttempt(request.AttemptID) }
	h.mu.Lock()
	if _, exists := h.attempts[request.AttemptID]; exists {
		h.mu.Unlock()
		return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
	}
	h.attempts[request.AttemptID] = attempt
	h.mu.Unlock()
	callbacks := rendezvousclient.HostCallbacks{
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if err := attempt.start(ctx, channel); err != nil {
				logWarn("rendezvous", "Peer session start failed session=%s: %v", request.SessionID, err)
				h.removeAttempt(request.AttemptID)
			}
		},
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			if err := attempt.handleRecord(ctx, kind, payload); err != nil {
				logWarn("rendezvous", "Peer record rejected session=%s: %v", request.SessionID, err)
				h.removeAttempt(request.AttemptID)
			}
		},
		OnConfigMessage: attempt.handleConfigMessage,
		OnClosed: func(_ error) {
			h.removeAttempt(request.AttemptID)
		},
	}
	return rendezvousclient.HostAuthorization{
		Identity: authorization.Identity, GenesisToken: authorization.GenesisToken,
		ClientMembershipToken: authorization.ClientMembershipToken,
		HostMembershipToken:   authorization.HostMembershipToken,
		Permission:            authorization.Permission,
	}, callbacks, nil
}

func (h *peerRendezvousHost) removeAttempt(id uuid.UUID) {
	h.mu.Lock()
	attempt := h.attempts[id]
	delete(h.attempts, id)
	h.mu.Unlock()
	if attempt != nil {
		attempt.close(false)
	}
}

func (h *peerRendezvousHost) syncConfigNow() (int, error) {
	if h == nil {
		return 0, nil
	}
	h.mu.Lock()
	attempts := make([]*peerHostAttempt, 0, len(h.attempts))
	for _, attempt := range h.attempts {
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	sent := 0
	var syncErr error
	for _, attempt := range attempts {
		active, err := attempt.syncConfigNow()
		if active {
			sent++
		}
		if err != nil {
			syncErr = errors.Join(syncErr, err)
		}
	}
	return sent, syncErr
}

func (h *peerRendezvousHost) Close() {
	if h == nil {
		return
	}
	if h.route != nil {
		h.route.Close()
	}
	h.mu.Lock()
	attempts := make([]*peerHostAttempt, 0, len(h.attempts))
	for id, attempt := range h.attempts {
		delete(h.attempts, id)
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	for _, attempt := range attempts {
		attempt.close(false)
	}
}

func (m *peerSpaceManager) resolveRendezvousPeerRoute(presenceID string, at time.Time) (rendezvousclient.PeerRoute, bool) {
	peerID, ok, err := m.resolveRendezvousPresence(presenceID, at)
	if err != nil || !ok {
		return rendezvousclient.PeerRoute{}, false
	}
	state, err := m.store.Load()
	if err != nil {
		return rendezvousclient.PeerRoute{}, false
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return rendezvousclient.PeerRoute{}, false
	}
	active, err := activePeerMemberships(state, genesis, at)
	if err != nil {
		return rendezvousclient.PeerRoute{}, false
	}
	membership := membershipForPeerID(active, peerID)
	if membership == nil {
		return rendezvousclient.PeerRoute{}, false
	}
	return rendezvousclient.PeerRoute{
		PeerID: peerID, PresenceID: presenceID,
		WrappingPublicKey: append([]byte(nil), membership.WrappingPublicKey...),
	}, true
}

var _ peerQuickTunnelChannel = (*peertransport.PionHostChannel)(nil)
