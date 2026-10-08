package rendezvousclient

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"
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

func TestRendezvousWireContainsNoPeerSecretsOrDataChannelPlaintext(t *testing.T) {
	recorder := &rendezvousWireRecorder{}
	fixture := newRouteFixtureWithService(t, func(backendURL string) string {
		return recorder.proxy(t, backendURL)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	hostAuthenticated := make(chan *peertransport.PionHostChannel, 1)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)
	terminalReceived := make(chan []byte, 1)
	configReceived := make(chan []byte, 1)
	metadataMarker := "private-session-title-marker"
	bundleMarker := "private-signed-route-bundle-marker"
	hostRoute := fixture.newHostRouteWithCatalogAndBundle(t, ctx, func(context.Context, PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		return fixture.hostAuthorization(), HostCallbacks{
			OnAuthenticated: func(channel *peertransport.PionHostChannel) { hostAuthenticated <- channel },
			OnRecord: func(_ peertransport.RecordKind, payload []byte) {
				terminalReceived <- append([]byte(nil), payload...)
			},
			OnConfigMessage: func(_ peertransport.RecordKind, payload []byte) error {
				configReceived <- append([]byte(nil), payload...)
				return nil
			},
		}, nil
	}, func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
		return []PeerSession{{
			ID: fixture.sessionID.String(), Title: metadataMarker, Cols: 80, Rows: 24,
			StartedAt: time.Now().Unix(), HostID: fixture.hostIdentity.PeerID(),
			Permission: peertransport.PermissionControl,
		}}, nil
	}, func(context.Context, PeerCatalogRequest) (string, error) {
		return bundleMarker, nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	clientInstanceID := "private-client-instance-marker"
	attempt, err := clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: fixture.hostPeer(), Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.sessionID,
		ClientInstanceID: clientInstanceID, WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) { clientAuthenticated <- channel },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()

	select {
	case <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host membership handshake did not authenticate")
	}
	var clientChannel *peertransport.PionClientChannel
	select {
	case clientChannel = <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("client membership handshake did not authenticate")
	}

	terminalMarker := []byte("private-terminal-frame-marker")
	if err := clientChannel.SendRecord(ctx, peertransport.RecordFrame, terminalMarker); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-terminalReceived:
		if !bytes.Equal(got, terminalMarker) {
			t.Fatalf("terminal marker=%q", got)
		}
	case <-ctx.Done():
		t.Fatal("terminal marker did not cross the DataChannel")
	}

	configMarker := []byte("private-config-sync-marker")
	if err := clientChannel.SendConfigMessage(ctx, peertransport.RecordConfigInventory, configMarker); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-configReceived:
		if !bytes.Equal(got, configMarker) {
			t.Fatalf("config marker=%q", got)
		}
	case <-ctx.Done():
		t.Fatal("config marker did not cross the DataChannel")
	}
	catalog, err := clientRoute.CatalogWithRoutes(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sessions) != 1 || catalog.Sessions[0].Title != metadataMarker || catalog.ConnectionBundle != bundleMarker {
		t.Fatalf("catalog=%+v", catalog)
	}

	sdpMarker := "private-sdp-offer-marker"
	if err := clientRoute.sendConfirmed(ctx, fixture.hostPeer(), routeWireMessage{
		Version: routeWireVersion, Kind: routeKindSignal, AttemptID: uuid.New(),
		SignalType: "offer", Payload: sdpMarker,
	}); err != nil {
		t.Fatal(err)
	}

	wire := recorder.bytes()
	if !bytes.Contains(wire, []byte(`"kind":"publish"`)) {
		t.Fatal("wire recorder did not observe Rendezvous publish traffic")
	}
	for _, sensitive := range []struct {
		name   string
		secret string
	}{
		{name: "invitation", secret: fixture.invitationToken},
		{name: "host membership", secret: fixture.hostMembership},
		{name: "client membership", secret: fixture.clientMembership},
		{name: "session id", secret: fixture.sessionID.String()},
		{name: "session metadata", secret: metadataMarker},
		{name: "connection bundle", secret: bundleMarker},
		{name: "client instance", secret: clientInstanceID},
		{name: "SDP", secret: sdpMarker},
		{name: "config", secret: string(configMarker)},
		{name: "terminal", secret: string(terminalMarker)},
	} {
		if bytes.Contains(wire, []byte(sensitive.secret)) {
			t.Fatalf("Rendezvous wire exposed %s plaintext", sensitive.name)
		}
	}
}

func TestRouteConnectsConfigSyncOutsideTerminalSessionScope(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hostAuthenticated := make(chan *peertransport.PionHostChannel, 1)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)

	hostRoute := fixture.newHostRoute(t, ctx, func(_ context.Context, request PeerOpenRequest) (HostAuthorization, HostCallbacks, error) {
		if request.SessionID != ConfigSyncSessionID() {
			return HostAuthorization{}, HostCallbacks{}, ErrAuthentication
		}
		authorization := fixture.hostAuthorization()
		authorization.Permission = peertransport.PermissionView
		return authorization, HostCallbacks{
			OnAuthenticated: func(channel *peertransport.PionHostChannel) { hostAuthenticated <- channel },
		}, nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	attempt, err := clientRoute.Dial(ctx, ClientAttemptConfig{
		Remote: fixture.hostPeer(), Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: ConfigSyncSessionID(),
		ClientInstanceID: "config-sync", WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) { clientAuthenticated <- channel },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()

	select {
	case <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("config-sync host did not authenticate")
	}
	select {
	case <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("config-sync client did not authenticate")
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

func TestRouteOnlinePeerCountTracksPresenceWithoutExposingIdentifiers(t *testing.T) {
	route := &Route{
		cfg:    RouteConfig{Connection: &PresenceConnection{presenceID: encodedID(1, 32)}},
		online: make(map[string]rendezvous.Role),
	}
	first := encodedID(2, 32)
	second := encodedID(3, 32)

	route.handlePresence(rendezvous.EventMessage{
		Kind: rendezvous.KindPresence, Event: rendezvous.PresenceOnline,
		PresenceID: first, Role: rendezvous.RoleHost,
	})
	route.handlePresence(rendezvous.EventMessage{
		Kind: rendezvous.KindPresence, Event: rendezvous.PresenceOnline,
		PresenceID: second, Role: rendezvous.RoleMember,
	})
	if got := route.OnlinePeerCount(); got != 2 {
		t.Fatalf("online peer count=%d want=2", got)
	}

	route.handlePresence(rendezvous.EventMessage{
		Kind: rendezvous.KindPresence, Event: rendezvous.PresenceOffline,
		PresenceID: first, Role: rendezvous.RoleHost,
	})
	if got := route.OnlinePeerCount(); got != 1 {
		t.Fatalf("online peer count after offline=%d want=1", got)
	}
}

func TestRouteCatalogReturnsAuthorizedSessionsWithoutOpeningTerminalAttempt(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	requests := make(chan PeerCatalogRequest, 2)
	hostRoute, err := NewRoute(ctx, RouteConfig{
		Connection: fixture.hostConnection, InitialPresence: fixture.hostSnapshot,
		SpaceID: fixture.spaceID, EpochKey: fixture.epoch, LocalPeerID: fixture.hostIdentity.PeerID(),
		LocalWrappingIdentity: fixture.hostWrapping,
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := fixture.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		CatalogHost: func(_ context.Context, request PeerCatalogRequest) ([]PeerSession, error) {
			requests <- request
			result := make([]PeerSession, routeCatalogPageSize+1)
			for index := range result {
				result[index] = PeerSession{
					ID: uuid.NewString(), Command: "zsh", Cwd: "/tmp", Title: "terminal",
					Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(),
					Permission: peertransport.PermissionControl,
				}
			}
			return result, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	sessions, err := clientRoute.Catalog(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != routeCatalogPageSize+1 || sessions[0].Permission != peertransport.PermissionControl {
		t.Fatalf("catalog=%+v", sessions)
	}
	select {
	case request := <-requests:
		if request.ClientPeerID != fixture.clientIdentity.PeerID() {
			t.Fatalf("catalog client=%q", request.ClientPeerID)
		}
	default:
		t.Fatal("host catalog callback was not called")
	}
	hostRoute.mu.Lock()
	attempts := len(hostRoute.hosts)
	hostRoute.mu.Unlock()
	if attempts != 0 {
		t.Fatalf("catalog created %d terminal attempts", attempts)
	}
}

func TestRouteCatalogWithRoutesReturnsBundleAndKeepsLegacyCatalogUnchanged(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bundleRequests := make(chan PeerCatalogRequest, 2)
	hostRoute := fixture.newHostRouteWithCatalogAndBundle(t, ctx, nil, func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
		result := make([]PeerSession, routeCatalogPageSize+1)
		for index := range result {
			result[index] = PeerSession{
				ID: uuid.NewString(), Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(),
				Permission: peertransport.PermissionControl,
			}
		}
		return result, nil
	}, func(_ context.Context, request PeerCatalogRequest) (string, error) {
		bundleRequests <- request
		return "signed-member-route-bundle", nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	legacy, err := clientRoute.Catalog(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != routeCatalogPageSize+1 {
		t.Fatalf("legacy entries=%d", len(legacy))
	}
	select {
	case request := <-bundleRequests:
		t.Fatalf("legacy catalog requested bundle for %q", request.ClientPeerID)
	default:
	}

	catalog, err := clientRoute.CatalogWithRoutes(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sessions) != routeCatalogPageSize+1 || catalog.ConnectionBundle != "signed-member-route-bundle" {
		t.Fatalf("route catalog=%+v", catalog)
	}
	select {
	case request := <-bundleRequests:
		if request.ClientPeerID != fixture.clientIdentity.PeerID() {
			t.Fatalf("bundle client=%q", request.ClientPeerID)
		}
	default:
		t.Fatal("route catalog did not request a bundle")
	}
	select {
	case request := <-bundleRequests:
		t.Fatalf("route catalog requested bundle again for %q", request.ClientPeerID)
	default:
	}
}

func TestRouteCatalogWithRoutesCachesV1OnlyPeerAfterProbe(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	catalogCalls := 0
	hostRoute := fixture.newHostRouteWithCatalog(t, ctx, nil, func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
		catalogCalls++
		return []PeerSession{{
			ID: uuid.NewString(), Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(),
			Permission: peertransport.PermissionView,
		}}, nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	for range 2 {
		catalog, err := clientRoute.CatalogWithRoutes(ctx, fixture.hostPeer())
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog.Sessions) != 1 || catalog.ConnectionBundle != "" {
			t.Fatalf("fallback catalog=%+v", catalog)
		}
	}
	if catalogCalls != 2 {
		t.Fatalf("v1 catalog calls=%d want=2", catalogCalls)
	}
	clientRoute.mu.Lock()
	capability := clientRoute.catalogRoutes[fixture.hostIdentity.PeerID()]
	clientRoute.mu.Unlock()
	if capability != catalogRoutesUnsupported {
		t.Fatalf("cached capability=%d want unsupported", capability)
	}
}

func TestRouteCatalogWithRoutesKeepsSupportedCapabilityAcrossTransientFailure(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bundleAvailable := true
	hostRoute := fixture.newHostRouteWithCatalogAndBundle(t, ctx, nil, func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
		return []PeerSession{{
			ID: uuid.NewString(), Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(),
			Permission: peertransport.PermissionView,
		}}, nil
	}, func(context.Context, PeerCatalogRequest) (string, error) {
		if !bundleAvailable {
			return "", ErrServiceUnavailable
		}
		return "signed-member-route-bundle", nil
	})
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	first, err := clientRoute.CatalogWithRoutes(ctx, fixture.hostPeer())
	if err != nil || first.ConnectionBundle == "" {
		t.Fatalf("first catalog=%+v err=%v", first, err)
	}
	bundleAvailable = false
	second, err := clientRoute.CatalogWithRoutes(ctx, fixture.hostPeer())
	if err != nil || len(second.Sessions) != 1 || second.ConnectionBundle != "" {
		t.Fatalf("fallback catalog=%+v err=%v", second, err)
	}
	clientRoute.mu.Lock()
	capability := clientRoute.catalogRoutes[fixture.hostIdentity.PeerID()]
	clientRoute.mu.Unlock()
	if capability != catalogRoutesSupported {
		t.Fatalf("cached capability=%d want supported", capability)
	}
}

func TestCatalogRouteBundleMustFitEncryptedEnvelope(t *testing.T) {
	_, err := catalogPageResponse(
		routeKindCatalogRoutesOK, "host", uuid.New(), 0, 0, nil,
		strings.Repeat("x", maxSignalPlaintext),
	)
	if !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("oversized route bundle error=%v", err)
	}
}

func TestRouteCatalogRejectsUnauthorizedAndOversizedResponses(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hostRoute, err := NewRoute(ctx, RouteConfig{
		Connection: fixture.hostConnection, InitialPresence: fixture.hostSnapshot,
		SpaceID: fixture.spaceID, EpochKey: fixture.epoch, LocalPeerID: fixture.hostIdentity.PeerID(),
		LocalWrappingIdentity: fixture.hostWrapping,
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := fixture.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		CatalogHost: func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
			return []PeerSession{{
				ID: uuid.NewString(), Title: string(bytes.Repeat([]byte("x"), routeMaxMetadataSize+1)),
				Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(), Permission: peertransport.PermissionView,
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	if _, err := clientRoute.Catalog(ctx, fixture.hostPeer()); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("oversized catalog error=%v", err)
	}
}

func TestRouteCatalogShrinksMaximalMetadataPagesToFitEncryptedEnvelope(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	maximal := string(bytes.Repeat([]byte("x"), routeMaxMetadataSize))
	hostRoute, err := NewRoute(ctx, RouteConfig{
		Connection: fixture.hostConnection, InitialPresence: fixture.hostSnapshot,
		SpaceID: fixture.spaceID, EpochKey: fixture.epoch, LocalPeerID: fixture.hostIdentity.PeerID(),
		LocalWrappingIdentity: fixture.hostWrapping,
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := fixture.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		CatalogHost: func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
			result := make([]PeerSession, routeCatalogPageSize)
			for index := range result {
				result[index] = PeerSession{
					ID: uuid.NewString(), Command: maximal, Cwd: maximal, Title: maximal,
					Cols: 80, Rows: 24, HostID: fixture.hostIdentity.PeerID(), Host: maximal,
					User: maximal, SSHHostID: maximal, Permission: peertransport.PermissionView,
					TaskState: maximal, CurrentCommand: maximal, Type: maximal,
				}
			}
			return result, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	sessions, err := clientRoute.Catalog(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != routeCatalogPageSize {
		t.Fatalf("catalog entries=%d want=%d", len(sessions), routeCatalogPageSize)
	}
}

func TestRouteCatalogCapsHostSnapshotAtMaximumEntries(t *testing.T) {
	fixture := newRouteFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hostRoute, err := NewRoute(ctx, RouteConfig{
		Connection: fixture.hostConnection, InitialPresence: fixture.hostSnapshot,
		SpaceID: fixture.spaceID, EpochKey: fixture.epoch, LocalPeerID: fixture.hostIdentity.PeerID(),
		LocalWrappingIdentity: fixture.hostWrapping,
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := fixture.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		CatalogHost: func(context.Context, PeerCatalogRequest) ([]PeerSession, error) {
			result := make([]PeerSession, routeMaxCatalogEntries+1)
			for index := range result {
				result[index] = PeerSession{
					ID: uuid.NewString(), Cols: 80, Rows: 24,
					HostID: fixture.hostIdentity.PeerID(), Permission: peertransport.PermissionView,
				}
			}
			return result, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hostRoute.Close()
	clientRoute := fixture.newClientRoute(t, ctx)
	defer clientRoute.Close()

	sessions, err := clientRoute.Catalog(ctx, fixture.hostPeer())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != routeMaxCatalogEntries {
		t.Fatalf("catalog entries=%d want=%d", len(sessions), routeMaxCatalogEntries)
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
	invitationToken  string
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
	return newRouteFixtureWithService(t, nil)
}

func newRouteFixtureWithService(t *testing.T, wrap func(string) string) *routeFixture {
	t.Helper()
	now := time.Now()
	sessionID := uuid.New()
	hostIdentity, _ := peercrypto.GenerateIdentity()
	hostWrapping := testWrappingIdentity(t)
	genesisToken, hostMembership, err := peerproto.NewSpace(hostIdentity, hostWrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(genesisToken)
	hostGrant, _ := peerproto.VerifyGrant(hostMembership, genesis, now)
	invitations, err := peerproto.NewInvitationBatch(hostIdentity, genesis, hostGrant, now, peerproto.InvitationOptions{
		Count: 1, AllowedSessionIDs: []string{sessionID.String()},
	})
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
	serviceURL := server.URL
	if wrap != nil {
		serviceURL = wrap(serviceURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	topic := encodedID(40, 32)
	hostPresence := encodedID(41, 32)
	clientPresence := encodedID(42, 32)
	hostConnection, hostSnapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: serviceURL, AllowInsecureLoopback: true, Topic: topic,
		PresenceID: hostPresence, Role: rendezvous.RoleHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hostConnection.CloseNow)
	clientConnection, clientSnapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: serviceURL, AllowInsecureLoopback: true, Topic: topic,
		PresenceID: clientPresence, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clientConnection.CloseNow)
	return &routeFixture{
		server: server, service: service, epoch: epoch, spaceID: genesis.Document.SpaceID,
		genesisToken: genesisToken, invitationToken: invitations[0],
		hostIdentity: hostIdentity, hostWrapping: hostWrapping,
		hostMembership: hostMembership, hostPresence: hostPresence, hostConnection: hostConnection,
		hostSnapshot: hostSnapshot, clientIdentity: clientIdentity, clientWrapping: clientWrapping,
		clientMembership: clientMembership, clientPresence: clientPresence,
		clientConnection: clientConnection, clientSnapshot: clientSnapshot, topic: topic,
		sessionID: sessionID,
	}
}

type rendezvousWireRecorder struct {
	mu       sync.Mutex
	messages [][]byte
}

func (r *rendezvousWireRecorder) proxy(t *testing.T, backendURL string) string {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		upstreamURL := "ws" + strings.TrimPrefix(backendURL, "http") + request.URL.Path
		upstream, _, err := websocket.Dial(request.Context(), upstreamURL, &websocket.DialOptions{
			Subprotocols: []string{rendezvous.Subprotocol},
		})
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer upstream.CloseNow()
		downstream, err := websocket.Accept(w, request, &websocket.AcceptOptions{
			Subprotocols: []string{rendezvous.Subprotocol}, InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer downstream.CloseNow()

		ctx, cancel := context.WithCancel(request.Context())
		defer cancel()
		done := make(chan struct{}, 2)
		go r.copy(ctx, upstream, downstream, done)
		go r.copy(ctx, downstream, upstream, done)
		<-done
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func (r *rendezvousWireRecorder) copy(ctx context.Context, destination, source *websocket.Conn, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for {
		messageType, payload, err := source.Read(ctx)
		if err != nil {
			return
		}
		r.mu.Lock()
		r.messages = append(r.messages, append([]byte(nil), payload...))
		r.mu.Unlock()
		if err := destination.Write(ctx, messageType, payload); err != nil {
			return
		}
	}
}

func (r *rendezvousWireRecorder) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Join(r.messages, []byte{'\n'})
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
	return f.newHostRouteWithCatalog(t, ctx, authorize, nil)
}

func (f *routeFixture) newHostRouteWithCatalog(t *testing.T, ctx context.Context, authorize HostAuthorizeFunc, catalog HostCatalogFunc) *Route {
	return f.newHostRouteWithCatalogAndBundle(t, ctx, authorize, catalog, nil)
}

func (f *routeFixture) newHostRouteWithCatalogAndBundle(
	t *testing.T,
	ctx context.Context,
	authorize HostAuthorizeFunc,
	catalog HostCatalogFunc,
	bundle HostConnectionBundleFunc,
) *Route {
	t.Helper()
	route, err := NewRoute(ctx, RouteConfig{
		Connection: f.hostConnection, InitialPresence: f.hostSnapshot,
		SpaceID: f.spaceID, EpochKey: f.epoch, LocalPeerID: f.hostIdentity.PeerID(),
		LocalWrappingIdentity: f.hostWrapping, WebRTC: webrtc.Configuration{},
		ResolvePeer: func(presenceID string) (PeerRoute, bool) {
			peer := f.clientPeer()
			return peer, presenceID == peer.PresenceID
		},
		AuthorizeHost: authorize, CatalogHost: catalog, ConnectionBundleHost: bundle,
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
