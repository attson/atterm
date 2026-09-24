package peerproto

import (
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
	genesisToken, membershipToken, err := NewSpace(id, now)
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
	return id, genesis, membership, now
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
