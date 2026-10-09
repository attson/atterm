package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
)

// This test uses two independent Peer identities over a real TCP listener. It
// keeps every public reachability mechanism disabled so regressions cannot be
// hidden by Rendezvous, STUN, Quick Tunnel, or Relay fallback.
func TestPeerLANOnlyInvitationCatalogAndTerminalAttach(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) {
		testPeerLANOnlyInvitationCatalogAndTerminalAttach(t, "tcp4", "127.0.0.1")
	})
	t.Run("IPv6", func(t *testing.T) {
		host, ok := peerLANReachableIPv6TestHost()
		if !ok {
			t.Skip("no locally reachable IPv6 address is available")
		}
		testPeerLANOnlyInvitationCatalogAndTerminalAttach(t, "tcp6", host)
	})
}

func testPeerLANOnlyInvitationCatalogAndTerminalAttach(t *testing.T, network, advertiseHost string) {
	port, ok := freeTCPPortForNetwork(t, network)
	if !ok {
		t.Skipf("%s listener is unavailable", network)
	}
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	fixture.app.ctx = context.Background()
	fixture.app.mu.Lock()
	fixture.app.quickTunnel = fixture.peerHost
	fixture.app.mu.Unlock()

	cfg := fixture.app.cfgStore.Get()
	cfg.RelayURL = ""
	cfg.PeerRendezvousMode = "disabled"
	cfg.PeerSTUNMode = "disabled"
	if err := fixture.app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if err := fixture.app.SetPeerLANConfig(SetPeerLANConfigReq{
		Enabled: true, AdvertiseHost: advertiseHost, Port: port,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.app.stopPeerLAN)
	if status := fixture.app.GetPeerQuickTunnelStatus(); status.Running || status.Starting || status.PublicURL != "" {
		t.Fatalf("Quick Tunnel unexpectedly active: %+v", status)
	}
	if fixture.app.peerRendezvous != nil {
		t.Fatal("Rendezvous lifecycle unexpectedly active")
	}

	invitations, err := fixture.app.CreatePeerInvitations(CreatePeerInvitationsReq{
		Count: 1, Permission: string(peerproto.PermissionControl),
		AllowedSessionIDs: []string{fixture.session.ID.String()}, CanSyncSecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bundleToken, err := fixture.app.CreatePeerConnectionBundle(invitations[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := peerproto.VerifyConnectionBundle(bundleToken, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Document.Routes) != 1 || bundle.Document.Routes[0].Kind != peerproto.RouteManualLAN {
		t.Fatalf("LAN-only bundle routes=%+v", bundle.Document.Routes)
	}

	guestIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	guestWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bootstrap, err := quicktunnel.Join(ctx, quicktunnel.JoinClientConfig{
		BundleToken: bundleToken, Identity: guestIdentity, WrappingPublicKey: guestWrapping.PublicBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(bootstrap.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	guestMembership, err := peerproto.VerifyGrant(bootstrap.MembershipToken, genesis, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if guestMembership.Document.SubjectPeerID != guestIdentity.PeerID() {
		t.Fatalf("joined membership peer=%q want=%q", guestMembership.Document.SubjectPeerID, guestIdentity.PeerID())
	}

	routeURL := bundle.Document.Routes[0].URL
	controlRecords := make(chan peerConfigTestRecord, 8)
	controlSignal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: routeURL, AllowInsecure: true, Identity: guestIdentity,
		GenesisToken: bootstrap.GenesisToken, ClientMembershipToken: bootstrap.MembershipToken,
		HostMembershipToken: fixture.hostMembership, SessionID: peertransport.ConfigSyncSessionID(),
		ClientInstanceID: "lan-only-control",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer controlSignal.Close()
	if err := controlSignal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnAuthenticated: func(channel *quicktunnel.WSSChannel) {
			payload, _ := json.Marshal(peerLANCatalogRequest{V: peerLANCatalogVersion})
			if sendErr := channel.SendConfigMessage(ctx, peertransport.RecordCatalogRequest, payload); sendErr != nil {
				controlRecords <- peerConfigTestRecord{payload: []byte(sendErr.Error())}
			}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			controlRecords <- peerConfigTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	control, err := controlSignal.StartWSSFallback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gotCatalog, gotInventory, gotBatch bool
	for !gotCatalog || !gotInventory || !gotBatch {
		select {
		case record := <-controlRecords:
			switch record.kind {
			case peertransport.RecordCatalogResponse:
				var catalog peerLANCatalogResponse
				if err := json.Unmarshal(record.payload, &catalog); err != nil {
					t.Fatal(err)
				}
				if len(catalog.Sessions) != 1 || catalog.Sessions[0].ID != fixture.session.ID.String() {
					t.Fatalf("LAN-only catalog=%+v", catalog.Sessions)
				}
				gotCatalog = true
			case peertransport.RecordConfigInventory:
				gotInventory = true
				if err := control.SendConfigMessage(ctx, peertransport.RecordConfigInventory, record.payload); err != nil {
					t.Fatal(err)
				}
			case peertransport.RecordConfigBatch:
				gotBatch = true
			default:
				if len(record.payload) != 0 {
					t.Fatalf("LAN control setup failed: %s", record.payload)
				}
			}
		case <-ctx.Done():
			t.Fatal("LAN-only catalog/config exchange timed out")
		}
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("control route created %d terminal subscribers", got)
	}
	_ = controlSignal.Close()

	terminalRecords := make(chan directClientRecord, 16)
	terminalSignal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: routeURL, AllowInsecure: true, Identity: guestIdentity,
		GenesisToken: bootstrap.GenesisToken, ClientMembershipToken: bootstrap.MembershipToken,
		HostMembershipToken: fixture.hostMembership, SessionID: fixture.session.ID,
		ClientInstanceID: "lan-only-terminal",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terminalSignal.Close()
	if err := terminalSignal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			terminalRecords <- directClientRecord{kind: kind, payload: append([]byte(nil), payload...)}
		},
		OnConfigMessage: func(peertransport.RecordKind, []byte) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	terminal, err := terminalSignal.StartWSSFallback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gotOutput, gotReady bool
	for !gotOutput || !gotReady {
		select {
		case record := <-terminalRecords:
			switch record.kind {
			case peertransport.RecordDirectReady:
				gotReady = len(record.payload) == 8 && binary.BigEndian.Uint64(record.payload) == 1
			case peertransport.RecordFrame:
				frame, err := proto.Unmarshal(record.payload)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Type == proto.TypeOut {
					seq, output, err := proto.DecodeOut(frame.Payload)
					if err != nil {
						t.Fatal(err)
					}
					gotOutput = seq == 1 && bytes.Equal(output, []byte("peer output"))
				}
			}
		case <-ctx.Done():
			t.Fatal("LAN-only terminal replay timed out")
		}
	}
	if got := fixture.session.SubscriberCount(); got != 1 {
		t.Fatalf("LAN terminal subscribers=%d want=1", got)
	}
	claim, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "lan-only-terminal", ClientName: "LAN-only test"})
	if err := terminal.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: fixture.session.ID, Payload: claim,
	})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fixture.session.DriverClientID() != "lan-only-terminal" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fixture.session.DriverClientID() != "lan-only-terminal" {
		t.Fatal("LAN-only client did not become driver")
	}
	if err := terminal.SendFrame(ctx, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: fixture.session.ID, Payload: []byte("LAN-only input"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-fixture.session.Inbound():
		if frame.Type != proto.TypeIn || !bytes.Equal(frame.Payload, []byte("LAN-only input")) {
			t.Fatalf("LAN-only inbound=%+v", frame)
		}
	case <-ctx.Done():
		t.Fatal("LAN-only terminal input timed out")
	}
	if err := terminalSignal.Close(); err != nil {
		t.Fatal(err)
	}
	waitForPeerSubscribers(t, fixture.session, 0)
}
