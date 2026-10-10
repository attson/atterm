package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
)

type peerNativeTestChannel struct {
	remoteMembership string

	mu       sync.Mutex
	frames   [][]byte
	services [][]byte
	configs  []peertransport.RecordKind
	closed   bool
}

func (c *peerNativeTestChannel) SendRecord(context.Context, peertransport.RecordKind, []byte) error {
	return nil
}

func (c *peerNativeTestChannel) SendFrame(_ context.Context, frame []byte) error {
	c.mu.Lock()
	c.frames = append(c.frames, append([]byte(nil), frame...))
	c.mu.Unlock()
	return nil
}

func (c *peerNativeTestChannel) SendServiceMessage(_ context.Context, payload []byte) error {
	c.mu.Lock()
	c.services = append(c.services, append([]byte(nil), payload...))
	c.mu.Unlock()
	return nil
}

func (c *peerNativeTestChannel) SendConfigMessage(_ context.Context, kind peertransport.RecordKind, _ []byte) error {
	c.mu.Lock()
	c.configs = append(c.configs, kind)
	c.mu.Unlock()
	return nil
}

func (c *peerNativeTestChannel) RemoteMembershipToken() (string, bool) {
	return c.remoteMembership, c.remoteMembership != ""
}

func (c *peerNativeTestChannel) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func TestStartPeerNativeDirectRejectsUnknownOrUnverifiedRoutes(t *testing.T) {
	sessionID := uuid.New()
	app := &App{
		ctx: context.Background(),
		peerRendezvous: &peerRendezvousLifecycle{active: &peerRendezvousHost{
			catalog: map[uuid.UUID]peerDiscoveredSession{
				sessionID: {remote: rendezvousclient.PeerRoute{PeerID: "peer-host"}},
			},
		}},
	}
	request := NativeDirectStartRequest{
		ID: uuid.NewString(), SessionID: sessionID.String(), ClientInstanceID: "peer-client",
	}
	request.Route = "relay"
	if err := app.StartPeerNativeDirect(request); err == nil || err.Error() != "unsupported Peer native route" {
		t.Fatalf("unknown route error=%v", err)
	}
	request.Route = peerNativeRouteQuickTunnel
	if err := app.StartPeerNativeDirect(request); !errors.Is(err, errPeerQuickTunnelRouteUnavailable) {
		t.Fatalf("unverified Quick Tunnel route error=%v", err)
	}
}

func TestStartPeerNativeDirectLANOnlyAllowsLANAndRejectsPublicRoutes(t *testing.T) {
	sessionID := uuid.New()
	app := newRelayTestApp(t)
	app.replacePeerManualCatalog(map[uuid.UUID]peerDiscoveredSession{
		sessionID: {
			remote:    rendezvousclient.PeerRoute{PeerID: "peer-host"},
			manualURL: "http://127.0.0.1:1",
		},
	})
	cfg := app.cfgStore.Get()
	cfg.PeerLANOnly = true
	if err := app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	request := NativeDirectStartRequest{
		ID: uuid.NewString(), SessionID: sessionID.String(), ClientInstanceID: "peer-client",
	}
	for _, route := range []string{peerNativeRouteDirect, peerNativeRouteQuickTunnel} {
		request.ID = uuid.NewString()
		request.Route = route
		if err := app.StartPeerNativeDirect(request); !errors.Is(err, errPeerLANOnlyPublicRoute) {
			t.Fatalf("route %q error=%v", route, err)
		}
	}
	request.ID = uuid.NewString()
	request.Route = peerNativeRouteLAN
	if err := app.StartPeerNativeDirect(request); err != nil {
		t.Fatalf("manual LAN route rejected: %v", err)
	}
	app.stopPeerNativeDirectClients()
}

func TestNativeDirectStartRequestExposesNoEndpointOrCredential(t *testing.T) {
	payload, err := json.Marshal(NativeDirectStartRequest{
		ID: uuid.NewString(), SessionID: uuid.NewString(), ClientInstanceID: "peer-client",
		Route: peerNativeRouteQuickTunnel,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"url", "token", "membership", "account_key"} {
		if _, ok := fields[forbidden]; ok {
			t.Fatalf("renderer request exposed %q: %s", forbidden, payload)
		}
	}
	if got := string(fields["route"]); got != `"quick_tunnel"` {
		t.Fatalf("route=%s payload=%s", got, payload)
	}
}

func TestPeerNativeQuickTunnelChannelUsesSharedEventAndFrameSurface(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	fixture.app.ctx = context.Background()
	events := make(chan NativeDirectEvent, 8)
	fixture.app.eventsEmitter = func(_ context.Context, _ string, data ...interface{}) {
		if len(data) == 1 {
			if event, ok := data[0].(NativeDirectEvent); ok {
				events <- event
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &peerNativeDirectClient{
		id: uuid.NewString(), app: fixture.app, ctx: ctx, cancel: cancel,
		sessionID: fixture.session.ID, clientInstanceID: "peer-wss-client",
		route: peerNativeRouteQuickTunnel,
	}
	fixture.app.peerNative = map[string]*peerNativeDirectClient{client.id: client}
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	client.onAuthenticated(channel)

	ready := make([]byte, 8)
	binary.BigEndian.PutUint64(ready, 7)
	client.handleRecord(peertransport.RecordDirectReady, ready)
	if err := client.sendFrame([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	wantKinds := []string{"diagnostics", "authenticated", "ready"}
	for _, want := range wantKinds {
		select {
		case event := <-events:
			if event.Kind != want || want == "diagnostics" && event.Route != peerNativeRouteQuickTunnel {
				t.Fatalf("event=%+v want kind=%s", event, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s event", want)
		}
	}
	channel.mu.Lock()
	if len(channel.frames) != 1 || len(channel.configs) == 0 || channel.configs[0] != peertransport.RecordConfigInventory {
		t.Fatalf("frames=%v configs=%v", channel.frames, channel.configs)
	}
	channel.mu.Unlock()

	client.stop()
	fixture.app.peerNativeMu.Lock()
	_, registered := fixture.app.peerNative[client.id]
	fixture.app.peerNativeMu.Unlock()
	if registered {
		t.Fatal("stopped Quick Tunnel client remained registered")
	}
}
