package configsync

import (
	"bytes"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

type rotationFixture struct {
	now               time.Time
	creator           *peercrypto.Identity
	creatorWrapping   *peercrypto.WrappingIdentity
	genesis           peerproto.VerifiedGenesis
	creatorMembership peerproto.VerifiedGrant
	members           []peerproto.VerifiedGrant
	wrapping          map[string]*peercrypto.WrappingIdentity
}

func newRotationFixture(t *testing.T) *rotationFixture {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	creator := testIdentity(t)
	creatorWrapping := testWrappingIdentity(t)
	genesisToken, membershipToken, err := peerproto.NewSpace(creator, creatorWrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	return &rotationFixture{
		now: now, creator: creator, creatorWrapping: creatorWrapping,
		genesis: genesis, creatorMembership: membership,
		members:  []peerproto.VerifiedGrant{membership},
		wrapping: map[string]*peercrypto.WrappingIdentity{creator.PeerID(): creatorWrapping},
	}
}

func (f *rotationFixture) addMember(t *testing.T, permission peerproto.Permission, canInvite, canSyncSecrets bool) (*peercrypto.Identity, peerproto.VerifiedGrant) {
	t.Helper()
	tickets, err := peerproto.NewInvitationBatch(f.creator, f.genesis, f.creatorMembership, f.now, peerproto.InvitationOptions{
		Count: 1, Permission: permission, CanInvite: canInvite, CanSyncSecrets: canSyncSecrets,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity(t)
	wrapping := testWrappingIdentity(t)
	requestToken, err := peerproto.NewJoinRequest(identity, wrapping.PublicBytes(), tickets[0], f.now)
	if err != nil {
		t.Fatal(err)
	}
	request, err := peerproto.VerifyJoinRequest(requestToken, f.genesis, f.now)
	if err != nil {
		t.Fatal(err)
	}
	membershipToken, err := peerproto.IssueMembership(f.creator, f.genesis, f.creatorMembership, request, f.now)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(membershipToken, f.genesis, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.members = append(f.members, membership)
	f.wrapping[identity.PeerID()] = wrapping
	return identity, membership
}

func testWrappingIdentity(t *testing.T) *peercrypto.WrappingIdentity {
	t.Helper()
	identity, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestEpochRotationBindsMembershipsCapabilitiesAndCommitment(t *testing.T) {
	fixture := newRotationFixture(t)
	secretIdentity, _ := fixture.addMember(t, peerproto.PermissionControl, false, true)
	plainIdentity, _ := fixture.addMember(t, peerproto.PermissionControl, false, false)

	syncKey, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	syncRotation, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, nil,
		syncKey, fixture.members, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncRotation.Document.Recipients) != 3 {
		t.Fatalf("sync recipients=%d want=3", len(syncRotation.Document.Recipients))
	}
	if err := ValidateEpochKeyForRotation(syncKey, syncRotation); err != nil {
		t.Fatalf("validate committed sync key: %v", err)
	}
	wrongKey, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEpochKeyForRotation(wrongKey, syncRotation); !errors.Is(err, ErrInvalidEpochKey) {
		t.Fatalf("wrong committed key error=%v", err)
	}
	for _, member := range fixture.members {
		peerID := member.Document.SubjectPeerID
		opened, err := OpenRotationEpochKey(syncRotation, fixture.genesis, peerID, fixture.wrapping[peerID])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(opened.Bytes(), syncKey.Bytes()) {
			t.Fatalf("sync key differs for peer %s", peerID)
		}
	}

	vaultKey, err := GenerateEpochKey(KeyClassVault, 1)
	if err != nil {
		t.Fatal(err)
	}
	vaultRotation, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, nil,
		vaultKey, fixture.members, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(vaultRotation.Document.Recipients) != 2 {
		t.Fatalf("vault recipients=%d want=2", len(vaultRotation.Document.Recipients))
	}
	if _, err := OpenRotationEpochKey(vaultRotation, fixture.genesis, plainIdentity.PeerID(), fixture.wrapping[plainIdentity.PeerID()]); !errors.Is(err, ErrEpochRotationDenied) {
		t.Fatalf("plain member opened vault rotation: %v", err)
	}
	if _, err := OpenRotationEpochKey(vaultRotation, fixture.genesis, secretIdentity.PeerID(), fixture.wrapping[secretIdentity.PeerID()]); err != nil {
		t.Fatal(err)
	}
}

func TestEpochRotationRequiresActiveAdminAndExactRecipients(t *testing.T) {
	fixture := newRotationFixture(t)
	memberIdentity, memberGrant := fixture.addMember(t, peerproto.PermissionControl, true, false)
	key, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEpochRotation(memberIdentity, fixture.genesis, memberGrant, nil, key, fixture.members, fixture.now); !errors.Is(err, ErrEpochRotationDenied) {
		t.Fatalf("non-admin rotation error=%v", err)
	}

	rotation, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, nil,
		key, fixture.members, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeEpochRotation(rotation, fixture.genesis, fixture.members, fixture.now); err != nil {
		t.Fatal(err)
	}
	remaining := []peerproto.VerifiedGrant{fixture.creatorMembership}
	if err := AuthorizeEpochRotation(rotation, fixture.genesis, remaining, fixture.now); !errors.Is(err, ErrEpochRotationDenied) {
		t.Fatalf("stale recipient set error=%v", err)
	}
	futureKey, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	future, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, nil,
		futureKey, fixture.members, fixture.now.Add(10*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeEpochRotation(future, fixture.genesis, fixture.members, fixture.now); !errors.Is(err, ErrInvalidEpochRotation) {
		t.Fatalf("future rotation error=%v", err)
	}

	rotatedKey, err := GenerateEpochKey(KeyClassSync, 2)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, &rotation,
		rotatedKey, remaining, fixture.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated.Document.Recipients) != 1 || rotated.Document.Recipients[0].PeerID != fixture.creator.PeerID() {
		t.Fatalf("revoked member retained in rotation: %+v", rotated.Document.Recipients)
	}
}

func TestEpochRotationResolverConvergesConcurrentBranchesAndRebases(t *testing.T) {
	fixture := newRotationFixture(t)
	authorize := func(rotation VerifiedEpochRotation) error {
		return AuthorizeEpochRotation(rotation, fixture.genesis, fixture.members, fixture.now.Add(time.Hour))
	}
	resolver, err := NewEpochRotationResolver(fixture.genesis, KeyClassSync, authorize)
	if err != nil {
		t.Fatal(err)
	}
	otherResolver, err := NewEpochRotationResolver(fixture.genesis, KeyClassSync, authorize)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, _ := GenerateEpochKey(KeyClassSync, 1)
	secondKey, _ := GenerateEpochKey(KeyClassSync, 1)
	first, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, firstKey, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, secondKey, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []VerifiedEpochRotation{first, second}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Hash < candidates[j].Hash })
	winner, loser := candidates[0], candidates[1]

	if _, err := resolver.Apply(loser.Token); err != nil {
		t.Fatal(err)
	}
	loserChildKey, _ := GenerateEpochKey(KeyClassSync, 2)
	loserChild, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, &loser, loserChildKey, fixture.members, fixture.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Apply(loserChild.Token); err != nil {
		t.Fatal(err)
	}
	if current, ok := resolver.Current(); !ok || current.Hash != loserChild.Hash {
		t.Fatalf("pre-merge current=%+v ok=%v", current.Document, ok)
	}
	result, err := resolver.Apply(winner.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !result.CanonicalChanged || result.Current == nil || result.Current.Hash != winner.Hash || result.Current.Document.Epoch != 1 {
		t.Fatalf("concurrent winner did not replace losing branch: %+v", result)
	}

	winnerChildKey, _ := GenerateEpochKey(KeyClassSync, 2)
	winnerChild, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, &winner, winnerChildKey, fixture.members, fixture.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Apply(winnerChild.Token); err != nil {
		t.Fatal(err)
	}
	if current, ok := resolver.Current(); !ok || current.Hash != winnerChild.Hash {
		t.Fatalf("rebased current=%+v ok=%v", current.Document, ok)
	}
	for _, token := range []string{winnerChild.Token, winner.Token, loserChild.Token, loser.Token} {
		if _, err := otherResolver.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	otherCurrent, ok := otherResolver.Current()
	if !ok || otherCurrent.Hash != winnerChild.Hash {
		t.Fatalf("opposite arrival order diverged: %+v", otherCurrent.Document)
	}
	if got := resolver.Tokens(); len(got) != 4 {
		t.Fatalf("retained candidates=%d want=4", len(got))
	}
	if left, right := resolver.Tokens(), otherResolver.Tokens(); !slices.Equal(left, right) {
		t.Fatalf("token order diverged:\nleft=%v\nright=%v", left, right)
	}
	duplicate, err := resolver.Apply(loser.Token)
	if err != nil || !duplicate.Duplicate || duplicate.Current == nil || duplicate.Current.Hash != winnerChild.Hash {
		t.Fatalf("duplicate result=%+v err=%v", duplicate, err)
	}
}

func TestEpochRotationRejectsForkedIDAndTampering(t *testing.T) {
	fixture := newRotationFixture(t)
	authorize := func(rotation VerifiedEpochRotation) error {
		return AuthorizeEpochRotation(rotation, fixture.genesis, fixture.members, fixture.now.Add(time.Hour))
	}
	resolver, err := NewEpochRotationResolver(fixture.genesis, KeyClassSync, authorize)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, _ := GenerateEpochKey(KeyClassSync, 1)
	first, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, firstKey, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Apply(first.Token); err != nil {
		t.Fatal(err)
	}

	otherKey, _ := GenerateEpochKey(KeyClassSync, 1)
	other, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, otherKey, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	forkDoc := cloneEpochRotation(other.Document)
	forkDoc.RotationID = first.Document.RotationID
	forkToken, err := signEpochRotation(forkDoc, fixture.creator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Apply(forkToken); !errors.Is(err, ErrEpochRotationFork) {
		t.Fatalf("forked rotation id error=%v", err)
	}

	parts := strings.Split(first.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("rotation token parts=%d", len(parts))
	}
	last := parts[2][len(parts[2])-1]
	if last == 'A' {
		last = 'B'
	} else {
		last = 'A'
	}
	tamperedSignature := parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-1] + string(last)
	if _, err := VerifyEpochRotation(tamperedSignature, fixture.genesis); !errors.Is(err, ErrInvalidEpochRotation) {
		t.Fatalf("tampered signature error=%v", err)
	}

	tamperedCommitment := cloneEpochRotation(first.Document)
	tamperedCommitment.KeyCommitment = encode(make([]byte, 32))
	tamperedToken, err := signEpochRotation(tamperedCommitment, fixture.creator)
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := VerifyEpochRotation(tamperedToken, fixture.genesis)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRotationEpochKey(tampered, fixture.genesis, fixture.creator.PeerID(), fixture.creatorWrapping); !errors.Is(err, ErrInvalidEpochRotation) {
		t.Fatalf("tampered commitment error=%v", err)
	}
}

func TestEpochRotationPublicAPIsReverifyToken(t *testing.T) {
	fixture := newRotationFixture(t)
	key, _ := GenerateEpochKey(KeyClassSync, 1)
	rotation, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, key, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	forged := cloneVerifiedEpochRotation(rotation)
	forged.Document.Recipients = nil
	forged.Document.ActorPeerID = strings.Repeat("0", len(forged.Document.ActorPeerID))
	opened, err := OpenRotationEpochKey(forged, fixture.genesis, fixture.creator.PeerID(), fixture.creatorWrapping)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened.Bytes(), key.Bytes()) {
		t.Fatal("forged in-memory document changed opened key")
	}
	if err := AuthorizeEpochRotation(forged, fixture.genesis, fixture.members, fixture.now); err != nil {
		t.Fatal(err)
	}
}

func TestEpochRotationResolverRetainsOutOfOrderCandidate(t *testing.T) {
	fixture := newRotationFixture(t)
	authorize := func(rotation VerifiedEpochRotation) error {
		return AuthorizeEpochRotation(rotation, fixture.genesis, fixture.members, fixture.now.Add(time.Hour))
	}
	resolver, err := NewEpochRotationResolver(fixture.genesis, KeyClassSync, authorize)
	if err != nil {
		t.Fatal(err)
	}
	rootKey, _ := GenerateEpochKey(KeyClassSync, 1)
	root, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, nil, rootKey, fixture.members, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	childKey, _ := GenerateEpochKey(KeyClassSync, 2)
	child, err := NewEpochRotation(fixture.creator, fixture.genesis, fixture.creatorMembership, &root, childKey, fixture.members, fixture.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Apply(child.Token)
	if err != nil {
		t.Fatal(err)
	}
	if result.Current != nil {
		t.Fatalf("orphan became current: %+v", result.Current.Document)
	}
	result, err = resolver.Apply(root.Token)
	if err != nil {
		t.Fatal(err)
	}
	if result.Current == nil || result.Current.Hash != child.Hash {
		t.Fatalf("out-of-order child not connected: %+v", result.Current)
	}
}
