package main

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/rendezvous"
)

func TestPeerRendezvousDiscoveryUsesActiveMembershipAndDurableSyncProgress(t *testing.T) {
	app, now := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	creatorMembership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}

	remoteIDs := make([]string, 0, 2)
	for range 2 {
		remote, err := peercrypto.GenerateIdentity()
		if err != nil {
			t.Fatal(err)
		}
		wrapping, err := peercrypto.GenerateWrappingIdentity()
		if err != nil {
			t.Fatal(err)
		}
		membership := issueTestPeerMembership(t, creator, genesis, creatorMembership, remote, wrapping, now)
		if _, err := app.peerSpace.store.ApplyMemberships([]string{membership.Token}, now); err != nil {
			t.Fatal(err)
		}
		remoteIDs = append(remoteIDs, remote.PeerID())
	}

	localCoordinates, err := app.peerSpace.rendezvousCoordinates(now)
	if err != nil {
		t.Fatal(err)
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	observations := make([]peerdiscovery.Reachability, 0, 3)
	for _, peerID := range remoteIDs {
		coordinates, err := peerdiscovery.DeriveCoordinates(key, genesis.Document.SpaceID, peerID, now)
		if err != nil {
			t.Fatal(err)
		}
		resolved, ok, err := app.peerSpace.resolveRendezvousPresence(coordinates.PresenceID, now)
		if err != nil || !ok || resolved != peerID {
			t.Fatalf("presence resolution peer=%q ok=%v err=%v", resolved, ok, err)
		}
		observations = append(observations, peerdiscovery.Reachability{
			PeerID: peerID, PresenceID: coordinates.PresenceID, Role: rendezvous.RoleMember,
			ObservedAt: now, ExpiresAt: now.Add(peerdiscovery.RotationInterval),
		})
	}
	observations = append(observations, peerdiscovery.Reachability{
		PeerID: "revoked-or-unknown", PresenceID: "untrusted-route", Role: rendezvous.RoleMember,
		ObservedAt: now, ExpiresAt: now.Add(peerdiscovery.RotationInterval),
	})

	runtime := app.peerSpace.configReplica
	exchangedAt := now.Add(-time.Second)
	if err := app.peerSpace.store.RecordConfigExchange(remoteIDs[0], runtime.replica.Vector(), exchangedAt); err != nil {
		t.Fatal(err)
	}
	targets, err := app.peerSpace.planRendezvousSyncTargets(observations, now)
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]string, len(targets))
	for index := range targets {
		gotIDs[index] = targets[index].PeerID
	}
	wantIDs := append([]string(nil), remoteIDs...)
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("sync targets=%v want=%v", gotIDs, wantIDs)
	}
	var recorded peerdiscovery.SyncCandidate
	for _, target := range targets {
		if target.PeerID == remoteIDs[0] {
			recorded = target
		}
	}
	if !reflect.DeepEqual(recorded.Acknowledged, runtime.replica.Vector()) || recorded.LastExchangeAt != exchangedAt.Unix() {
		t.Fatalf("durable progress was not attached: %+v", recorded)
	}
	if localCoordinates.Topic == "" || localCoordinates.PresenceID == "" || localCoordinates.Slot == 0 {
		t.Fatalf("invalid local coordinates: %+v", localCoordinates)
	}
}

func TestPeerRendezvousPresenceCannotResolveAfterEpochRotation(t *testing.T) {
	app, now := newTestPeerApp(t)
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := app.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	creatorMembership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	membership := issueTestPeerMembership(t, creator, genesis, creatorMembership, remote, wrapping, now)
	if _, err := app.peerSpace.store.ApplyMemberships([]string{membership.Token}, now); err != nil {
		t.Fatal(err)
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	oldCoordinates, err := peerdiscovery.DeriveCoordinates(key, genesis.Document.SpaceID, remote.PeerID(), now)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.RevokePeerMember(remote.PeerID()); err != nil {
		t.Fatal(err)
	}
	if peerID, ok, err := app.peerSpace.resolveRendezvousPresence(oldCoordinates.PresenceID, now); err != nil || ok || peerID != "" {
		t.Fatalf("old presence survived revocation: peer=%q ok=%v err=%v", peerID, ok, err)
	}
}
