package main

import (
	"errors"
	"time"

	"github.com/attson/atterm/internal/peerproto"
)

var errPeerQuickTunnelRouteUnavailable = errors.New("verified Quick Tunnel route unavailable")

// peerQuickTunnelRoute is deliberately process-local. Reachability hints are
// short-lived and replaceable; durable trust remains in the Peer store.
type peerQuickTunnelRoute struct {
	URL       string
	ExpiresAt int64
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
