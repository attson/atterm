package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

type peerQuickTunnelFixture struct {
	app              *App
	host             *relayHost
	peerHost         *peerQuickTunnelHost
	session          *session.Session
	clientIdentity   *peercrypto.Identity
	clientWrapping   *peercrypto.WrappingIdentity
	genesisToken     string
	clientMembership string
	hostMembership   string
}

type peerConfigTestRecord struct {
	kind    peertransport.RecordKind
	payload []byte
}

func newPeerQuickTunnelFixture(t *testing.T, permission peerproto.Permission) peerQuickTunnelFixture {
	t.Helper()
	app, _ := newTestPeerApp(t)
	now := time.Now().Add(-time.Second).UTC()
	app.peerSpace.now = func() time.Time { return now }
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	host := newTestRelayHost(t)
	host.cfg.cfg.RemotePermission = proto.RemotePermissionFull
	app.host = host
	app.cfgStore = host.cfg

	sessionID := uuid.New()
	sess := session.New(sessionID, proto.SessionInfo{
		ID: sessionID.String(), HostID: host.hostID, Host: host.host, User: host.user,
		Title: "peer terminal", Cwd: "/tmp", Cols: 80, Rows: 24,
	})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatal(err)
	}
	sess.PushOut(1, []byte("peer output"))

	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	hostMembership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := peerproto.NewInvitationBatch(identity, genesis, hostMembership, now, peerproto.InvitationOptions{
		Count: 1, Permission: permission, AllowedSessionIDs: []string{sessionID.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	joinToken, err := peerproto.NewJoinRequest(clientIdentity, clientWrapping.PublicBytes(), tickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	join, err := peerproto.VerifyJoinRequest(joinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	clientMembership, err := peerproto.IssueMembership(identity, genesis, hostMembership, join, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.peerSpace.store.ApplyMemberships([]string{clientMembership}, now); err != nil {
		t.Fatal(err)
	}
	peerHost, err := newPeerQuickTunnelHost(app, host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerHost.Stop() })
	return peerQuickTunnelFixture{
		app: app, host: host, peerHost: peerHost, session: sess,
		clientIdentity: clientIdentity, clientWrapping: clientWrapping, genesisToken: state.GenesisToken,
		clientMembership: clientMembership, hostMembership: state.LocalMembership,
	}
}

func TestPeerQuickTunnelHostAttachesLocalSessionAndConfigChannel(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	records := make(chan directClientRecord, 16)
	configMessages := make(chan peerConfigTestRecord, 8)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.session.ID,
		ClientInstanceID: "peer-client",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer signal.Close()
	attempt, err := signal.BridgePionClient(ctx, quicktunnel.PionClientBridgeConfig{
		WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			clientAuthenticated <- channel
		},
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			records <- directClientRecord{kind: kind, payload: append([]byte(nil), payload...)}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			configMessages <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("BridgePionClient: %v", err)
	}
	defer attempt.Close()

	var client *peertransport.PionClientChannel
	select {
	case client = <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("Pion client did not authenticate")
	}
	if token, ok := client.RemoteMembershipToken(); !ok || token != fixture.hostMembership {
		t.Fatal("client channel lost the exact authenticated host membership")
	}

	var gotOutput, gotReady, gotInventory, gotBatch bool
	for !gotOutput || !gotReady || !gotInventory || !gotBatch {
		select {
		case record := <-records:
			switch record.kind {
			case peertransport.RecordDirectReady:
				if len(record.payload) != 8 || binary.BigEndian.Uint64(record.payload) != 1 {
					t.Fatalf("DIRECT_READY payload = %x", record.payload)
				}
				gotReady = true
			case peertransport.RecordFrame:
				frame, err := proto.Unmarshal(record.payload)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Type != proto.TypeOut {
					continue
				}
				seq, output, err := proto.DecodeOut(frame.Payload)
				if err != nil || seq != 1 || !bytes.Equal(output, []byte("peer output")) {
					t.Fatalf("Peer OUT seq=%d output=%q err=%v", seq, output, err)
				}
				gotOutput = true
			}
		case record := <-configMessages:
			switch record.kind {
			case peertransport.RecordConfigInventory:
				if !gotInventory {
					gotInventory = true
					if err := client.SendConfigMessage(ctx, peertransport.RecordConfigInventory, record.payload); err != nil {
						t.Fatal(err)
					}
				}
			case peertransport.RecordConfigBatch:
				gotBatch = true
			}
		case <-ctx.Done():
			t.Fatal("terminal replay/config inventory not received")
		}
	}
	if got := fixture.session.SubscriberCount(); got != 1 {
		t.Fatalf("terminal subscribers = %d, want exactly one; config sync must not subscribe", got)
	}

	claimPayload, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "peer-client", ClientName: "Peer test"})
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: fixture.session.ID, Payload: claimPayload,
	})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fixture.session.DriverClientID() != "peer-client" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := fixture.session.DriverClientID(); got != "peer-client" {
		t.Fatalf("driver client id = %q", got)
	}
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("peer input"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-fixture.session.Inbound():
		if frame.Type != proto.TypeIn || !bytes.Equal(frame.Payload, []byte("peer input")) {
			t.Fatalf("local inbound = %+v", frame)
		}
	case <-ctx.Done():
		t.Fatal("Peer input did not reach local session")
	}

	fixture.app.cfgStore.mu.Lock()
	fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionView
	fixture.app.cfgStore.mu.Unlock()
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("blocked after downgrade"),
	})); err != nil {
		t.Fatal(err)
	}
	waitForPeerSubscribers(t, fixture.session, 0)
	select {
	case frame := <-fixture.session.Inbound():
		t.Fatalf("owner policy downgrade leaked inbound frame %+v", frame)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPeerHostRouteLeaseReplacesSubscriberAndPreservesDriver(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var subscriberCounts []int
	fixture.session.SetSubscriberCountHook(func(count int) {
		subscriberCounts = append(subscriberCounts, count)
	})

	newAttempt := func() (*peerHostAttempt, *peerNativeTestChannel) {
		attempt := &peerHostAttempt{
			host: fixture.peerHost.runtime, sessionID: fixture.session.ID,
			permission: proto.RemotePermissionControl, clientInstanceID: "stable-client-instance",
		}
		attempt.remove = func() { attempt.close(true) }
		channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
		return attempt, channel
	}

	first, firstChannel := newAttempt()
	if err := first.start(ctx, firstChannel); err != nil {
		t.Fatal(err)
	}
	claimPayload, err := json.Marshal(proto.ClaimDriverPayload{ClientID: "logical-client", ClientName: "desktop-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.handleRecord(ctx, peertransport.RecordFrame, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: fixture.session.ID, Payload: claimPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if got := fixture.session.DriverClientID(); got != "logical-client" {
		t.Fatalf("initial driver=%q", got)
	}

	second, secondChannel := newAttempt()
	if err := second.start(ctx, secondChannel); err != nil {
		t.Fatal(err)
	}
	defer second.close(true)
	if got := fixture.session.SubscriberCount(); got != 1 {
		t.Fatalf("subscriber count after route replacement=%d want=1", got)
	}
	if got := fixture.session.DriverClientID(); got != "logical-client" {
		t.Fatalf("replacement driver=%q want preserved identity", got)
	}
	if len(subscriberCounts) != 1 || subscriberCounts[0] != 1 {
		t.Fatalf("subscriber count transitions=%v want [1]", subscriberCounts)
	}
	firstChannel.mu.Lock()
	firstClosed := firstChannel.closed
	firstChannel.mu.Unlock()
	if !firstClosed {
		t.Fatal("superseded route channel remained open")
	}
	if err := first.handleRecord(ctx, peertransport.RecordFrame, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("old"),
	})); err == nil {
		t.Fatal("superseded route was still allowed to send input")
	}
	if err := second.handleRecord(ctx, peertransport.RecordFrame, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("new"),
	})); err != nil {
		t.Fatalf("replacement route input: %v", err)
	}
	select {
	case frame := <-fixture.session.Inbound():
		if string(frame.Payload) != "new" {
			t.Fatalf("inbound payload=%q", frame.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement route input was not delivered")
	}

	first.close(true)
	key := peerHostRouteLeaseKey{
		remotePeerID: fixture.clientIdentity.PeerID(), sessionID: fixture.session.ID,
		clientInstanceID: "stable-client-instance",
	}
	if current := fixture.app.currentPeerHostRouteLease(key); current != second {
		t.Fatalf("late old close changed current lease=%p want=%p", current, second)
	}
}

func TestPeerHostRouteLeaseKeepsDifferentClientInstancesIndependent(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionView)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := func(clientInstanceID string) *peerHostAttempt {
		attempt := &peerHostAttempt{
			host: fixture.peerHost.runtime, sessionID: fixture.session.ID,
			permission: proto.RemotePermissionView, clientInstanceID: clientInstanceID,
		}
		attempt.remove = func() { attempt.close(true) }
		if err := attempt.start(ctx, &peerNativeTestChannel{remoteMembership: fixture.clientMembership}); err != nil {
			t.Fatal(err)
		}
		return attempt
	}

	first := start("client-a")
	defer first.close(true)
	second := start("client-b")
	defer second.close(true)
	if got := fixture.session.SubscriberCount(); got != 2 {
		t.Fatalf("independent subscriber count=%d want=2", got)
	}
	for _, item := range []struct {
		clientID string
		attempt  *peerHostAttempt
	}{{"client-a", first}, {"client-b", second}} {
		key := peerHostRouteLeaseKey{
			remotePeerID: fixture.clientIdentity.PeerID(), sessionID: fixture.session.ID,
			clientInstanceID: item.clientID,
		}
		if current := fixture.app.currentPeerHostRouteLease(key); current != item.attempt {
			t.Fatalf("lease %s=%p want=%p", item.clientID, current, item.attempt)
		}
	}
}

func TestPeerHostRouteLeaseRejectsStaleClaim(t *testing.T) {
	app := &App{}
	key := peerHostRouteLeaseKey{
		remotePeerID: "peer-a", sessionID: uuid.New(), clientInstanceID: "client-a",
	}
	old := &peerHostAttempt{}
	winner := &peerHostAttempt{}
	stale := &peerHostAttempt{}

	if previous, ok := app.claimPeerHostRouteLease(key, nil, old); !ok || previous != nil {
		t.Fatalf("initial claim previous=%p ok=%v", previous, ok)
	}
	app.releasePeerHostRouteLease(key, old)
	if previous, ok := app.claimPeerHostRouteLease(key, old, winner); !ok || previous != old {
		t.Fatalf("replacement claim previous=%p ok=%v", previous, ok)
	}
	if current, ok := app.claimPeerHostRouteLease(key, old, stale); ok || current != winner {
		t.Fatalf("stale claim current=%p ok=%v want winner=%p", current, ok, winner)
	}
	app.releasePeerHostRouteLease(key, old)
	if current := app.currentPeerHostRouteLease(key); current != winner {
		t.Fatalf("old release changed current lease=%p want=%p", current, winner)
	}
}

func TestPeerQuickTunnelHostWSSFallbackAttachesLocalSession(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	records := make(chan directClientRecord, 16)
	configMessages := make(chan peerConfigTestRecord, 8)
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.session.ID,
		ClientInstanceID: "peer-wss-client",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer signal.Close()
	if err := signal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			records <- directClientRecord{kind: kind, payload: append([]byte(nil), payload...)}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			configMessages <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	client, err := signal.StartWSSFallback(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var gotOutput, gotReady, gotInventory bool
	for !gotOutput || !gotReady || !gotInventory {
		select {
		case record := <-records:
			switch record.kind {
			case peertransport.RecordDirectReady:
				gotReady = true
			case peertransport.RecordFrame:
				frame, err := proto.Unmarshal(record.payload)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Type == proto.TypeOut {
					gotOutput = true
				}
			}
		case record := <-configMessages:
			if record.kind == peertransport.RecordConfigInventory {
				gotInventory = true
			}
		case <-ctx.Done():
			t.Fatal("WSS terminal replay/config inventory not received")
		}
	}
	if got := fixture.session.SubscriberCount(); got != 1 {
		t.Fatalf("WSS terminal subscribers = %d, want exactly one", got)
	}

	claimPayload, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "peer-wss-client", ClientName: "Peer WSS test"})
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: fixture.session.ID, Payload: claimPayload,
	})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fixture.session.DriverClientID() != "peer-wss-client" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("wss input"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-fixture.session.Inbound():
		if frame.Type != proto.TypeIn || !bytes.Equal(frame.Payload, []byte("wss input")) {
			t.Fatalf("WSS local inbound = %+v", frame)
		}
	case <-ctx.Done():
		t.Fatal("WSS input did not reach local session")
	}
}

func TestPeerQuickTunnelHostViewGrantCannotClaimDriver(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionView)
	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.session.ID,
		ClientInstanceID: "view-client",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := signal.BridgePionClient(ctx, quicktunnel.PionClientBridgeConfig{
		WebRTC:          webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) { clientAuthenticated <- channel },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	var client *peertransport.PionClientChannel
	select {
	case client = <-clientAuthenticated:
	case <-ctx.Done():
		t.Fatal("view-only Pion client did not authenticate")
	}
	claimPayload, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "view-client", ClientName: "Viewer"})
	if err := client.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: fixture.session.ID, Payload: claimPayload,
	})); err != nil {
		t.Fatal(err)
	}
	waitForPeerSubscribers(t, fixture.session, 0)
	if got := fixture.session.DriverClientID(); got != "" {
		t.Fatalf("view-only member became driver %q", got)
	}
}

func TestPeerQuickTunnelAuthorizationRejectsUnknownRevokedAndWrongSession(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	request := quicktunnel.OpenRequest{
		Version: 1, AttemptID: uuid.New(), Ticket: bytes.Repeat([]byte{1}, 32),
		SessionID: fixture.session.ID, ClientPeerID: fixture.clientIdentity.PeerID(),
		ClientInstanceID: "peer-client",
	}
	authorization, err := fixture.peerHost.authorize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.ClientMembershipToken != fixture.clientMembership {
		t.Fatal("authorizer did not return the canonical exact client membership")
	}
	fixture.app.cfgStore.mu.Lock()
	fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionView
	fixture.app.cfgStore.mu.Unlock()
	authorization, err = fixture.peerHost.authorize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.Permission != peertransport.PermissionView {
		t.Fatalf("effective owner permission = %v, want view", authorization.Permission)
	}
	fixture.app.cfgStore.mu.Lock()
	fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionFull
	fixture.app.cfgStore.mu.Unlock()

	unknown := request
	unknown.ClientPeerID = "unknown-peer"
	if _, err := fixture.peerHost.authorize(context.Background(), unknown); !errors.Is(err, quicktunnel.ErrUnauthorized) {
		t.Fatalf("unknown Peer error = %v", err)
	}
	wrongSession := request
	wrongSession.SessionID = uuid.New()
	if _, err := fixture.peerHost.authorize(context.Background(), wrongSession); !errors.Is(err, quicktunnel.ErrUnauthorized) {
		t.Fatalf("wrong session error = %v", err)
	}

	state, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := fixture.app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	local, err := peerproto.VerifyGrant(state.LocalMembership, genesis, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	client, err := peerproto.VerifyGrant(fixture.clientMembership, genesis, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	revocation, err := peerproto.NewRevocation(identity, genesis, local, peerproto.RevocationGrant, client.Document.Serial, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.peerSpace.store.ApplyRevocations([]string{revocation.Token}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.peerHost.authorize(context.Background(), request); !errors.Is(err, quicktunnel.ErrUnauthorized) {
		t.Fatalf("revoked membership error = %v", err)
	}
}

func TestRevokePeerMemberImmediatelyRemovesQuickTunnelAttempt(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	signal := &quicktunnel.SignalChannel{}
	attempt := &peerHostAttempt{
		host: fixture.peerHost.runtime, signal: signal, sessionID: fixture.session.ID,
		permission: proto.RemotePermissionControl, remoteMembership: fixture.clientMembership,
	}
	// This focused lifecycle test does not construct a signaling socket. Mark
	// cleanup complete so removal only exercises authorization and registry state.
	attempt.closeOnce.Do(func() {})
	fixture.peerHost.attempts[signal] = attempt
	fixture.app.quickTunnel = fixture.peerHost

	if err := fixture.app.RevokePeerMember(fixture.clientIdentity.PeerID()); err != nil {
		t.Fatal(err)
	}
	fixture.peerHost.mu.Lock()
	remaining := len(fixture.peerHost.attempts)
	fixture.peerHost.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("Quick Tunnel attempts after member revocation = %d, want 0", remaining)
	}
	request := quicktunnel.OpenRequest{
		Version: 1, AttemptID: uuid.New(), Ticket: bytes.Repeat([]byte{1}, 32),
		SessionID: fixture.session.ID, ClientPeerID: fixture.clientIdentity.PeerID(), ClientInstanceID: "peer-client",
	}
	if _, err := fixture.peerHost.authorize(context.Background(), request); !errors.Is(err, quicktunnel.ErrUnauthorized) {
		t.Fatalf("revoked member reconnect error = %v", err)
	}
}

func TestPeerQuickTunnelConnectionBundleRotatesOnlyRoute(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	invitations, err := fixture.app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1, Permission: proto.RemotePermissionControl})
	if err != nil {
		t.Fatal(err)
	}
	fixture.peerHost.tunnel = &fakePeerQuickTunnelManager{status: quicktunnel.Status{
		Running: true, PublicURL: "https://first-route.trycloudflare.com",
	}}
	fixture.app.quickTunnel = fixture.peerHost
	first, err := fixture.app.CreatePeerConnectionBundle(invitations[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	fixture.peerHost.tunnel.(*fakePeerQuickTunnelManager).status.PublicURL = "https://second-route.trycloudflare.com"
	second, err := fixture.app.CreatePeerConnectionBundle(invitations[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	firstBundle, err := peerproto.VerifyConnectionBundle(first, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := peerproto.VerifyConnectionBundle(second, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if firstBundle.Document.BundleID == secondBundle.Document.BundleID {
		t.Fatal("route rotation reused bundle id")
	}
	if firstBundle.Document.Ticket != secondBundle.Document.Ticket || firstBundle.Issuer.Token != secondBundle.Issuer.Token || firstBundle.Genesis.Hash != secondBundle.Genesis.Hash {
		t.Fatal("route rotation changed invitation or Peer Space trust")
	}
	if firstBundle.Document.Routes[0].URL == secondBundle.Document.Routes[0].URL {
		t.Fatal("route rotation did not publish the new URL")
	}
	memberBundle, err := fixture.app.CreatePeerConnectionBundle("")
	if err != nil {
		t.Fatal(err)
	}
	verifiedMemberBundle, err := peerproto.VerifyConnectionBundle(memberBundle, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if verifiedMemberBundle.Document.Ticket != "" || verifiedMemberBundle.Issuer.Token != fixture.hostMembership {
		t.Fatal("member reconnect bundle changed local membership trust")
	}
	if err := fixture.app.RevokePeerInvitation(invitations[0].InviteID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.CreatePeerConnectionBundle(invitations[0].Token); err == nil {
		t.Fatal("revoked invitation received a published route")
	}
}

func TestPeerConnectionBundleCarriesRendezvousAndQuickTunnelHints(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	fixture.peerHost.tunnel = &fakePeerQuickTunnelManager{status: quicktunnel.Status{
		Running: true, PublicURL: "https://dual-route.trycloudflare.com",
	}}
	fixture.app.quickTunnel = fixture.peerHost
	fixture.app.peerRendezvous = &peerRendezvousLifecycle{active: &peerRendezvousHost{
		serviceURL: "https://rendezvous.example", topic: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
	}}

	token, err := fixture.app.CreatePeerConnectionBundle("")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := peerproto.VerifyConnectionBundle(token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Document.Routes) != 2 || verified.Document.Routes[0].Kind != peerproto.RouteQuickTunnel ||
		verified.Document.Routes[1].Kind != peerproto.RouteRendezvous || verified.Document.Routes[1].Topic == "" {
		t.Fatalf("mixed routes=%+v", verified.Document.Routes)
	}

	fixture.peerHost.tunnel = &fakePeerQuickTunnelManager{}
	rendezvousOnly, err := fixture.app.CreatePeerConnectionBundle("")
	if err != nil {
		t.Fatal(err)
	}
	verified, err = peerproto.VerifyConnectionBundle(rendezvousOnly, time.Now())
	if err != nil || len(verified.Document.Routes) != 1 || verified.Document.Routes[0].Kind != peerproto.RouteRendezvous {
		t.Fatalf("Rendezvous-only routes=%+v err=%v", verified.Document.Routes, err)
	}
}

func TestPeerQuickTunnelLifecycleIsExplicitAndRestartable(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	fake := &fakePeerQuickTunnelManager{status: quicktunnel.Status{
		Running: true, PublicURL: "https://lifecycle-route.trycloudflare.com", LocalOrigin: "http://127.0.0.1:1234",
	}}
	fixture.peerHost.tunnel = fake
	fixture.app.quickTunnel = fixture.peerHost
	if got := fixture.app.GetPeerQuickTunnelStatus(); !got.Running || got.PublicURL != fake.status.PublicURL {
		t.Fatalf("status = %+v", got)
	}
	status, err := fixture.app.StartPeerQuickTunnel()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || fake.starts != 1 {
		t.Fatalf("start status=%+v calls=%d", status, fake.starts)
	}
	if err := fixture.app.StopPeerQuickTunnel(); err != nil {
		t.Fatal(err)
	}
	if fake.stops != 1 {
		t.Fatalf("stop calls = %d, want 1", fake.stops)
	}
	if _, err := fixture.app.StartPeerQuickTunnel(); err != nil {
		t.Fatal(err)
	}
	if fake.starts != 2 {
		t.Fatalf("restart calls = %d, want 2", fake.starts)
	}
}

type fakePeerQuickTunnelManager struct {
	status quicktunnel.Status
	starts int
	stops  int
}

func (f *fakePeerQuickTunnelManager) Start(context.Context) (quicktunnel.Status, error) {
	f.starts++
	return f.status, nil
}
func (f *fakePeerQuickTunnelManager) Stop() error {
	f.stops++
	return nil
}
func (f *fakePeerQuickTunnelManager) Status() quicktunnel.Status { return f.status }

func waitForPeerSubscribers(t *testing.T, sess *session.Session, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for sess.SubscriberCount() != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := sess.SubscriberCount(); got != want {
		t.Fatalf("subscriber count = %d, want %d", got, want)
	}
}
