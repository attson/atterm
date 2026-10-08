package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/google/uuid"
)

var errPeerQuickTunnelRouteUnavailable = errors.New("verified Quick Tunnel route unavailable")
var errPeerConnectionBundleImportRejected = errors.New("Peer connection bundle import rejected")

// peerQuickTunnelRoute is deliberately process-local. Reachability hints are
// short-lived and replaceable; durable trust remains in the Peer store.
type peerQuickTunnelRoute struct {
	URL       string
	ExpiresAt int64
}

// PeerRouteImportResult reports only the authenticated route capability. The
// endpoint remains in Go memory and is never exposed to the renderer.
type PeerRouteImportResult struct {
	IssuerPeerID string `json:"issuer_peer_id"`
	QuickTunnel  bool   `json:"quick_tunnel"`
	ExpiresAt    int64  `json:"expires_at"`
}

// PeerSessionRouteStatus exposes route kinds for one authenticated catalog
// entry without exposing temporary endpoints or membership credentials.
type PeerSessionRouteStatus struct {
	Direct      bool `json:"direct"`
	QuickTunnel bool `json:"quick_tunnel"`
}

// ImportPeerConnectionBundle rotates the process-local route hint for an
// existing Peer member. First-join bundles are deliberately rejected here.
func (a *App) ImportPeerConnectionBundle(raw string) (PeerRouteImportResult, error) {
	return a.importPeerConnectionBundle(raw, "")
}

func (a *App) importPeerConnectionBundle(raw, expectedIssuerPeerID string) (PeerRouteImportResult, error) {
	token, err := normalizePeerConnectionBundle(raw)
	if err != nil {
		return PeerRouteImportResult{}, errPeerConnectionBundleImportRejected
	}
	manager, err := a.peerManager()
	if err != nil {
		return PeerRouteImportResult{}, err
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	bundle, err := peerproto.VerifyConnectionBundle(token, now)
	if err != nil || bundle.Ticket != nil || expectedIssuerPeerID != "" && bundle.Document.IssuerPeerID != expectedIssuerPeerID {
		return PeerRouteImportResult{}, errPeerConnectionBundleImportRejected
	}
	state, err := manager.store.Load()
	if err != nil {
		return PeerRouteImportResult{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil || genesis.Hash != bundle.Genesis.Hash {
		return PeerRouteImportResult{}, errPeerConnectionBundleImportRejected
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return PeerRouteImportResult{}, fmt.Errorf("resolve active Peer memberships: %w", err)
	}
	issuer := membershipForPeerID(active, bundle.Document.IssuerPeerID)
	if issuer == nil || issuer.Token != bundle.Issuer.Token {
		return PeerRouteImportResult{}, errPeerConnectionBundleImportRejected
	}

	result := PeerRouteImportResult{
		IssuerPeerID: bundle.Document.IssuerPeerID,
		ExpiresAt:    bundle.Document.ExpiresAt,
	}
	var route peerQuickTunnelRoute
	for _, candidate := range bundle.Document.Routes {
		if candidate.Kind == peerproto.RouteQuickTunnel {
			route = peerQuickTunnelRoute{URL: candidate.URL, ExpiresAt: bundle.Document.ExpiresAt}
			result.QuickTunnel = true
			break
		}
	}
	a.peerRouteMu.Lock()
	if result.QuickTunnel {
		if a.peerRoutes == nil {
			a.peerRoutes = make(map[string]peerQuickTunnelRoute)
		}
		a.peerRoutes[result.IssuerPeerID] = route
	} else {
		delete(a.peerRoutes, result.IssuerPeerID)
	}
	a.peerRouteMu.Unlock()
	return result, nil
}

// GetPeerSessionRouteStatus resolves route availability from the current
// authenticated Rendezvous catalog and the verified ephemeral route cache.
func (a *App) GetPeerSessionRouteStatus(sessionID string) PeerSessionRouteStatus {
	id, err := parsePeerSessionID(sessionID)
	if err != nil {
		return PeerSessionRouteStatus{}
	}
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvousMu.Unlock()
	if lifecycle == nil {
		return PeerSessionRouteStatus{}
	}
	lifecycle.mu.Lock()
	host := lifecycle.active
	lifecycle.mu.Unlock()
	if host == nil {
		return PeerSessionRouteStatus{}
	}
	discovered, ok := host.discoveredSession(id)
	if !ok {
		return PeerSessionRouteStatus{}
	}
	status := PeerSessionRouteStatus{Direct: host.route != nil}
	_, status.QuickTunnel = a.peerQuickTunnelRouteAvailable(discovered.remote.PeerID)
	return status
}

func parsePeerSessionID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid Peer session id")
	}
	return id, nil
}

func (a *App) peerQuickTunnelRouteAvailable(peerID string) (peerQuickTunnelRoute, bool) {
	route, err := a.peerQuickTunnelRoute(peerID)
	return route, err == nil
}

func (a *App) rememberPeerQuickTunnelRoute(bundle peerproto.VerifiedConnectionBundle) {
	if a == nil || bundle.Document.IssuerPeerID == "" || bundle.Document.ExpiresAt <= time.Now().Unix() {
		return
	}
	var route peerproto.ConnectionRoute
	for _, candidate := range bundle.Document.Routes {
		if candidate.Kind == peerproto.RouteQuickTunnel {
			route = candidate
			break
		}
	}
	if route.URL == "" || !a.peerQuickTunnelIssuerActive(bundle.Genesis.Hash, bundle.Document.IssuerPeerID) {
		return
	}
	a.peerRouteMu.Lock()
	if a.peerRoutes == nil {
		a.peerRoutes = make(map[string]peerQuickTunnelRoute)
	}
	a.peerRoutes[bundle.Document.IssuerPeerID] = peerQuickTunnelRoute{URL: route.URL, ExpiresAt: bundle.Document.ExpiresAt}
	a.peerRouteMu.Unlock()
}

func (a *App) peerQuickTunnelRoute(peerID string) (peerQuickTunnelRoute, error) {
	now := time.Now().Unix()
	a.peerRouteMu.Lock()
	route, ok := a.peerRoutes[peerID]
	if ok && route.ExpiresAt <= now {
		delete(a.peerRoutes, peerID)
		ok = false
	}
	a.peerRouteMu.Unlock()
	if !ok || route.URL == "" || !a.peerQuickTunnelIssuerActive("", peerID) {
		return peerQuickTunnelRoute{}, errPeerQuickTunnelRouteUnavailable
	}
	return route, nil
}

func (a *App) peerQuickTunnelIssuerActive(expectedGenesisHash, peerID string) bool {
	manager, err := a.peerManager()
	if err != nil {
		return false
	}
	state, err := manager.store.Load()
	if err != nil {
		return false
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil || expectedGenesisHash != "" && genesis.Hash != expectedGenesisHash {
		return false
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	active, err := activePeerMemberships(state, genesis, now)
	return err == nil && membershipForPeerID(active, peerID) != nil
}
