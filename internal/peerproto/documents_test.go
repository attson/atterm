package peerproto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
)

func newTestSpace(t *testing.T) (*peercrypto.Identity, VerifiedGenesis, VerifiedGrant, time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	id, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping := newTestWrappingIdentity(t)
	genesisToken, membershipToken, err := NewSpace(id, wrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := VerifyGenesis(genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(membership.WrappingPublicKey, wrapping.PublicBytes()) {
		t.Fatal("creator membership does not contain its wrapping public key")
	}
	return id, genesis, membership, now
}

func newTestWrappingIdentity(t *testing.T) *peercrypto.WrappingIdentity {
	t.Helper()
	identity, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestNewSpaceAndInvitationBatch(t *testing.T) {
	id, genesis, membership, now := newTestSpace(t)
	tokens, err := NewInvitationBatch(id, genesis, membership, now, InvitationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 5 {
		t.Fatalf("tokens = %d, want 5", len(tokens))
	}
	batch := ""
	seen := map[string]bool{}
	for _, token := range tokens {
		if !strings.HasPrefix(token, "atp1.") {
			t.Fatalf("unexpected token prefix: %q", token[:8])
		}
		doc, issuer, err := VerifyInvitation(token, genesis, now)
		if err != nil {
			t.Fatal(err)
		}
		if issuer.Document.SubjectPeerID != id.PeerID() || doc.RedemptionPeerID != id.PeerID() {
			t.Fatal("invitation is not bound to issuer redemption authority")
		}
		if batch == "" {
			batch = doc.BatchID
		} else if doc.BatchID != batch {
			t.Fatal("one batch produced multiple batch ids")
		}
		if seen[doc.InviteID] {
			t.Fatal("duplicate invite id")
		}
		seen[doc.InviteID] = true
	}
}

func TestInvitationRejectsMutationAndExpiry(t *testing.T) {
	id, genesis, membership, now := newTestSpace(t)
	tokens, err := NewInvitationBatch(id, genesis, membership, now, InvitationOptions{Count: 1, ValidFor: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tokens[0], ".")
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	if _, _, err := VerifyInvitation(strings.Join(parts, "."), genesis, now); err == nil {
		t.Fatal("mutated invitation verified")
	}
	if _, _, err := VerifyInvitation(tokens[0], genesis, now.Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired invitation error = %v", err)
	}
}

func TestInvitationCannotBroadenMembership(t *testing.T) {
	id, genesis, membership, now := newTestSpace(t)
	membership.Document.Permission = PermissionView
	if _, err := NewInvitationBatch(id, genesis, membership, now, InvitationOptions{Count: 1, Permission: PermissionControl}); err == nil {
		t.Fatal("broadened invitation permission was accepted")
	}
}

func TestStrictJSONRejectsUnknownFields(t *testing.T) {
	id, _, _, now := newTestSpace(t)
	doc := SpaceGenesis{
		V: Version, SpaceID: "unused", CreatorPeerID: id.PeerID(),
		CreatorPublicKey: "unused", CreatedAt: now.Unix(),
	}
	_ = doc
	// A valid signature must not make a payload with an unknown field valid.
	raw := []byte(`{"v":1,"space_id":"x","creator_peer_id":"x","creator_public_key":"x","created_at":1,"extra":true}`)
	sig, err := id.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	token := genesisPrefix + "." + encode(raw) + "." + encode(sig)
	if _, err := VerifyGenesis(token); err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
}

func TestJoinRequestAndIssuedMembership(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	tokens, err := NewInvitationBatch(issuerIdentity, genesis, issuerMembership, now, InvitationOptions{
		Count: 1, Permission: PermissionControl, CanSyncSecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	joiningIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joiningWrapping := newTestWrappingIdentity(t)
	requestToken, err := NewJoinRequest(joiningIdentity, joiningWrapping.PublicBytes(), tokens[0], now)
	if err != nil {
		t.Fatal(err)
	}
	request, err := VerifyJoinRequest(requestToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Document.SubjectPeerID != joiningIdentity.PeerID() {
		t.Fatal("join request subject does not own signing key")
	}
	if !bytes.Equal(request.WrappingPublicKey, joiningWrapping.PublicBytes()) {
		t.Fatal("join request does not contain its wrapping public key")
	}
	membershipToken, err := IssueMembership(issuerIdentity, genesis, issuerMembership, request, now)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Document.SubjectPeerID != joiningIdentity.PeerID() || membership.Document.Permission != PermissionControl || !membership.Document.CanSyncSecrets {
		t.Fatalf("unexpected issued membership: %+v", membership.Document)
	}
	if !bytes.Equal(membership.WrappingPublicKey, joiningWrapping.PublicBytes()) {
		t.Fatal("issued membership did not copy the join wrapping public key")
	}
}

func TestVerifyGrantAtIssuanceRetainsExpiredMembership(t *testing.T) {
	identity, genesis, membership, now := newTestSpace(t)
	doc := membership.Document
	doc.ExpiresAt = now.Add(time.Hour).Unix()
	token, err := signDocument(membershipPrefix, doc, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyGrant(token, genesis, now.Add(2*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired membership error = %v", err)
	}
	verified, err := VerifyGrantAtIssuance(token, genesis)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Token != token || verified.Document.ExpiresAt != doc.ExpiresAt {
		t.Fatalf("verified historical membership = %+v", verified.Document)
	}
}

func TestVerifyGrantPreservesExpiredIssuerClassification(t *testing.T) {
	creator, genesis, creatorMembership, now := newTestSpace(t)
	issuerIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	issuerTickets, err := NewInvitationBatch(creator, genesis, creatorMembership, now, InvitationOptions{Count: 1, CanInvite: true})
	if err != nil {
		t.Fatal(err)
	}
	issuerJoinToken, err := NewJoinRequest(issuerIdentity, newTestWrappingIdentity(t).PublicBytes(), issuerTickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	issuerJoin, err := VerifyJoinRequest(issuerJoinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	issuerToken, err := IssueMembership(creator, genesis, creatorMembership, issuerJoin, now)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := VerifyGrant(issuerToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	issuerDoc := issuer.Document
	issuerDoc.ExpiresAt = now.Add(time.Hour).Unix()
	issuerToken, err = signDocument(membershipPrefix, issuerDoc, creator)
	if err != nil {
		t.Fatal(err)
	}
	expiringIssuer, err := VerifyGrant(issuerToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := NewInvitationBatch(issuerIdentity, genesis, expiringIssuer, now, InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	child, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joinToken, err := NewJoinRequest(child, newTestWrappingIdentity(t).PublicBytes(), tickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	join, err := VerifyJoinRequest(joinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	childToken, err := IssueMembership(issuerIdentity, genesis, expiringIssuer, join, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyGrant(childToken, genesis, now.Add(2*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired issuer chain error = %v", err)
	}
	if _, err := VerifyGrantAtIssuance(childToken, genesis); err != nil {
		t.Fatal(err)
	}
}

func TestJoinRequestRejectsDifferentSubjectSignature(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	tokens, err := NewInvitationBatch(issuerIdentity, genesis, issuerMembership, now, InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	joiningIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	requestToken, err := NewJoinRequest(joiningIdentity, newTestWrappingIdentity(t).PublicBytes(), tokens[0], now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(requestToken, ".")
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	if _, err := VerifyJoinRequest(strings.Join(parts, "."), genesis, now); err == nil {
		t.Fatal("join request with modified signature verified")
	}
}

func TestJoinRequestRejectsMutatedWrappingPublicKey(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	tokens, err := NewInvitationBatch(issuerIdentity, genesis, issuerMembership, now, InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	joiningIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	requestToken, err := NewJoinRequest(joiningIdentity, newTestWrappingIdentity(t).PublicBytes(), tokens[0], now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(requestToken, ".")
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var doc JoinRequest
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	doc.SubjectWrappingPublicKey = encode(newTestWrappingIdentity(t).PublicBytes())
	payload, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	parts[1] = encode(payload)
	if _, err := VerifyJoinRequest(strings.Join(parts, "."), genesis, now); err == nil {
		t.Fatal("join request with mutated wrapping public key verified")
	}
}

func TestConnectionBundleRotatesRoutesWithoutRotatingInvitation(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	invitations, err := NewInvitationBatch(issuerIdentity, genesis, issuerMembership, now, InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewConnectionBundle(issuerIdentity, genesis, invitations[0], []ConnectionRoute{{
		Kind: RouteQuickTunnel, URL: "https://first-route.trycloudflare.com",
	}}, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewConnectionBundle(issuerIdentity, genesis, invitations[0], []ConnectionRoute{{
		Kind: RouteQuickTunnel, URL: "https://second-route.trycloudflare.com",
	}}, now.Add(time.Minute), 0)
	if err != nil {
		t.Fatal(err)
	}
	firstBundle, err := VerifyConnectionBundle(first, now)
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := VerifyConnectionBundle(second, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if firstBundle.Ticket == nil || secondBundle.Ticket == nil || firstBundle.Ticket.InviteID != secondBundle.Ticket.InviteID || firstBundle.Document.BundleID == secondBundle.Document.BundleID {
		t.Fatalf("route rotation changed trust or reused bundle id: first=%+v second=%+v", firstBundle.Document, secondBundle.Document)
	}
}

func TestMemberConnectionBundleSurvivesInvitationExpiry(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	routes := []ConnectionRoute{{Kind: RouteQuickTunnel, URL: "https://member-route.trycloudflare.com"}}
	bundle, err := NewMemberConnectionBundle(issuerIdentity, genesis, issuerMembership.Token, routes, now.Add(48*time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyConnectionBundle(bundle, now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if verified.Ticket != nil || verified.Issuer.Document.SubjectPeerID != issuerIdentity.PeerID() || verified.Genesis.Hash != genesis.Hash {
		t.Fatalf("unexpected member route bundle: %+v", verified)
	}
}

func TestConnectionBundleRejectsInsecureOrMutatedRoutes(t *testing.T) {
	issuerIdentity, genesis, issuerMembership, now := newTestSpace(t)
	invitations, err := NewInvitationBatch(issuerIdentity, genesis, issuerMembership, now, InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []ConnectionRoute{
		{Kind: RouteQuickTunnel, URL: "http://unsafe.trycloudflare.com"},
		{Kind: RouteQuickTunnel, URL: "https://trycloudflare.com.evil.example"},
		{Kind: RouteQuickTunnel, URL: "https://nested.unsafe.trycloudflare.com"},
		{Kind: RouteQuickTunnel, URL: "https://trycloudflare.com"},
		{Kind: RouteQuickTunnel, URL: "https://user@unsafe.trycloudflare.com"},
		{Kind: RouteQuickTunnel, URL: "https://unsafe.trycloudflare.com:443"},
		{Kind: RouteQuickTunnel, URL: "https://unsafe.trycloudflare.com/path"},
		{Kind: RouteQuickTunnel, URL: "https://unsafe.trycloudflare.com?"},
		{Kind: RouteQuickTunnel, URL: "https://unsafe.trycloudflare.com#fragment"},
		{Kind: RouteQuickTunnel, URL: "https://*.trycloudflare.com"},
		{Kind: RouteRendezvous, URL: "https://rendezvous.example", Topic: "short"},
	} {
		if _, err := NewConnectionBundle(issuerIdentity, genesis, invitations[0], []ConnectionRoute{route}, now, 0); err == nil {
			t.Fatalf("accepted invalid route: %+v", route)
		}
	}
	token, err := NewConnectionBundle(issuerIdentity, genesis, invitations[0], []ConnectionRoute{{
		Kind: RouteQuickTunnel, URL: "https://valid.trycloudflare.com",
	}}, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	raw[20] ^= 1
	parts[1] = base64.RawURLEncoding.EncodeToString(raw)
	if _, err := VerifyConnectionBundle(strings.Join(parts, "."), now); err == nil {
		t.Fatal("mutated connection bundle verified")
	}
}
