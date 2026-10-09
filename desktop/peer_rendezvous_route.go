package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
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
	ctx        context.Context
	cancel     context.CancelFunc
	serviceURL string
	topic      string

	mu             sync.Mutex
	attempts       map[uuid.UUID]*peerHostAttempt
	configAttempts map[uuid.UUID]*peerRendezvousConfigHostAttempt
	configClients  map[string]*peerRendezvousConfigClient
	catalog        map[uuid.UUID]peerDiscoveredSession
}

type peerDiscoveredSession struct {
	info      proto.SessionInfo
	remote    rendezvousclient.PeerRoute
	manualURL string
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
	hostCtx, cancelHost := context.WithCancel(ctx)
	peerHost := &peerRendezvousHost{
		app: app, runtime: &peerHostRuntime{app: app, host: host}, ctx: hostCtx, cancel: cancelHost,
		serviceURL: presence.ServiceURL, topic: presence.Topic,
		attempts:       make(map[uuid.UUID]*peerHostAttempt),
		configAttempts: make(map[uuid.UUID]*peerRendezvousConfigHostAttempt),
		configClients:  make(map[string]*peerRendezvousConfigClient),
		catalog:        make(map[uuid.UUID]peerDiscoveredSession),
	}
	route, err := rendezvousclient.NewRoute(hostCtx, rendezvousclient.RouteConfig{
		Connection: connection, InitialPresence: snapshot,
		SpaceID: discovery.spaceID, EpochKey: discovery.syncKey,
		LocalPeerID: discovery.localPeerID, LocalWrappingIdentity: wrapping,
		WebRTC: webRTC,
		ResolvePeer: func(presenceID string) (rendezvousclient.PeerRoute, bool) {
			return manager.resolveRendezvousPeerRoute(presenceID, time.Now())
		},
		AuthorizeHost: peerHost.authorize,
		CatalogHost: func(ctx context.Context, request rendezvousclient.PeerCatalogRequest) ([]rendezvousclient.PeerSession, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return peerHost.runtime.catalog(request.ClientPeerID)
		},
		ConnectionBundleHost: func(ctx context.Context, _ rendezvousclient.PeerCatalogRequest) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			bundle, err := app.CreatePeerConnectionBundle("")
			if err != nil {
				return "", err
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return bundle, nil
		},
	})
	if err != nil {
		cancelHost()
		connection.CloseNow()
		return nil, err
	}
	peerHost.route = route
	return peerHost, nil
}

func (h *peerRendezvousHost) discoverSessions(ctx context.Context) ([]proto.SessionInfo, error) {
	if h == nil || h.route == nil || ctx == nil {
		return nil, rendezvousclient.ErrServiceUnavailable
	}
	remotes := h.route.OnlinePeerRoutes()
	type result struct {
		remote  rendezvousclient.PeerRoute
		catalog rendezvousclient.PeerCatalog
		err     error
	}
	results := make(chan result, len(remotes))
	for _, remote := range remotes {
		remote := remote
		go func() {
			catalog, err := h.route.CatalogWithRoutes(ctx, remote)
			results <- result{remote: remote, catalog: catalog, err: err}
		}()
	}
	next := make(map[uuid.UUID]peerDiscoveredSession)
	var joined error
	for range remotes {
		select {
		case item := <-results:
			if item.err != nil {
				joined = errors.Join(joined, item.err)
				continue
			}
			accepted, err := h.validateAndApplyCatalog(item.remote, item.catalog)
			if err != nil {
				joined = errors.Join(joined, err)
				continue
			}
			for id, session := range accepted {
				next[id] = session
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(remotes) != 0 && len(next) == 0 && joined != nil {
		return nil, joined
	}
	h.mu.Lock()
	h.catalog = next
	h.mu.Unlock()
	infos := make([]proto.SessionInfo, 0, len(next))
	for _, session := range next {
		infos = append(infos, session.info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, nil
}

func (h *peerRendezvousHost) validateAndApplyCatalog(remote rendezvousclient.PeerRoute, catalog rendezvousclient.PeerCatalog) (map[uuid.UUID]peerDiscoveredSession, error) {
	accepted, err := h.validateCatalog(remote, catalog.Sessions)
	if err != nil {
		return nil, err
	}
	if catalog.ConnectionBundle != "" {
		if _, err := h.app.importPeerConnectionBundle(catalog.ConnectionBundle, remote.PeerID); err != nil {
			return nil, rendezvousclient.ErrAuthentication
		}
	}
	return accepted, nil
}

func (h *peerRendezvousHost) validateCatalog(remote rendezvousclient.PeerRoute, sessions []rendezvousclient.PeerSession) (map[uuid.UUID]peerDiscoveredSession, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return nil, rendezvousclient.ErrAuthentication
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, rendezvousclient.ErrAuthentication
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, rendezvousclient.ErrAuthentication
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, rendezvousclient.ErrAuthentication
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return nil, rendezvousclient.ErrAuthentication
	}
	local := membershipForPeerID(active, identity.PeerID())
	host := membershipForPeerID(active, remote.PeerID)
	if local == nil || host == nil || !bytes.Equal(host.WrappingPublicKey, remote.WrappingPublicKey) {
		return nil, rendezvousclient.ErrAuthentication
	}
	result := make(map[uuid.UUID]peerDiscoveredSession, len(sessions))
	for _, advertised := range sessions {
		id, err := uuid.Parse(advertised.ID)
		if err != nil || id == uuid.Nil || !peerMembershipAllowsSession(*local, id) || !peerMembershipAllowsSession(*host, id) ||
			!permissionWithinPeerGrant(advertised.Permission, local.Document.Permission) ||
			!permissionWithinPeerGrant(advertised.Permission, host.Document.Permission) {
			continue
		}
		result[id] = peerDiscoveredSession{info: peerCatalogSessionInfo(advertised), remote: remote}
	}
	return result, nil
}

func permissionWithinPeerGrant(permission peertransport.Permission, granted peerproto.Permission) bool {
	ceiling, ok := peerPermission(granted)
	return ok && permission >= peertransport.PermissionView && permission <= ceiling
}

func peerCatalogSessionInfo(session rendezvousclient.PeerSession) proto.SessionInfo {
	return proto.SessionInfo{
		ID: session.ID, Command: session.Command, Cwd: session.Cwd, Title: session.Title,
		Cols: session.Cols, Rows: session.Rows, StartedAt: session.StartedAt,
		HostID: session.HostID, Host: session.Host, User: session.User, SSHHostID: session.SSHHostID,
		RemotePermission: peerPermissionName(session.Permission), TaskState: session.TaskState,
		CurrentCommand: session.CurrentCommand, CommandStartedAt: session.CommandStartedAt,
		CommandEndedAt: session.CommandEndedAt, CommandDurationMS: session.CommandDurationMS,
		CommandExitCode: session.CommandExitCode, LastOutputAt: session.LastOutputAt,
		Type: session.Type, AttentionAt: session.AttentionAt,
	}
}

func (h *peerRendezvousHost) discoveredSession(sessionID uuid.UUID) (peerDiscoveredSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session, ok := h.catalog[sessionID]
	return session, ok
}

// ListPeerSessions discovers active, authorized Peer sessions through all
// configured Peer routes. The returned JSON matches proto.SessionInfo[] so
// the renderer can merge it with its existing sidebar model.
func (a *App) ListPeerSessions() (string, error) {
	if a == nil || a.ctx == nil {
		return "", rendezvousclient.ErrServiceUnavailable
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvousMu.Unlock()
	var active *peerRendezvousHost
	if lifecycle != nil {
		lifecycle.mu.Lock()
		active = lifecycle.active
		lifecycle.mu.Unlock()
	}
	type discoveryResult struct {
		manual   bool
		sessions []proto.SessionInfo
		err      error
	}
	resultCount := 1
	if active != nil {
		resultCount++
	}
	results := make(chan discoveryResult, resultCount)
	go func() {
		sessions, err := a.discoverPeerLANSessions(ctx)
		results <- discoveryResult{manual: true, sessions: sessions, err: err}
	}()
	if active != nil {
		go func() {
			sessions, err := active.discoverSessions(ctx)
			results <- discoveryResult{sessions: sessions, err: err}
		}()
	}
	var manualSessions, rendezvousSessions []proto.SessionInfo
	var discoveryErr error
	for range resultCount {
		result := <-results
		if result.err != nil {
			discoveryErr = errors.Join(discoveryErr, result.err)
			continue
		}
		if result.manual {
			manualSessions = result.sessions
		} else {
			rendezvousSessions = result.sessions
		}
	}
	byID := make(map[string]proto.SessionInfo, len(manualSessions)+len(rendezvousSessions))
	for _, session := range manualSessions {
		byID[session.ID] = session
	}
	// Preserve the established Rendezvous entry when the same session is
	// reachable through both discovery paths.
	for _, session := range rendezvousSessions {
		byID[session.ID] = session
	}
	if len(byID) == 0 && discoveryErr != nil {
		return "", discoveryErr
	}
	sessions := make([]proto.SessionInfo, 0, len(byID))
	for _, session := range byID {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	encoded, err := json.Marshal(sessions)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
	if request.SessionID == rendezvousclient.ConfigSyncSessionID() {
		return h.authorizeConfig(ctx, request)
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
	configAttempts := make([]*peerRendezvousConfigHostAttempt, 0, len(h.configAttempts))
	for _, attempt := range h.configAttempts {
		configAttempts = append(configAttempts, attempt)
	}
	configClients := make([]*peerRendezvousConfigClient, 0, len(h.configClients))
	for _, client := range h.configClients {
		configClients = append(configClients, client)
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
	for _, attempt := range configAttempts {
		active, err := attempt.syncConfigNow()
		if active {
			sent++
		}
		if err != nil {
			syncErr = errors.Join(syncErr, err)
		}
	}
	for _, client := range configClients {
		active, err := client.syncConfigNow()
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
	if h.cancel != nil {
		h.cancel()
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
	configAttempts := make([]*peerRendezvousConfigHostAttempt, 0, len(h.configAttempts))
	for id, attempt := range h.configAttempts {
		delete(h.configAttempts, id)
		configAttempts = append(configAttempts, attempt)
	}
	configClients := make([]*peerRendezvousConfigClient, 0, len(h.configClients))
	for peerID, client := range h.configClients {
		delete(h.configClients, peerID)
		configClients = append(configClients, client)
	}
	h.mu.Unlock()
	for _, attempt := range attempts {
		attempt.close(false)
	}
	for _, attempt := range configAttempts {
		attempt.close(false)
	}
	for _, client := range configClients {
		client.close(true)
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
