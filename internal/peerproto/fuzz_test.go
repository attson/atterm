package peerproto

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
)

func FuzzVerifyPeerDocuments(f *testing.F) {
	now := time.Unix(1_800_000_000, 0)
	issuer, err := peercrypto.GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	issuerWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		f.Fatal(err)
	}
	genesisToken, membershipToken, err := NewSpace(issuer, issuerWrapping.PublicBytes(), now)
	if err != nil {
		f.Fatal(err)
	}
	genesis, err := VerifyGenesis(genesisToken)
	if err != nil {
		f.Fatal(err)
	}
	membership, err := VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		f.Fatal(err)
	}
	invitations, err := NewInvitationBatch(issuer, genesis, membership, now, InvitationOptions{
		Count: 1, ValidFor: time.Hour, Permission: PermissionControl,
	})
	if err != nil {
		f.Fatal(err)
	}
	joining, err := peercrypto.GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	joiningWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		f.Fatal(err)
	}
	joinToken, err := NewJoinRequest(joining, joiningWrapping.PublicBytes(), invitations[0], now)
	if err != nil {
		f.Fatal(err)
	}
	bundleToken, err := NewConnectionBundle(issuer, genesis, invitations[0], []ConnectionRoute{{
		Kind: RouteQuickTunnel,
		URL:  "https://example.trycloudflare.com",
	}}, now, 10*time.Minute)
	if err != nil {
		f.Fatal(err)
	}

	for _, token := range []string{
		genesisToken,
		membershipToken,
		invitations[0],
		joinToken,
		bundleToken,
		"",
		"apg1.invalid.invalid",
	} {
		f.Add(token)
	}

	f.Fuzz(func(t *testing.T, token string) {
		_, _ = VerifyGenesis(token)
		_, _ = VerifyGrant(token, genesis, now)
		_, _, _ = VerifyInvitation(token, genesis, now)
		_, _ = VerifyJoinRequest(token, genesis, now)
		_, _ = VerifyConnectionBundle(token, now)
	})
}
