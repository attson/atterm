package rendezvousclient

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestRouteConnectsTrustedPeersThroughEncryptedRendezvousSignaling(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hostRecords := make(chan []byte, 1)
	clientRecords := make(chan []byte, 1)
	hostAuthenticated := make(chan *peertransport.PionHostChannel, 1)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)

	hostRoute := fixture.newHostRoute(t, ctx, func(_ context.Context, request PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		if request.ClientPeerID != fixture.clientIdentity.PeerID() || request.SessionID != fixture.sessionID {
			return HostAuthorization{}, HostCallbacks{}, ErrAuthentication
		}
		return fixture.hostAuthorization(), HostCallbacks{
			OnAuthenticated: func(channel *peertransport.PionHostChannel) {
				hostAuthenticated <- channel
				_ = channel.SendRecord(ctx, peertransport.RecordPing, []byte("hostpong"))
			},
			OnRecord: func(_ peertransport.RecordKind, payload []byte) {
				hostRecords <- append([]byte(nil), payload...)
			},
		}, nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	attempt, err := clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: fixture.hostPeer(), Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.sessionID,
		ClientInstanceID: "rendezvous-client", WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			clientAuthenticated <- channel
		},
		OnRecord: func(_ peertransport.RecordKind, payload []byte) {
			clientRecords <- append([]byte(nil), payload...)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()

	var clientChannel *peertransport.PionClientChannel
	select {
	case <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host membership handshake did not authenticate")
	}
	select {
	case clientChannel = <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("client membership handshake did not authenticate")
	}
	select {
	case payload := <-clientRecords:
		if !bytes.Equal(payload, []byte("hostpong")) {
			t.Fatalf("client payload=%q", payload)
		}
	case <-ctx.Done():
		t.Fatal("host record did not cross DataChannel")
	}
	if err := clientChannel.SendRecord(ctx, peertransport.RecordPong, []byte("clientok")); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-hostRecords:
		if !bytes.Equal(payload, []byte("clientok")) {
			t.Fatalf("host payload=%q", payload)
		}
	case <-ctx.Done():
		t.Fatal("client record did not cross DataChannel")
	}
}

func TestRouteClassifiesOfflineAndAuthorizationFailure(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hostCallbacks := 0
	hostRoute := fixture.newHostRoute(t, ctx, func(context.Context, PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		return HostAuthorization{}, HostCallbacks{
			OnAuthenticated: func(*peertransport.PionHostChannel) { hostCallbacks++ },
		}, ErrAuthentication
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	_, err := clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: fixture.hostPeer(), Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.sessionID,
		ClientInstanceID: "rejected-client",
	})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("authorization error=%v", err)
	}
	if hostCallbacks != 0 {
		t.Fatalf("terminal callback ran before authentication: %d", hostCallbacks)
	}

	offline := fixture.hostPeer()
	offline.PresenceID = encodedID(99, 32)
	_, err = clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: offline, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.sessionID,
		ClientInstanceID: "offline-client",
	})
	if !errors.Is(err, ErrPeerOffline) {
		t.Fatalf("offline error=%v", err)
	}
}

func TestRouteIgnoresMalformedPresenceEvents(t *testing.T) {
	route := &Route{
		cfg:    RouteConfig{Connection: &PresenceConnection{presenceID: encodedID(1, 32)}},
		online: make(map[string]rendezvous.Role),
	}
	remote := encodedID(2, 32)

	route.handlePresence(rendezvous.EventMessage{
		Kind: rendezvous.KindPresence, Event: rendezvous.PresenceOnline,
		PresenceID: remote, Role: rendezvous.Role("operator"),
	})
	route.handlePresence(rendezvous.EventMessage{
		Kind: rendezvous.KindPresence, Event: "available",
		PresenceID: remote, Role: rendezvous.RoleHost,
	})
	if len(route.online) != 0 {
		t.Fatalf("malformed presence changed reachability: %+v", route.online)
	}
}

func TestRouteReleasesAuthorizedHostCallbackWhenAuthorizationDocumentsFail(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closed := make(chan error, 1)
	hostRoute := fixture.newHostRoute(t, ctx, func(context.Context, PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		authorization := fixture.hostAuthorization()
		authorization.HostMembershipToken = fixture.clientMembership
		return authorization, HostCallbacks{OnClosed: func(err error) { closed <- err }}, nil
	})
	defer hostRoute.Close()

	hostRoute.handleOpen(fixture.clientPeer(), routeWireMessage{
		Version: routeWireVersion, Kind: routeKindOpen, AttemptID: uuid.New(),
		Ticket: make([]byte, 32), SessionID: fixture.sessionID,
		ClientPeerID: fixture.clientIdentity.PeerID(), ClientInstanceID: "invalid-host-authorization",
	})
	select {
	case err := <-closed:
		if !errors.Is(err, ErrAuthentication) {
			t.Fatalf("cleanup error=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("authorized host callback was not released")
	}
}

func TestRouteClassifiesHostCapacityAsServiceUnavailable(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closed := make(chan error, 1)
	hostRoute := fixture.newHostRoute(t, ctx, func(context.Context, PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		return fixture.hostAuthorization(), HostCallbacks{OnClosed: func(err error) { closed <- err }}, nil
	})
	defer hostRoute.Close()
	hostRoute.mu.Lock()
	for len(hostRoute.hosts) < routeMaxAttempts {
		hostRoute.hosts[uuid.New()] = &hostRouteAttempt{}
	}
	hostRoute.mu.Unlock()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	_, err := clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: fixture.hostPeer(), Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.sessionID,
		ClientInstanceID: "capacity-client",
	})
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("capacity error=%v", err)
	}
	select {
	case err := <-closed:
		if !errors.Is(err, ErrServiceUnavailable) {
			t.Fatalf("cleanup error=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("capacity rejection did not release host callback")
	}
}

type routeFixture struct {
	server           *httptest.Server
	service          *rendezvous.Server
	epoch            configsync.EpochKey
	spaceID          string
	genesisToken     string
	hostIdentity     *peercrypto.Identity
	hostWrapping     *peercrypto.WrappingIdentity
	hostMembership   string
	hostPresence     string
	hostConnection   *PresenceConnection
	hostSnapshot     []rendezvous.Presence
	clientIdentity   *peercrypto.Identity
	clientWrapping   *peercrypto.WrappingIdentity
	clientMembership string
	clientPresence   string
	clientConnection *PresenceConnection
	clientSnapshot   []rendezvous.Presence
	topic            string
	sessionID        uuid.UUID
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	now := time.Now()
	hostIdentity, _ := peercrypto.GenerateIdentity()
	hostWrapping := testWrappingIdentity(t)
	genesisToken, hostMembership, err := peerproto.NewSpace(hostIdentity, hostWrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(genesisToken)
	hostGrant, _ := peerproto.VerifyGrant(hostMembership, genesis, now)
	invitations, err := peerproto.NewInvitationBatch(hostIdentity, genesis, hostGrant, now, peerproto.InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, _ := peercrypto.GenerateIdentity()
	clientWrapping := testWrappingIdentity(t)
	requestToken, _ := peerproto.NewJoinRequest(clientIdentity, clientWrapping.PublicBytes(), invitations[0], now)
	request, _ := peerproto.VerifyJoinRequest(requestToken, genesis, now)
	clientMembership, err := peerproto.IssueMembership(hostIdentity, genesis, hostGrant, request, now)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := configsync.GenerateEpochKey(configsync.KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	service, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	t.Cleanup(service.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	topic := encodedID(40, 32)
	hostPresence := encodedID(41, 32)
	clientPresence := encodedID(42, 32)
	hostConnection, hostSnapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true, Topic: topic,
		PresenceID: hostPresence, Role: rendezvous.RoleHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hostConnection.CloseNow)
	clientConnection, clientSnapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true, Topic: topic,
		PresenceID: clientPresence, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clientConnection.CloseNow)
	return &routeFixture{
		server: server, service: service, epoch: epoch, spaceID: genesis.Document.SpaceID,
		genesisToken: genesisToken, hostIdentity: hostIdentity, hostWrapping: hostWrapping,
		hostMembership: hostMembership, hostPresence: hostPresence, hostConnection: hostConnection,
		hostSnapshot: hostSnapshot, clientIdentity: clientIdentity, clientWrapping: clientWrapping,
		clientMembership: clientMembership, clientPresence: clientPresence,
		clientConnection: clientConnection, clientSnapshot: clientSnapshot, topic: topic,
		sessionID: uuid.New(),
	}
}

func (f *routeFixture) hostPeer() PeerRoute {
	return PeerRoute{PeerID: f.hostIdentity.PeerID(), PresenceID: f.hostPresence, WrappingPublicKey: f.hostWrapping.PublicBytes()}
}

func (f *routeFixture) clientPeer() PeerRoute {
	return PeerRoute{PeerID: f.clientIdentity.PeerID(), PresenceID: f.clientPresence, WrappingPublicKey: f.clientWrapping.PublicBytes()}
}

func (f *routeFixture) hostAuthorization() HostAuthorization {
	return HostAuthorization{
		Identity: f.hostIdentity, GenesisToken: f.genesisToken,
		ClientMembershipToken: f.clientMembership, HostMembershipToken: f.hostMembership,
		Permission: peertransport.PermissionControl,
	}
}

func (f *routeFixture) newHostRoute(t *testing.T, ctx context.Context, authorize HostAuthorizeFunc) *Route {
	t.Helper()
	route, err := NewRoute(ctx, RouteConfig{
		Connection: f.hostConnection, InitialPresence: f.hostSnapshot,
		SpaceID: f.spaceID, EpochKey: f.epoch, LocalPeerID: f.hostIdentity.PeerID(),
		LocalWrappingIdentity: f.hostWrapping, WebRTC: webrtc.Configuration{},
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := f.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		AuthorizeHost: authorize,
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func (f *routeFixture) newClientRoute(t *testing.T, ctx context.Context) *Route {
	t.Helper()
	route, err := NewRoute(ctx, RouteConfig{
		Connection: f.clientConnection, InitialPresence: f.clientSnapshot,
		SpaceID: f.spaceID, EpochKey: f.epoch, LocalPeerID: f.clientIdentity.PeerID(),
		LocalWrappingIdentity: f.clientWrapping,
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := f.hostPeer()
			return peer, presenceID == peer.PresenceID
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}
