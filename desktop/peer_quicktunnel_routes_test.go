package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
)

func TestImportPeerConnectionBundleRotatesAndRemovesQuickTunnelRoute(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}

	first := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://first.trycloudflare.com",
	}})
	result, err := fixture.app.ImportPeerConnectionBundle("atterm://peer/connect#" + first)
	if err != nil {
		t.Fatal(err)
	}
	if result.IssuerPeerID != fixture.clientIdentity.PeerID() || !result.QuickTunnel || result.ExpiresAt == 0 {
		t.Fatalf("import result=%+v", result)
	}
	route, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID())
	if err != nil || route.URL != "https://first.trycloudflare.com" {
		t.Fatalf("first route=%+v err=%v", route, err)
	}

	second := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://second.trycloudflare.com",
	}})
	if _, err := fixture.app.ImportPeerConnectionBundle(second); err != nil {
		t.Fatal(err)
	}
	route, err = fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID())
	if err != nil || route.URL != "https://second.trycloudflare.com" {
		t.Fatalf("rotated route=%+v err=%v", route, err)
	}

	rendezvousOnly := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteRendezvous, URL: "https://rendezvous.example",
		Topic: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
	}})
	result, err = fixture.app.ImportPeerConnectionBundle(rendezvousOnly)
	if err != nil {
		t.Fatal(err)
	}
	if result.QuickTunnel {
		t.Fatalf("Rendezvous-only result=%+v", result)
	}
	if _, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID()); !errors.Is(err, errPeerQuickTunnelRouteUnavailable) {
		t.Fatalf("removed route error=%v", err)
	}
}

func TestImportPeerConnectionBundleRejectsJoinAndInactiveIssuer(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.peerRoutes = map[string]peerQuickTunnelRoute{
		fixture.clientIdentity.PeerID(): {URL: "https://existing.trycloudflare.com", ExpiresAt: time.Now().Add(time.Hour).Unix()},
	}

	invitations, err := fixture.app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1, Permission: string(peerproto.PermissionControl)})
	if err != nil {
		t.Fatal(err)
	}
	hostIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	now := fixture.app.peerSpace.now()
	joinBundle, err := peerproto.NewConnectionBundle(hostIdentity, genesis, invitations[0].Token, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://join.trycloudflare.com",
	}}, now, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.ImportPeerConnectionBundle(joinBundle); !errors.Is(err, errPeerConnectionBundleImportRejected) {
		t.Fatalf("join bundle error=%v", err)
	}

	revocation, err := peerproto.NewRevocation(hostIdentity, genesis, mustMembership(t, fixture.hostMembership, genesis), peerproto.RevocationGrant, mustMembership(t, fixture.clientMembership, genesis).Document.Serial, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.peerSpace.store.ApplyRevocations([]string{revocation.Token}, now); err != nil {
		t.Fatal(err)
	}
	memberBundle := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://revoked.trycloudflare.com",
	}})
	if _, err := fixture.app.ImportPeerConnectionBundle(memberBundle); !errors.Is(err, errPeerConnectionBundleImportRejected) {
		t.Fatalf("revoked issuer error=%v", err)
	}
	fixture.app.peerRouteMu.Lock()
	remaining := fixture.app.peerRoutes[fixture.clientIdentity.PeerID()]
	fixture.app.peerRouteMu.Unlock()
	if remaining.URL != "https://existing.trycloudflare.com" {
		t.Fatalf("rejected import mutated route=%+v", remaining)
	}
}

func TestGetPeerSessionRouteStatusRequiresAuthenticatedCatalogEntry(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	bundle := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://status.trycloudflare.com",
	}})
	if _, err := fixture.app.ImportPeerConnectionBundle(bundle); err != nil {
		t.Fatal(err)
	}
	if got := fixture.app.GetPeerSessionRouteStatus(fixture.session.ID.String()); got != (PeerSessionRouteStatus{}) {
		t.Fatalf("status without catalog=%+v", got)
	}
	fixture.app.peerRendezvous = &peerRendezvousLifecycle{active: &peerRendezvousHost{
		catalog: map[uuid.UUID]peerDiscoveredSession{
			fixture.session.ID: {remote: rendezvousclient.PeerRoute{PeerID: fixture.clientIdentity.PeerID()}},
		},
	}}
	got := fixture.app.GetPeerSessionRouteStatus(fixture.session.ID.String())
	if got.Direct || !got.QuickTunnel {
		t.Fatalf("catalog route status=%+v", got)
	}
	if unknown := fixture.app.GetPeerSessionRouteStatus(uuid.NewString()); unknown != (PeerSessionRouteStatus{}) {
		t.Fatalf("unknown session status=%+v", unknown)
	}
}

func TestPeerLANOnlyHidesCachedPublicRoutesButKeepsManualLAN(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.peerRoutes = map[string]peerQuickTunnelRoute{
		fixture.clientIdentity.PeerID(): {
			URL: "https://cached.trycloudflare.com", ExpiresAt: time.Now().Add(time.Hour).Unix(),
		},
	}
	fixture.app.peerRendezvous = &peerRendezvousLifecycle{active: &peerRendezvousHost{
		route: &rendezvousclient.Route{},
		catalog: map[uuid.UUID]peerDiscoveredSession{
			fixture.session.ID: {remote: rendezvousclient.PeerRoute{PeerID: fixture.clientIdentity.PeerID()}},
		},
	}}
	fixture.app.replacePeerManualCatalog(map[uuid.UUID]peerDiscoveredSession{
		fixture.session.ID: {
			remote:    rendezvousclient.PeerRoute{PeerID: fixture.clientIdentity.PeerID()},
			manualURL: "http://192.168.1.25:8484",
		},
	})
	cfg := fixture.app.cfgStore.Get()
	cfg.PeerLANOnly = true
	if err := fixture.app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID()); !errors.Is(err, errPeerLANOnlyPublicRoute) {
		t.Fatalf("cached Quick Tunnel route error=%v", err)
	}
	if got := fixture.app.GetPeerSessionRouteStatus(fixture.session.ID.String()); got.Direct || got.QuickTunnel || !got.LAN {
		t.Fatalf("LAN-only route status=%+v", got)
	}
	bundle := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://ignored.trycloudflare.com",
	}})
	result, err := fixture.app.ImportPeerConnectionBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if result.QuickTunnel {
		t.Fatalf("LAN-only import activated Quick Tunnel: %+v", result)
	}
}

func newPeerMemberRouteBundle(t *testing.T, fixture peerQuickTunnelFixture, genesis peerproto.VerifiedGenesis, routes []peerproto.ConnectionRoute) string {
	t.Helper()
	token, err := peerproto.NewMemberConnectionBundle(
		fixture.clientIdentity, genesis, fixture.clientMembership, routes, fixture.app.peerSpace.now(), 10*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
