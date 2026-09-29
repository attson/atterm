package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

func TestActivePeerMembershipsChooseCanonicalAndApplyDenyWins(t *testing.T) {
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
	sibling, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	siblingWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	first := issueTestPeerMembership(t, creator, genesis, creatorMembership, sibling, siblingWrapping, now)
	renewed := issueTestPeerMembership(t, creator, genesis, creatorMembership, sibling, siblingWrapping, now.Add(time.Minute))

	state.Memberships = []string{state.LocalMembership, first.Token, renewed.Token}
	active, err := activePeerMemberships(state, genesis, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !membershipIsActive(renewed, active) || membershipIsActive(first, active) {
		t.Fatalf("canonical membership view = %+v", active)
	}

	revocation, err := peerproto.NewRevocation(
		creator, genesis, creatorMembership, peerproto.RevocationGrant,
		renewed.Document.Serial, now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	state.Revocations = []string{revocation.Token}
	active, err = activePeerMemberships(state, genesis, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if membershipForPeer(active, sibling.PeerID()) != nil {
		t.Fatal("revoking the canonical grant fell back to an older grant")
	}
}

func TestActivePeerMembershipsExcludeExpiredDirectoryEntries(t *testing.T) {
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
	sibling, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	siblingWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	membership := issueTestPeerMembership(t, creator, genesis, creatorMembership, sibling, siblingWrapping, now)
	doc := membership.Document
	doc.ExpiresAt = now.Add(time.Hour).Unix()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := creator.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	expired := "apm1." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature)
	if _, err := peerproto.VerifyGrantAtIssuance(expired, genesis); err != nil {
		t.Fatal(err)
	}
	state.Memberships = []string{state.LocalMembership, expired}
	active, err := activePeerMemberships(state, genesis, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if membershipForPeer(active, sibling.PeerID()) != nil {
		t.Fatal("expired membership entered the active recipient view")
	}
}

func issueTestPeerMembership(
	t *testing.T,
	issuer *peercrypto.Identity,
	genesis peerproto.VerifiedGenesis,
	issuerMembership peerproto.VerifiedGrant,
	subject *peercrypto.Identity,
	wrapping *peercrypto.WrappingIdentity,
	now time.Time,
) peerproto.VerifiedGrant {
	t.Helper()
	tickets, err := peerproto.NewInvitationBatch(issuer, genesis, issuerMembership, now, peerproto.InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	joinToken, err := peerproto.NewJoinRequest(subject, wrapping.PublicBytes(), tickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	join, err := peerproto.VerifyJoinRequest(joinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := peerproto.IssueMembership(issuer, genesis, issuerMembership, join, now)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(token, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	return membership
}

func membershipForPeer(memberships []peerproto.VerifiedGrant, peerID string) *peerproto.VerifiedGrant {
	for index := range memberships {
		if memberships[index].Document.SubjectPeerID == peerID {
			return &memberships[index]
		}
	}
	return nil
}

func TestPeerMemberDirectoryAndRevocationRotateEpochs(t *testing.T) {
	app, now := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(state.GenesisToken)
	creator, _ := app.peerSpace.loadIdentity()
	creatorMembership, _ := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	remote, _ := peercrypto.GenerateIdentity()
	remoteWrapping, _ := peercrypto.GenerateWrappingIdentity()
	remoteMembership := issueTestPeerMembership(t, creator, genesis, creatorMembership, remote, remoteWrapping, now)
	if _, err := app.peerSpace.store.ApplyMemberships([]string{remoteMembership.Token}, now); err != nil {
		t.Fatal(err)
	}

	members, err := app.ListPeerMembers()
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || !members[0].Local || members[0].PeerID != status.PeerID || members[0].CanRevoke {
		t.Fatalf("member directory=%+v", members)
	}
	if members[1].PeerID != remote.PeerID() || members[1].Status != "active" || !members[1].CanRevoke || members[1].GrantSerial == "" {
		t.Fatalf("remote directory entry=%+v", members[1])
	}
	publicJSON, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretPrefix := range []string{"apm1.", "arv1.", "akr1."} {
		if strings.Contains(string(publicJSON), secretPrefix) {
			t.Fatalf("member directory exposed %s token", secretPrefix)
		}
	}
	if err := app.RevokePeerMember(status.PeerID); !errors.Is(err, errCannotRevokeLocalPeer) {
		t.Fatalf("local revoke error=%v", err)
	}

	before, _ := currentEpochRotations(state.EpochRotations, genesis)
	if err := app.RevokePeerMember(remote.PeerID()); err != nil {
		t.Fatal(err)
	}
	afterState, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	after, err := currentEpochRotations(afterState.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if after[class].Document.Epoch != before[class].Document.Epoch+1 {
			t.Fatalf("%s epoch=%d want=%d", class, after[class].Document.Epoch, before[class].Document.Epoch+1)
		}
		if len(after[class].Document.Recipients) != 1 || after[class].Document.Recipients[0].PeerID != status.PeerID {
			t.Fatalf("%s recipients=%+v", class, after[class].Document.Recipients)
		}
		key, err := loadPeerEpochKey(status.SpaceID, class)
		if err != nil {
			t.Fatal(err)
		}
		if key.Epoch != after[class].Document.Epoch || bytes.Equal(key.Bytes(), make([]byte, configsync.EpochKeySize)) {
			t.Fatalf("saved %s key epoch=%d", class, key.Epoch)
		}
	}
	members, err = app.ListPeerMembers()
	if err != nil {
		t.Fatal(err)
	}
	if members[1].Status != "revoked" || members[1].RevokedAt == 0 || members[1].CanRevoke {
		t.Fatalf("revoked directory entry=%+v", members[1])
	}
	if err := app.RevokePeerMember(remote.PeerID()); err != nil {
		t.Fatal(err)
	}
	idempotent, _ := app.peerSpace.store.Load()
	if len(idempotent.Revocations) != 1 || len(idempotent.EpochRotations) != len(afterState.EpochRotations) {
		t.Fatalf("idempotent revoke changed governance state")
	}
}
