package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestPeerRendezvousHostReusesSessionAndConfigAttachment(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	service, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	syncKey, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	hostIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hostCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, hostIdentity.PeerID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hostRoute, err := newPeerRendezvousHost(ctx, fixture.app, fixture.host, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: hostCoordinates.Topic, PresenceID: hostCoordinates.PresenceID, Role: rendezvous.RoleHost,
	}, webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()
	fixture.peerHost.tunnel = &fakePeerQuickTunnelManager{status: quicktunnel.Status{
		Running: true, PublicURL: "https://catalog-route.trycloudflare.com",
	}}
	fixture.app.quickTunnel = fixture.peerHost
	fixture.app.peerRendezvous = &peerRendezvousLifecycle{active: &peerRendezvousHost{
		serviceURL: "https://rendezvous.example", topic: hostCoordinates.Topic,
	}}
	if _, err := fixture.app.CreatePeerConnectionBundle(""); err != nil {
		t.Fatalf("create catalog connection bundle: %v", err)
	}

	clientCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, fixture.clientIdentity.PeerID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clientConnection, snapshot, err := rendezvousclient.DialPresence(ctx, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: clientCoordinates.Topic, PresenceID: clientCoordinates.PresenceID, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConnection.CloseNow()
	clientRoute, err := rendezvousclient.NewRoute(ctx, rendezvousclient.RouteConfig{
		Connection: clientConnection, InitialPresence: snapshot,
		SpaceID: genesis.Document.SpaceID, EpochKey: syncKey,
		LocalPeerID: fixture.clientIdentity.PeerID(), LocalWrappingIdentity: fixture.clientWrapping,
		ResolvePeer: func(presenceID string) (rendezvousclient.PeerRoute, bool) {
			return rendezvousclient.PeerRoute{
				PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
				WrappingPublicKey: append([]byte(nil), mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey...),
			}, presenceID == hostCoordinates.PresenceID
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientRoute.Close()
	catalog, err := clientRoute.CatalogWithRoutes(ctx, rendezvousclient.PeerRoute{
		PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
		WrappingPublicKey: mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sessions) != 1 || catalog.Sessions[0].ID != fixture.session.ID.String() || catalog.Sessions[0].Permission != peertransport.PermissionControl || catalog.ConnectionBundle == "" {
		t.Fatalf("catalog=%+v", catalog)
	}
	verifiedBundle, err := peerproto.VerifyConnectionBundle(catalog.ConnectionBundle, time.Now())
	if err != nil || verifiedBundle.Ticket != nil || verifiedBundle.Document.IssuerPeerID != hostIdentity.PeerID() {
		t.Fatalf("catalog connection bundle=%+v err=%v", verifiedBundle.Document, err)
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("catalog created %d terminal subscribers", got)
	}

	records := make(chan directClientRecord, 16)
	configMessages := make(chan peerConfigTestRecord, 8)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)
	attempt, err := clientRoute.Dial(ctx, rendezvousclient.ClientAttemptConfig{
		Remote: rendezvousclient.PeerRoute{
			PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
			WrappingPublicKey: mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey,
		},
		Identity: fixture.clientIdentity, GenesisToken: fixture.genesisToken,
		ClientMembershipToken: fixture.clientMembership, HostMembershipToken: fixture.hostMembership,
		SessionID: fixture.session.ID, ClientInstanceID: "rendezvous-desktop-client",
		OnAuthenticated: func(channel *peertransport.PionClientChannel) { clientAuthenticated <- channel },
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			records <- directClientRecord{kind: kind, payload: append([]byte(nil), payload...)}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			configMessages <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	var client *peertransport.PionClientChannel
	select {
	case client = <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("Rendezvous route did not authenticate")
	}

	var gotOutput, gotReady, gotInventory bool
	for !gotOutput || !gotReady || !gotInventory {
		select {
		case record := <-records:
			switch record.kind {
			case peertransport.RecordDirectReady:
				if len(record.payload) != 8 || binary.BigEndian.Uint64(record.payload) != 1 {
					t.Fatalf("DIRECT_READY=%x", record.payload)
				}
				gotReady = true
			case peertransport.RecordFrame:
				frame, err := proto.Unmarshal(record.payload)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Type == proto.TypeOut {
					_, output, _ := proto.DecodeOut(frame.Payload)
					gotOutput = bytes.Equal(output, []byte("peer output"))
				}
			}
		case message := <-configMessages:
			gotInventory = message.kind == peertransport.RecordConfigInventory || gotInventory
		case <-ctx.Done():
			t.Fatal("Rendezvous terminal/config attachment timed out")
		}
	}
	if got := fixture.session.SubscriberCount(); got != 1 {
		t.Fatalf("Rendezvous terminal subscribers=%d want=1", got)
	}
	if token, ok := client.RemoteMembershipToken(); !ok || token != fixture.hostMembership {
		t.Fatal("client lost exact authenticated host membership")
	}
}

func TestPeerRendezvousHostAcceptsConfigOnlyAttemptWithoutTerminalSubscriber(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	service, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	syncKey, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	hostIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hostCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, hostIdentity.PeerID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hostRoute, err := newPeerRendezvousHost(ctx, fixture.app, fixture.host, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: hostCoordinates.Topic, PresenceID: hostCoordinates.PresenceID, Role: rendezvous.RoleHost,
	}, webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()

	clientCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, fixture.clientIdentity.PeerID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clientConnection, snapshot, err := rendezvousclient.DialPresence(ctx, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: clientCoordinates.Topic, PresenceID: clientCoordinates.PresenceID, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConnection.CloseNow()
	clientRoute, err := rendezvousclient.NewRoute(ctx, rendezvousclient.RouteConfig{
		Connection: clientConnection, InitialPresence: snapshot,
		SpaceID: genesis.Document.SpaceID, EpochKey: syncKey,
		LocalPeerID: fixture.clientIdentity.PeerID(), LocalWrappingIdentity: fixture.clientWrapping,
		ResolvePeer: func(presenceID string) (rendezvousclient.PeerRoute, bool) {
			return rendezvousclient.PeerRoute{
				PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
				WrappingPublicKey: append([]byte(nil), mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey...),
			}, presenceID == hostCoordinates.PresenceID
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientRoute.Close()

	configMessages := make(chan peerConfigTestRecord, 4)
	authenticated := make(chan struct{}, 1)
	attempt, err := clientRoute.Dial(ctx, rendezvousclient.ClientAttemptConfig{
		Remote: rendezvousclient.PeerRoute{
			PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
			WrappingPublicKey: mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey,
		},
		Identity: fixture.clientIdentity, GenesisToken: fixture.genesisToken,
		ClientMembershipToken: fixture.clientMembership, HostMembershipToken: fixture.hostMembership,
		SessionID: rendezvousclient.ConfigSyncSessionID(), ClientInstanceID: "config-sync-test",
		OnAuthenticated: func(*peertransport.PionClientChannel) { authenticated <- struct{}{} },
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			configMessages <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	select {
	case <-authenticated:
	case <-ctx.Done():
		t.Fatal("config-only route did not authenticate")
	}
	select {
	case message := <-configMessages:
		if message.kind != peertransport.RecordConfigInventory {
			t.Fatalf("first config message kind=%d", message.kind)
		}
	case <-ctx.Done():
		t.Fatal("config-only route did not advertise inventory")
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("config-only route created %d terminal subscribers", got)
	}
}

func TestPeerRendezvousHostPlansConfigOnlyDialWithoutTerminalSubscriber(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	service, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	syncKey, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	hostIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	hostCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, hostIdentity.PeerID(), now)
	if err != nil {
		t.Fatal(err)
	}
	hostRoute, err := newPeerRendezvousHost(ctx, fixture.app, fixture.host, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: hostCoordinates.Topic, PresenceID: hostCoordinates.PresenceID, Role: rendezvous.RoleHost,
	}, webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()

	clientCoordinates, err := peerdiscovery.DeriveCoordinates(syncKey, genesis.Document.SpaceID, fixture.clientIdentity.PeerID(), now)
	if err != nil {
		t.Fatal(err)
	}
	clientConnection, snapshot, err := rendezvousclient.DialPresence(ctx, rendezvousclient.PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: clientCoordinates.Topic, PresenceID: clientCoordinates.PresenceID, Role: rendezvous.RoleHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConnection.CloseNow()
	configMessages := make(chan peerConfigTestRecord, 4)
	clientRoute, err := rendezvousclient.NewRoute(ctx, rendezvousclient.RouteConfig{
		Connection: clientConnection, InitialPresence: snapshot,
		SpaceID: genesis.Document.SpaceID, EpochKey: syncKey,
		LocalPeerID: fixture.clientIdentity.PeerID(), LocalWrappingIdentity: fixture.clientWrapping,
		ResolvePeer: func(presenceID string) (rendezvousclient.PeerRoute, bool) {
			return rendezvousclient.PeerRoute{
				PeerID: hostIdentity.PeerID(), PresenceID: hostCoordinates.PresenceID,
				WrappingPublicKey: mustMembership(t, fixture.hostMembership, genesis).WrappingPublicKey,
			}, presenceID == hostCoordinates.PresenceID
		},
		AuthorizeHost: func(_ context.Context, request rendezvousclient.PeerOpenRequest) (rendezvousclient.HostAuthorization, rendezvousclient.HostCallbacks, error) {
			if request.SessionID != rendezvousclient.ConfigSyncSessionID() {
				return rendezvousclient.HostAuthorization{}, rendezvousclient.HostCallbacks{}, rendezvousclient.ErrAuthentication
			}
			return rendezvousclient.HostAuthorization{
					Identity: fixture.clientIdentity, GenesisToken: fixture.genesisToken,
					ClientMembershipToken: fixture.hostMembership, HostMembershipToken: fixture.clientMembership,
					Permission: peertransport.PermissionView,
				}, rendezvousclient.HostCallbacks{
					OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
						configMessages <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
						return nil
					},
				}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientRoute.Close()
	deadline := time.Now().Add(2 * time.Second)
	for hostRoute.route.OnlinePeerCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	started, err := hostRoute.syncPlannedConfigRoutes(now)
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("planned config dials=%d want=1", started)
	}
	select {
	case message := <-configMessages:
		if message.kind != peertransport.RecordConfigInventory {
			t.Fatalf("first config message kind=%d", message.kind)
		}
	case <-ctx.Done():
		t.Fatal("planned config-only route did not advertise inventory")
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("planned config-only route created %d terminal subscribers", got)
	}
}

func TestPeerRendezvousCatalogValidationEnforcesSessionScopeAndPermission(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	genesis, err := peerproto.VerifyGenesis(fixture.genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	clientMembership := mustMembership(t, fixture.clientMembership, genesis)
	host := &peerRendezvousHost{app: fixture.app}
	remote := rendezvousclient.PeerRoute{
		PeerID: fixture.clientIdentity.PeerID(), PresenceID: "unused",
		WrappingPublicKey: clientMembership.WrappingPublicKey,
	}
	accepted, err := host.validateCatalog(remote, []rendezvousclient.PeerSession{
		{
			ID: fixture.session.ID.String(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
			Permission: peertransport.PermissionControl,
		},
		{
			ID: uuid.NewString(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
			Permission: peertransport.PermissionView,
		},
		{
			ID: fixture.session.ID.String(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
			Permission: peertransport.PermissionFull,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 1 || accepted[fixture.session.ID].info.RemotePermission != proto.RemotePermissionControl {
		t.Fatalf("unauthorized catalog entry changed accepted result: %+v", accepted)
	}

	accepted, err = host.validateCatalog(remote, []rendezvousclient.PeerSession{{
		ID: fixture.session.ID.String(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
		Permission: peertransport.PermissionControl,
	}})
	if err != nil || len(accepted) != 1 {
		t.Fatalf("authorized catalog=%+v err=%v", accepted, err)
	}
}

func TestPeerRendezvousCatalogAutomaticallyRotatesVerifiedQuickTunnelRoute(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	genesis, err := peerproto.VerifyGenesis(fixture.genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	clientMembership := mustMembership(t, fixture.clientMembership, genesis)
	host := &peerRendezvousHost{app: fixture.app}
	remote := rendezvousclient.PeerRoute{
		PeerID: fixture.clientIdentity.PeerID(), PresenceID: "unused",
		WrappingPublicKey: clientMembership.WrappingPublicKey,
	}
	session := rendezvousclient.PeerSession{
		ID: fixture.session.ID.String(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
		Permission: peertransport.PermissionControl,
	}
	bundle := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://automatic.trycloudflare.com",
	}})
	accepted, err := host.validateAndApplyCatalog(remote, rendezvousclient.PeerCatalog{
		Sessions: []rendezvousclient.PeerSession{session}, ConnectionBundle: bundle,
	})
	if err != nil || len(accepted) != 1 {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	route, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID())
	if err != nil || route.URL != "https://automatic.trycloudflare.com" {
		t.Fatalf("automatic route=%+v err=%v", route, err)
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("route catalog created %d terminal subscribers", got)
	}

	rendezvousOnly := newPeerMemberRouteBundle(t, fixture, genesis, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteRendezvous, URL: "https://rendezvous.example",
		Topic: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32)),
	}})
	if _, err := host.validateAndApplyCatalog(remote, rendezvousclient.PeerCatalog{
		Sessions: []rendezvousclient.PeerSession{session}, ConnectionBundle: rendezvousOnly,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID()); !errors.Is(err, errPeerQuickTunnelRouteUnavailable) {
		t.Fatalf("Rendezvous-only refresh route error=%v", err)
	}
}

func TestPeerRendezvousCatalogRejectsMismatchedBundleAndV1LeavesRouteUntouched(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	genesis, err := peerproto.VerifyGenesis(fixture.genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.peerRoutes = map[string]peerQuickTunnelRoute{
		fixture.clientIdentity.PeerID(): {
			URL: "https://existing.trycloudflare.com", ExpiresAt: time.Now().Add(time.Hour).Unix(),
		},
	}
	clientMembership := mustMembership(t, fixture.clientMembership, genesis)
	host := &peerRendezvousHost{app: fixture.app}
	remote := rendezvousclient.PeerRoute{
		PeerID: fixture.clientIdentity.PeerID(), PresenceID: "unused",
		WrappingPublicKey: clientMembership.WrappingPublicKey,
	}
	sessions := []rendezvousclient.PeerSession{{
		ID: fixture.session.ID.String(), Cols: 80, Rows: 24, HostID: fixture.host.hostID,
		Permission: peertransport.PermissionControl,
	}}
	hostIdentity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	mismatchedBundle, err := peerproto.NewMemberConnectionBundle(
		hostIdentity, genesis, fixture.hostMembership,
		[]peerproto.ConnectionRoute{{Kind: peerproto.RouteQuickTunnel, URL: "https://mismatch.trycloudflare.com"}},
		fixture.app.peerSpace.now(), 10*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.validateAndApplyCatalog(remote, rendezvousclient.PeerCatalog{
		Sessions: sessions, ConnectionBundle: mismatchedBundle,
	}); !errors.Is(err, rendezvousclient.ErrAuthentication) {
		t.Fatalf("issuer mismatch catalog error=%v", err)
	}
	if _, err := host.validateAndApplyCatalog(remote, rendezvousclient.PeerCatalog{Sessions: sessions}); err != nil {
		t.Fatal(err)
	}
	route, err := fixture.app.peerQuickTunnelRoute(fixture.clientIdentity.PeerID())
	if err != nil || route.URL != "https://existing.trycloudflare.com" {
		t.Fatalf("v1 fallback mutated route=%+v err=%v", route, err)
	}
}

func mustMembership(t *testing.T, token string, genesis peerproto.VerifiedGenesis) peerproto.VerifiedGrant {
	t.Helper()
	membership, err := peerproto.VerifyGrant(token, genesis, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return membership
}
