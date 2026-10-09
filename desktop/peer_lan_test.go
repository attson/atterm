package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerlan"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/quicktunnel"
)

func TestPeerLANConfigRejectsUnknownFingerprintAndCanonicalizesAddresses(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	request := SetPeerLANConfigReq{Routes: []PeerManualLANRoute{{
		Host: "[2001:0DB8::1]", Port: 8484, Fingerprint: peerFingerprint(fixture.clientIdentity.PeerID()),
	}}}
	if err := fixture.app.SetPeerLANConfig(request); err != nil {
		t.Fatal(err)
	}
	config, err := fixture.app.GetPeerLANConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Routes) != 1 || config.Routes[0].Host != "2001:db8::1" || config.Routes[0].Fingerprint != peerFingerprint(fixture.clientIdentity.PeerID()) {
		t.Fatalf("canonical routes=%+v", config.Routes)
	}

	request.Routes[0].Fingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := fixture.app.SetPeerLANConfig(request); err == nil {
		t.Fatal("unknown device fingerprint was accepted")
	}
	request.Routes[0].Fingerprint = peerFingerprint(fixture.clientIdentity.PeerID())
	for _, host := range []string{"https://192.168.1.3", "bad/name", "2001:db8::zz"} {
		request.Routes[0].Host = host
		if err := fixture.app.SetPeerLANConfig(request); err == nil {
			t.Fatalf("invalid host %q was accepted", host)
		}
	}
}

func TestPeerLANControlCatalogIsAuthenticatedWithoutTerminalSubscriber(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()
	fixture.app.ctx = context.Background()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	responses := make(chan peerLANCatalogResponse, 1)
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: peertransport.ConfigSyncSessionID(),
		ClientInstanceID: "peer-lan-catalog-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer signal.Close()
	if err := signal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnAuthenticated: func(channel *quicktunnel.WSSChannel) {
			payload, _ := json.Marshal(peerLANCatalogRequest{V: peerLANCatalogVersion})
			if err := channel.SendConfigMessage(ctx, peertransport.RecordCatalogRequest, payload); err != nil {
				t.Errorf("send catalog request: %v", err)
			}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			if kind != peertransport.RecordCatalogResponse {
				return nil
			}
			var response peerLANCatalogResponse
			if err := json.Unmarshal(payload, &response); err != nil {
				return err
			}
			responses <- response
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := signal.StartWSSFallback(ctx); err != nil {
		t.Fatal(err)
	}
	var response peerLANCatalogResponse
	select {
	case response = <-responses:
	case <-ctx.Done():
		t.Fatal("catalog response timed out")
	}
	if len(response.Sessions) != 1 || response.Sessions[0].ID != fixture.session.ID.String() {
		t.Fatalf("catalog=%+v", response.Sessions)
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("catalog control route created %d terminal subscribers", got)
	}
}

func TestPeerLANCatalogRejectsReachableEndpointWithWrongDeviceIdentity(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	remote := mustMembership(t, fixture.clientMembership, genesis)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fixture.app.fetchPeerLANCatalog(ctx, resolvedPeerLANRoute{
		URL: server.URL, PeerID: remote.Document.SubjectPeerID, Membership: remote,
	}); err == nil {
		t.Fatal("reachable endpoint authenticated with a different device identity")
	}
}

func TestPeerLANListenerStartsOnlyWhenExplicitlyEnabled(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	if err := fixture.app.reconcilePeerLAN(fixture.app.cfgStore.Get()); err != nil {
		t.Fatal(err)
	}
	if config, _ := fixture.app.GetPeerLANConfig(); config.Running {
		t.Fatal("LAN listener started without explicit enablement")
	}

	request := SetPeerLANConfigReq{Enabled: true, AdvertiseHost: "127.0.0.1", Port: freeTCPPort(t)}
	if err := fixture.app.SetPeerLANConfig(request); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.app.stopPeerLAN)
	config, err := fixture.app.GetPeerLANConfig()
	if err != nil || !config.Running || config.ListenAddress == "" {
		t.Fatalf("LAN listener config=%+v err=%v", config, err)
	}
	route, ok := fixture.app.peerLANConnectionRoute()
	if !ok || route.Kind != peerproto.RouteManualLAN || route.URL == "" {
		t.Fatalf("published LAN route=%+v ok=%t", route, ok)
	}
}

type testPeerLANPublisher struct{ closed bool }

func (p *testPeerLANPublisher) Close() error {
	p.closed = true
	return nil
}

func TestPeerLANAutoDiscoveryPublishesOpaqueTagAndStopsWithConfig(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	var published peerlan.Advertisement
	publisher := &testPeerLANPublisher{}
	fixture.app.peerLANPublish = func(ad peerlan.Advertisement) (peerlan.Publisher, error) {
		published = ad
		return publisher, nil
	}
	request := SetPeerLANConfigReq{
		Enabled: true, AutoDiscovery: true, AdvertiseHost: "127.0.0.1", Port: freeTCPPort(t),
	}
	if err := fixture.app.SetPeerLANConfig(request); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.app.stopPeerLAN)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	localIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	defaultAdvertisement, err := fixture.app.peerLANAdvertisement(appConfig{PeerLANAdvertiseHost: "127.0.0.1"})
	if err != nil || defaultAdvertisement.Port != defaultPeerLANPort {
		t.Fatalf("default advertisement=%+v err=%v", defaultAdvertisement, err)
	}
	expectedTag, err := peerlan.DeriveTag(key, genesis.Document.SpaceID, localIdentity.PeerID())
	if err != nil {
		t.Fatal(err)
	}
	if published.Tag != expectedTag || published.Port != request.Port || len(published.IPs) != 1 || !published.IPs[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("advertisement=%+v expected tag=%q", published, expectedTag)
	}
	if published.Tag == localIdentity.PeerID() || strings.Contains(published.Tag, genesis.Document.SpaceID) {
		t.Fatalf("advertisement leaked stable identity: %+v", published)
	}
	config, err := fixture.app.GetPeerLANConfig()
	if err != nil || !config.Running || !config.DiscoveryRunning || config.DiscoveryLastError != "" {
		t.Fatalf("LAN config=%+v err=%v", config, err)
	}

	request.AutoDiscovery = false
	if err := fixture.app.SetPeerLANConfig(request); err != nil {
		t.Fatal(err)
	}
	if !publisher.closed {
		t.Fatal("mDNS publisher was not stopped when automatic discovery was disabled")
	}
	config, err = fixture.app.GetPeerLANConfig()
	if err != nil || !config.Running || config.DiscoveryRunning {
		t.Fatalf("LAN config after disabling discovery=%+v err=%v", config, err)
	}
}

func TestPeerLANAutoDiscoveryFailureDoesNotStopListener(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	fixture.app.peerLANPublish = func(peerlan.Advertisement) (peerlan.Publisher, error) {
		return nil, errors.New("multicast unavailable")
	}
	if err := fixture.app.SetPeerLANConfig(SetPeerLANConfigReq{
		Enabled: true, AutoDiscovery: true, AdvertiseHost: "127.0.0.1", Port: freeTCPPort(t),
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.app.stopPeerLAN)
	config, err := fixture.app.GetPeerLANConfig()
	if err != nil || !config.Running || config.DiscoveryRunning || config.DiscoveryLastError != "publish_failed" {
		t.Fatalf("LAN config=%+v err=%v", config, err)
	}
}

func TestPeerLANAutoDiscoveryMatchesActiveMembersAndManualRouteWins(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	remoteTag, err := peerlan.DeriveTag(key, genesis.Document.SpaceID, fixture.clientIdentity.PeerID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.peerLANBrowse = func(context.Context, time.Duration) ([]peerlan.Entry, error) {
		return []peerlan.Entry{
			{Tag: remoteTag, IP: net.ParseIP("192.0.2.10"), Port: 8484},
			{Tag: "0123456789abcdef0123456789abcdef", IP: net.ParseIP("192.0.2.11"), Port: 8484},
		}, nil
	}
	cfg := fixture.app.cfgStore.Get()
	cfg.PeerLANAutoDiscovery = true
	if err := fixture.app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	routes, err := fixture.app.resolvedPeerLANRoutes(context.Background())
	if err != nil || len(routes) != 1 || routes[0].PeerID != fixture.clientIdentity.PeerID() || routes[0].URL != "http://192.0.2.10:8484" {
		t.Fatalf("discovered routes=%+v err=%v", routes, err)
	}

	cfg = fixture.app.cfgStore.Get()
	cfg.PeerLANRoutes = []PeerManualLANRoute{{
		Host: "192.0.2.20", Port: 9444, Fingerprint: peerFingerprint(fixture.clientIdentity.PeerID()),
	}}
	if err := fixture.app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	routes, err = fixture.app.resolvedPeerLANRoutes(context.Background())
	if err != nil || len(routes) != 1 || routes[0].URL != "http://192.0.2.20:9444" {
		t.Fatalf("manual precedence routes=%+v err=%v", routes, err)
	}

	fixture.app.peerLANBrowse = func(context.Context, time.Duration) ([]peerlan.Entry, error) {
		return nil, errors.New("browse failed")
	}
	routes, err = fixture.app.resolvedPeerLANRoutes(context.Background())
	if err != nil || len(routes) != 1 || routes[0].URL != "http://192.0.2.20:9444" {
		t.Fatalf("manual fallback routes=%+v err=%v", routes, err)
	}
}

func TestStoppingQuickTunnelLeavesPeerLANListenerRunning(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	request := SetPeerLANConfigReq{Enabled: true, AdvertiseHost: "127.0.0.1", Port: freeTCPPort(t)}
	if err := fixture.app.SetPeerLANConfig(request); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.app.stopPeerLAN)
	if err := fixture.app.StopPeerQuickTunnel(); err != nil {
		t.Fatal(err)
	}
	config, err := fixture.app.GetPeerLANConfig()
	if err != nil || !config.Running || config.ListenAddress == "" {
		t.Fatalf("LAN listener stopped with Quick Tunnel: config=%+v err=%v", config, err)
	}
}

func TestPeerManualLANRouteLimitAllowsReplacementButRejectsNewPeer(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	cfg := fixture.app.cfgStore.Get()
	for index := 0; index < 32; index++ {
		peerID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(index + 1)}, 32))
		cfg.PeerLANRoutes = append(cfg.PeerLANRoutes, PeerManualLANRoute{
			Host: "192.0.2.1", Port: 8484, Fingerprint: peerFingerprint(peerID),
		})
	}
	if err := fixture.app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	firstPeerID, err := peerIDFromFingerprint(cfg.PeerLANRoutes[0].Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.app.savePeerManualLANRoute(firstPeerID, "http://192.0.2.2:9444"); err != nil {
		t.Fatalf("replace route at limit: %v", err)
	}
	updated := fixture.app.cfgStore.Get().PeerLANRoutes
	if len(updated) != 32 || updated[len(updated)-1].Host != "192.0.2.2" || updated[len(updated)-1].Port != 9444 {
		t.Fatalf("replacement routes=%+v", updated)
	}
	newPeerID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{99}, 32))
	if err := fixture.app.savePeerManualLANRoute(newPeerID, "http://192.0.2.3:8484"); err == nil {
		t.Fatal("new route exceeded the manual LAN route limit")
	}
	if got := len(fixture.app.cfgStore.Get().PeerLANRoutes); got != 32 {
		t.Fatalf("route count after rejected append=%d", got)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	gateway, err := quicktunnel.OpenGatewayAt("tcp4", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(gateway.Address())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := gateway.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	return port
}
