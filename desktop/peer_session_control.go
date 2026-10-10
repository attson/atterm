package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertraffic"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
)

const peerSessionCreateTimeout = 30 * time.Second

// PeerHostDescriptor is the token-free host catalog exposed to the renderer.
// Route endpoints, membership documents and Peer keys remain Go-owned.
type PeerHostDescriptor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Permission string `json:"permission"`
}

// peerSessionControlHost carries host-level session creation over the
// reserved authenticated control route. It never subscribes to a PTY.
type peerSessionControlHost struct {
	runtime          *peerHostRuntime
	ctx              context.Context
	channel          peerQuickTunnelChannel
	remoteMembership string
	handler          *sessionCreateHandler
}

func newPeerSessionControlHost(ctx context.Context, runtime *peerHostRuntime, channel peerQuickTunnelChannel, remoteMembership string) (*peerSessionControlHost, error) {
	if ctx == nil || runtime == nil || runtime.host == nil || channel == nil || remoteMembership == "" {
		return nil, errors.New("Peer session control route is unavailable")
	}
	permission, err := runtime.currentHostControlPermission(remoteMembership)
	if err != nil {
		return nil, err
	}
	responses := make(chan proto.Frame, 1)
	handler := newSessionCreateHandler(ctx, responses, runtime.host, permission)
	handler.limit = 1
	handler.authorize = func() bool {
		current, currentErr := runtime.currentHostControlPermission(remoteMembership)
		return currentErr == nil && permissionRankName(current) >= permissionRankName(proto.RemotePermissionControl)
	}
	control := &peerSessionControlHost{
		runtime: runtime, ctx: ctx, channel: channel, remoteMembership: remoteMembership, handler: handler,
	}
	go control.stream(responses)
	return control, nil
}

func (c *peerSessionControlHost) Handle(kind peertransport.RecordKind, payload []byte) (bool, error) {
	if kind != peertransport.RecordSessionCreate {
		return false, nil
	}
	if len(payload) == 0 || len(payload) > maxPeerSessionCreatePayloadBytes {
		return true, errors.New("invalid Peer host session create request")
	}
	var request proto.SessionCreatePayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		request.RequestID == "" || len(request.RequestID) > 256 ||
		request.ProfileID == "" || len(request.ProfileID) > 256 ||
		request.HostID == "" || request.HostID != c.runtime.host.hostID {
		return true, errors.New("invalid Peer host session create request")
	}
	c.handler.submit(request)
	return true, nil
}

func (c *peerSessionControlHost) stream(responses <-chan proto.Frame) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case response, ok := <-responses:
			if !ok {
				return
			}
			if response.Type != proto.TypeSessionCreated || c.channel.SendConfigMessage(c.ctx, peertransport.RecordSessionCreated, response.Payload) != nil {
				_ = c.channel.Close()
				return
			}
		}
	}
}

func (h *peerHostRuntime) currentHostControlPermission(remoteMembership string) (string, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	state, err := manager.store.Load()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	remote, err := peerproto.VerifyGrant(remoteMembership, genesis, now)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	canonicalRemote := membershipForPeerID(active, remote.Document.SubjectPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if canonicalRemote == nil || canonicalRemote.Token != remoteMembership || local == nil ||
		len(canonicalRemote.Document.AllowedSessionIDs) != 0 || len(local.Document.AllowedSessionIDs) != 0 {
		return "", errors.New("Peer host control is unauthorized")
	}
	ownerPermission, err := h.currentOwnerPermission()
	if err != nil {
		return "", err
	}
	owner, ok := directTransportPermission(ownerPermission)
	if !ok {
		return "", errors.New("Peer host control is unauthorized")
	}
	remotePermission, remoteOK := peerPermission(canonicalRemote.Document.Permission)
	localPermission, localOK := peerPermission(local.Document.Permission)
	if !remoteOK || !localOK {
		return "", errors.New("Peer host control is unauthorized")
	}
	effective := peerPermissionName(minimumPeerTransportPermission(owner, remotePermission, localPermission))
	if permissionRankName(effective) < permissionRankName(proto.RemotePermissionControl) {
		return "", errors.New("Peer host control requires control permission")
	}
	return effective, nil
}

// ListPeerHosts refreshes the encrypted Rendezvous catalog and returns hosts
// that can authorize host-level actions even when they currently have no PTY.
func (a *App) ListPeerHosts() ([]PeerHostDescriptor, error) {
	if a == nil || a.ctx == nil {
		return nil, errors.New("Peer host catalog unavailable")
	}
	host := a.activePeerRendezvousHost()
	if host == nil || host.route == nil || a.peerLANOnly() {
		return []PeerHostDescriptor{}, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, peerLANCatalogTimeout)
	defer cancel()
	if _, err := host.discoverSessions(ctx); err != nil {
		return nil, err
	}
	host.mu.Lock()
	result := make([]PeerHostDescriptor, 0, len(host.hostCatalog))
	for _, discovered := range host.hostCatalog {
		result = append(result, PeerHostDescriptor{
			ID: discovered.info.ID, Name: discovered.info.Name,
			Permission: peerPermissionName(discovered.info.Permission),
		})
	}
	host.mu.Unlock()
	sortPeerHosts(result)
	return result, nil
}

func sortPeerHosts(hosts []PeerHostDescriptor) {
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Name == hosts[j].Name {
			return hosts[i].ID < hosts[j].ID
		}
		return hosts[i].Name < hosts[j].Name
	})
}

func (a *App) activePeerRendezvousHost() *peerRendezvousHost {
	if a == nil {
		return nil
	}
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvousMu.Unlock()
	if lifecycle == nil {
		return nil
	}
	lifecycle.mu.Lock()
	host := lifecycle.active
	lifecycle.mu.Unlock()
	return host
}

// CreatePeerSessionWithProfile opens one temporary host-control route. Direct
// is attempted first; Quick Tunnel is considered only when the renderer passes
// the device-local consent flag and no create request reached the Direct route.
func (a *App) CreatePeerSessionWithProfile(hostID, profileID string, allowQuickTunnel bool) (string, error) {
	if a == nil || a.ctx == nil {
		return "", errors.New("upstream_unavailable")
	}
	hostID = strings.TrimSpace(hostID)
	profileID = strings.TrimSpace(profileID)
	if hostID == "" || len(hostID) > 256 || profileID == "" || len(profileID) > 256 {
		return "", errors.New("invalid_request")
	}
	host := a.activePeerRendezvousHost()
	if host == nil || host.route == nil || a.peerLANOnly() {
		return "", errors.New("upstream_unavailable")
	}
	discovered, ok := host.discoveredHost(hostID)
	if !ok {
		if _, err := a.ListPeerHosts(); err != nil {
			return "", errors.New("upstream_unavailable")
		}
		discovered, ok = host.discoveredHost(hostID)
	}
	if !ok {
		return "", errors.New("unknown_host_id")
	}
	if discovered.info.Permission != peertransport.PermissionControl && discovered.info.Permission != peertransport.PermissionFull {
		return "", errors.New("permission_denied")
	}
	request := proto.SessionCreatePayload{
		RequestID: uuid.NewString(), HostID: hostID, ProfileID: profileID,
	}
	ctx, cancel := context.WithTimeout(a.ctx, peerSessionCreateTimeout)
	defer cancel()

	sessionID, sent, err := a.createPeerSessionDirect(ctx, host, discovered, request)
	if err == nil {
		return sessionID, nil
	}
	if sent || !allowQuickTunnel || !peerSessionCreateDirectFallbackAllowed(err) {
		return "", peerSessionCreatePublicError(err)
	}
	quickRoute, routeErr := a.peerQuickTunnelRoute(discovered.remote.PeerID)
	if routeErr != nil {
		return "", errors.New("upstream_unavailable")
	}
	sessionID, _, err = a.createPeerSessionQuickTunnel(ctx, host, discovered, quickRoute, request)
	if err != nil {
		return "", peerSessionCreatePublicError(err)
	}
	return sessionID, nil
}

type peerSessionCreateExchange struct {
	ctx              context.Context
	request          proto.SessionCreatePayload
	remoteMembership string

	mu     sync.Mutex
	sent   bool
	done   bool
	result chan peerSessionCreateResult
}

type peerSessionCreateResult struct {
	sessionID string
	err       error
}

type peerSessionCreateRemoteError string

func (e peerSessionCreateRemoteError) Error() string { return string(e) }

func newPeerSessionCreateExchange(ctx context.Context, request proto.SessionCreatePayload, remoteMembership string) *peerSessionCreateExchange {
	return &peerSessionCreateExchange{
		ctx: ctx, request: request, remoteMembership: remoteMembership,
		result: make(chan peerSessionCreateResult, 1),
	}
}

func (e *peerSessionCreateExchange) onAuthenticated(channel peerQuickTunnelChannel) {
	remoteMembership, ok := channel.RemoteMembershipToken()
	if !ok || remoteMembership != e.remoteMembership {
		e.complete("", rendezvousclient.ErrAuthentication)
		_ = channel.Close()
		return
	}
	payload, err := json.Marshal(e.request)
	if err != nil {
		e.complete("", errors.New("invalid_request"))
		return
	}
	e.mu.Lock()
	e.sent = true
	e.mu.Unlock()
	if err := channel.SendConfigMessage(e.ctx, peertransport.RecordSessionCreate, payload); err != nil {
		e.complete("", err)
	}
}

func (e *peerSessionCreateExchange) onConfigMessage(kind peertransport.RecordKind, payload []byte) error {
	if kind != peertransport.RecordSessionCreated {
		return nil
	}
	var response proto.SessionCreatedPayload
	if decodePeerConfigMessage(payload, &response) != nil || response.RequestID != e.request.RequestID {
		err := errors.New("invalid Peer session-created response")
		e.complete("", err)
		return err
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "invalid_request"
		}
		e.complete("", peerSessionCreateRemoteError(response.Error))
		return nil
	}
	sessionID, err := uuid.Parse(response.SessionID)
	if err != nil || sessionID == uuid.Nil {
		err = errors.New("invalid Peer session-created response")
		e.complete("", err)
		return err
	}
	e.complete(sessionID.String(), nil)
	return nil
}

func (e *peerSessionCreateExchange) complete(sessionID string, err error) {
	e.mu.Lock()
	if e.done {
		e.mu.Unlock()
		return
	}
	e.done = true
	e.mu.Unlock()
	e.result <- peerSessionCreateResult{sessionID: sessionID, err: err}
}

func (e *peerSessionCreateExchange) wasSent() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sent
}

func (e *peerSessionCreateExchange) wait() (string, error) {
	select {
	case result := <-e.result:
		return result.sessionID, result.err
	case <-e.ctx.Done():
		return "", e.ctx.Err()
	}
}

func (a *App) createPeerSessionDirect(ctx context.Context, host *peerRendezvousHost, discovered peerDiscoveredHost, request proto.SessionCreatePayload) (string, bool, error) {
	identity, genesisToken, localMembership, remoteMembership, err := host.configClientAuthorization(discovered.remote)
	if err != nil {
		return "", false, err
	}
	exchange := newPeerSessionCreateExchange(ctx, request, remoteMembership)
	attempt, err := host.route.Dial(ctx, rendezvousclient.ClientAttemptConfig{
		Remote: discovered.remote, Identity: identity, GenesisToken: genesisToken,
		ClientMembershipToken: localMembership, HostMembershipToken: remoteMembership,
		SessionID: rendezvousclient.ConfigSyncSessionID(), ClientInstanceID: "session-create-" + request.RequestID,
		OnAuthenticated: func(channel *peertransport.PionClientChannel) { exchange.onAuthenticated(channel) },
		OnRecord: func(peertransport.RecordKind, []byte) {
			exchange.complete("", errors.New("unexpected terminal record on Peer host-control route"))
		},
		OnConfigMessage: exchange.onConfigMessage,
		OnTraffic:       a.recordPeerTraffic(peertraffic.RouteDirect),
		OnClosed: func(closeErr error) {
			if closeErr == nil {
				closeErr = errors.New("Peer host-control route closed")
			}
			exchange.complete("", closeErr)
		},
	})
	if err != nil {
		return "", exchange.wasSent(), err
	}
	defer attempt.Close()
	sessionID, err := exchange.wait()
	return sessionID, exchange.wasSent(), err
}

func (a *App) createPeerSessionQuickTunnel(ctx context.Context, host *peerRendezvousHost, discovered peerDiscoveredHost, route peerQuickTunnelRoute, request proto.SessionCreatePayload) (string, bool, error) {
	identity, genesisToken, localMembership, remoteMembership, err := host.configClientAuthorization(discovered.remote)
	if err != nil {
		return "", false, err
	}
	exchange := newPeerSessionCreateExchange(ctx, request, remoteMembership)
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: route.URL, Identity: identity, GenesisToken: genesisToken,
		ClientMembershipToken: localMembership, HostMembershipToken: remoteMembership,
		SessionID: peertransport.ConfigSyncSessionID(), ClientInstanceID: "session-create-" + request.RequestID,
		OnClosed: func(_ *quicktunnel.SignalChannel, closeErr error) {
			if closeErr == nil {
				closeErr = errors.New("Peer host-control route closed")
			}
			exchange.complete("", closeErr)
		},
	})
	if err != nil {
		return "", false, err
	}
	defer signal.Close()
	if err := signal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnAuthenticated: func(channel *quicktunnel.WSSChannel) { exchange.onAuthenticated(channel) },
		OnRecord: func(peertransport.RecordKind, []byte) {
			exchange.complete("", errors.New("unexpected terminal record on Peer host-control route"))
		},
		OnConfigMessage: exchange.onConfigMessage,
		OnTraffic:       a.recordPeerTraffic(peertraffic.RouteQuickTunnel),
	}); err != nil {
		return "", false, err
	}
	if _, err := signal.StartWSSFallback(ctx); err != nil {
		return "", exchange.wasSent(), err
	}
	sessionID, err := exchange.wait()
	return sessionID, exchange.wasSent(), err
}

func peerSessionCreateDirectFallbackAllowed(err error) bool {
	return errors.Is(err, rendezvousclient.ErrPeerOffline) ||
		errors.Is(err, rendezvousclient.ErrServiceUnavailable) ||
		errors.Is(err, rendezvousclient.ErrICEFailed)
}

func peerSessionCreatePublicError(err error) error {
	if err == nil {
		return nil
	}
	var remoteErr peerSessionCreateRemoteError
	if errors.As(err, &remoteErr) {
		return errors.New(remoteErr.Error())
	}
	message := err.Error()
	switch message {
	case "unknown_profile", "permission_denied", "request_in_flight", "duplicate_request_id",
		"unknown_host_id", "invalid_request", "upstream_unavailable", "session_create_busy":
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timeout")
	}
	if errors.Is(err, rendezvousclient.ErrAuthentication) {
		return errors.New("permission_denied")
	}
	return errors.New("upstream_unavailable")
}
