package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

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
