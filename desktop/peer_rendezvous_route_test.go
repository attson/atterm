package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
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

func mustMembership(t *testing.T, token string, genesis peerproto.VerifiedGenesis) peerproto.VerifiedGrant {
	t.Helper()
	membership, err := peerproto.VerifyGrant(token, genesis, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return membership
}
