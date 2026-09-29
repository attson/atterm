package peerdiscovery

import (
	"reflect"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/rendezvous"
)

func TestDeriveCoordinatesRotatesPresenceWithoutChangingEpochTopic(t *testing.T) {
	key := testEpochKey(t, 7)
	now := time.Unix(1_800_000_000, 0)

	first, err := DeriveCoordinates(key, "space-one", "peer-a", now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := DeriveCoordinates(key, "space-one", "peer-a", now.Add(RotationInterval-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	next, err := DeriveCoordinates(key, "space-one", "peer-a", now.Add(RotationInterval))
	if err != nil {
		t.Fatal(err)
	}
	otherPeer, err := DeriveCoordinates(key, "space-one", "peer-b", now)
	if err != nil {
		t.Fatal(err)
	}
	otherEpoch := testEpochKey(t, 8)
	afterRotation, err := DeriveCoordinates(otherEpoch, "space-one", "peer-a", now)
	if err != nil {
		t.Fatal(err)
	}

	if first != again {
		t.Fatalf("coordinates changed inside one slot: first=%+v again=%+v", first, again)
	}
	if first.Topic != next.Topic || first.PresenceID == next.PresenceID {
		t.Fatalf("slot rotation topic/presence mismatch: first=%+v next=%+v", first, next)
	}
	if first.PresenceID == otherPeer.PresenceID {
		t.Fatal("different peers derived the same presence")
	}
	if first.Topic == afterRotation.Topic || first.PresenceID == afterRotation.PresenceID {
		t.Fatal("epoch rotation did not unlink discovery coordinates")
	}
	if len(first.Topic) != 43 || len(first.PresenceID) != 43 {
		t.Fatalf("coordinates are not canonical base64url SHA-256 values: %+v", first)
	}
}

func TestResolvePresenceAcceptsAdjacentSlotsAndRejectsUnknownPeer(t *testing.T) {
	key := testEpochKey(t, 3)
	now := time.Unix(1_800_100_000, 0).Truncate(RotationInterval)
	previous, err := DeriveCoordinates(key, "space-one", "peer-b", now.Add(-RotationInterval))
	if err != nil {
		t.Fatal(err)
	}

	peerID, ok, err := ResolvePresence(key, "space-one", []string{"peer-a", "peer-b"}, previous.PresenceID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || peerID != "peer-b" {
		t.Fatalf("adjacent-slot presence resolved to %q, %v", peerID, ok)
	}
	if peerID, ok, err = ResolvePresence(key, "space-one", []string{"peer-a"}, previous.PresenceID, now); err != nil || ok || peerID != "" {
		t.Fatalf("unknown peer presence resolved: peer=%q ok=%v err=%v", peerID, ok, err)
	}
}

func TestDirectoryExpiryOnlyChangesReachability(t *testing.T) {
	directory := NewDirectory()
	now := time.Unix(1_800_200_000, 0)
	if err := directory.Observe(Reachability{
		PeerID: "peer-a", PresenceID: "presence-a", Role: rendezvous.RoleMember,
		ObservedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	membership := map[string]bool{"peer-a": true}

	if expired := directory.Expire(now.Add(30 * time.Second)); len(expired) != 0 {
		t.Fatalf("expired active reachability: %v", expired)
	}
	if expired := directory.Expire(now.Add(time.Minute)); !reflect.DeepEqual(expired, []string{"peer-a"}) {
		t.Fatalf("expired peers = %v", expired)
	}
	if got := directory.Snapshot(); len(got) != 0 {
		t.Fatalf("expired reachability remained: %+v", got)
	}
	if !membership["peer-a"] {
		t.Fatal("reachability expiry changed external membership truth")
	}
}

func TestPlanSyncTargetsSelectsEveryReachablePeerInSmallSpace(t *testing.T) {
	candidates := []SyncCandidate{
		{PeerID: "peer-c", PresenceID: "presence-c"},
		{PeerID: "peer-b", PresenceID: "presence-b"},
	}
	got, err := PlanSyncTargets(PlanRequest{
		LocalPeerID: "peer-a", ActiveMemberCount: 3,
		LocalVector: configsync.VersionVector{"peer-a": 4}, Candidates: candidates, Slot: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(peerIDs(got), []string{"peer-b", "peer-c"}) {
		t.Fatalf("small-space targets = %v", peerIDs(got))
	}
}

func TestPlanSyncTargetsBoundsLargeSpaceByLagAndRotatesTies(t *testing.T) {
	candidates := []SyncCandidate{
		{PeerID: "peer-b", PresenceID: "presence-b", Acknowledged: configsync.VersionVector{"actor": 1}},
		{PeerID: "peer-c", PresenceID: "presence-c", Acknowledged: configsync.VersionVector{"actor": 2}},
		{PeerID: "peer-d", PresenceID: "presence-d", Acknowledged: configsync.VersionVector{"actor": 3}},
		{PeerID: "peer-e", PresenceID: "presence-e", Acknowledged: configsync.VersionVector{"actor": 10}},
		{PeerID: "peer-f", PresenceID: "presence-f", Acknowledged: configsync.VersionVector{"actor": 10}},
	}
	request := PlanRequest{
		LocalPeerID: "peer-a", ActiveMemberCount: 20,
		LocalVector: configsync.VersionVector{"actor": 10}, Candidates: candidates,
		MaxFanout: 3, Slot: 20,
	}
	got, err := PlanSyncTargets(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(peerIDs(got), []string{"peer-b", "peer-c", "peer-d"}) {
		t.Fatalf("lag-ranked targets = %v", peerIDs(got))
	}

	equal := make([]SyncCandidate, 0, 8)
	for _, id := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		equal = append(equal, SyncCandidate{PeerID: "peer-" + id, PresenceID: "presence-" + id})
	}
	request.Candidates = equal
	request.MaxFanout = 4
	request.Slot = 100
	first, err := PlanSyncTargets(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Slot++
	second, err := PlanSyncTargets(request)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(peerIDs(first), peerIDs(second)) {
		t.Fatalf("equal-priority fanout did not rotate: %v", peerIDs(first))
	}
}

func testEpochKey(t *testing.T, epoch uint64) configsync.EpochKey {
	t.Helper()
	raw := make([]byte, configsync.EpochKeySize)
	for index := range raw {
		raw[index] = byte(index + int(epoch))
	}
	key, err := configsync.ParseEpochKey(configsync.KeyClassSync, epoch, raw)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func peerIDs(candidates []SyncCandidate) []string {
	out := make([]string, len(candidates))
	for index := range candidates {
		out[index] = candidates[index].PeerID
	}
	return out
}
