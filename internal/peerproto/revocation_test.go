package peerproto

import (
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
)

func issueTestMember(t *testing.T, creator *peercrypto.Identity, genesis VerifiedGenesis, creatorMembership VerifiedGrant, now time.Time, permission Permission, canInvite bool) (*peercrypto.Identity, VerifiedGrant) {
	t.Helper()
	tickets, err := NewInvitationBatch(creator, genesis, creatorMembership, now, InvitationOptions{
		Count: 1, Permission: permission, CanInvite: canInvite,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	requestToken, err := NewJoinRequest(identity, newTestWrappingIdentity(t).PublicBytes(), tickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	request, err := VerifyJoinRequest(requestToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	membershipToken, err := IssueMembership(creator, genesis, creatorMembership, request, now)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	return identity, membership
}

func TestRevocationAuthorityAndScopes(t *testing.T) {
	creator, genesis, creatorMembership, now := newTestSpace(t)
	memberIdentity, member := issueTestMember(t, creator, genesis, creatorMembership, now, PermissionControl, true)
	targetIdentity, target := issueTestMember(t, creator, genesis, creatorMembership, now, PermissionControl, false)

	if _, err := NewRevocation(memberIdentity, genesis, member, RevocationMember, targetIdentity.PeerID(), now); !errors.Is(err, ErrRevocationDenied) {
		t.Fatalf("non-admin member revocation error=%v", err)
	}
	batchID := "4b4d16f5-cadc-488b-94f4-11470d09e69d"
	batchRevocation, err := NewRevocation(memberIdentity, genesis, member, RevocationInvitationBatch, batchID, now)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewRevocationSet(genesis)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Apply(batchRevocation.Token); err != nil {
		t.Fatal(err)
	}
	if _, ok := set.InvitationBatchRevoked(memberIdentity.PeerID(), batchID); !ok {
		t.Fatal("issuer-scoped batch was not revoked")
	}
	if _, ok := set.InvitationBatchRevoked(creator.PeerID(), batchID); ok {
		t.Fatal("batch revocation escaped issuer scope")
	}

	memberRevocation, err := NewRevocation(creator, genesis, creatorMembership, RevocationMember, targetIdentity.PeerID(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	grantRevocation, err := NewRevocation(creator, genesis, creatorMembership, RevocationGrant, member.Document.Serial, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{memberRevocation.Token, grantRevocation.Token} {
		if _, err := set.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	active, err := set.FilterActiveMemberships([]VerifiedGrant{target, creatorMembership, member}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Document.SubjectPeerID != creator.PeerID() {
		t.Fatalf("active memberships=%+v", active)
	}
}

func TestRevocationSetConvergesAndDenyWins(t *testing.T) {
	creator, genesis, creatorMembership, now := newTestSpace(t)
	target, _, _, _ := newTestSpace(t)
	first, err := NewRevocation(creator, genesis, creatorMembership, RevocationMember, target.PeerID(), now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRevocation(creator, genesis, creatorMembership, RevocationMember, target.PeerID(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]*RevocationSet, 2)
	for index := range sets {
		sets[index], err = NewRevocationSet(genesis)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, err := sets[0].Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	for _, token := range []string{second.Token, first.Token} {
		if _, err := sets[1].Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(sets[0].Tokens(), sets[1].Tokens()) {
		t.Fatal("arrival order changed anti-entropy token order")
	}
	if revokedAt, ok := sets[0].MemberRevoked(target.PeerID()); !ok || revokedAt != second.Document.CreatedAt {
		t.Fatalf("revokedAt=%d ok=%v", revokedAt, ok)
	}
	duplicate, err := sets[0].Apply(first.Token)
	if err != nil || !duplicate.Duplicate || duplicate.Stored {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
}

func TestRevocationRejectsIDForkAndMutableVerifiedInput(t *testing.T) {
	creator, genesis, creatorMembership, now := newTestSpace(t)
	firstTarget, _, _, _ := newTestSpace(t)
	secondTarget, _, _, _ := newTestSpace(t)
	first, err := NewRevocation(creator, genesis, creatorMembership, RevocationMember, firstTarget.PeerID(), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRevocation(creator, genesis, creatorMembership, RevocationMember, secondTarget.PeerID(), now)
	if err != nil {
		t.Fatal(err)
	}
	forkDoc := second.Document
	forkDoc.RevocationID = first.Document.RevocationID
	forkToken, err := signDocument(revocationPrefix, forkDoc, creator)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewRevocationSet(genesis)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Apply(first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Apply(forkToken); !errors.Is(err, ErrRevocationFork) {
		t.Fatalf("fork error=%v", err)
	}

	forged := first
	forged.Document.TargetID = secondTarget.PeerID()
	verified, err := VerifyRevocation(forged.Token, genesis)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Document.TargetID != firstTarget.PeerID() {
		t.Fatal("mutable verified document bypassed token verification")
	}

	tokens := set.Tokens()
	sort.Strings(tokens)
	if len(tokens) != 1 || tokens[0] != first.Token {
		t.Fatalf("unexpected retained tokens=%v", tokens)
	}
}
