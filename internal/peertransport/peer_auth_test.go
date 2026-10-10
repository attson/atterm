package peertransport

import (
	"errors"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

func TestPeerMembershipAuthenticatorHandshake(t *testing.T) {
	clientIdentity, hostIdentity, genesisToken, clientMembership, hostMembership, now := peerAuthDocuments(t)
	clientAuth, err := NewPeerMembershipAuthenticator(clientIdentity, RoleClient, genesisToken, clientMembership, hostMembership, now)
	if err != nil {
		t.Fatal(err)
	}
	hostAuth, err := NewPeerMembershipAuthenticator(hostIdentity, RoleHost, genesisToken, clientMembership, hostMembership, now)
	if err != nil {
		t.Fatal(err)
	}
	if clientAuth.ProofSize() != peercrypto.SignatureSize || hostAuth.ProofSize() != peercrypto.SignatureSize {
		t.Fatal("peer proof size is not P1363")
	}
	if clientAuth.RemoteMembershipToken() != hostMembership || hostAuth.RemoteMembershipToken() != clientMembership {
		t.Fatal("authenticator did not retain the role-bound remote membership")
	}
	authorization := testAuthorization(now.Add(time.Minute))
	authorization.UserID = clientIdentity.PeerID()
	authorization.HostID = hostIdentity.PeerID()
	client, err := NewClientHandshake(clientAuth, authorization)
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewHostHandshake(hostAuth, authorization)
	if err != nil {
		t.Fatal(err)
	}
	clientHello, err := client.ClientHello()
	if err != nil {
		t.Fatal(err)
	}
	hostHello, err := host.Handle(clientHello)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(hostHello.Response), handshakeHeaderSize+p256PublicKeySize+peercrypto.SignatureSize; got != want {
		t.Fatalf("host hello size = %d, want %d", got, want)
	}
	clientFinish, err := client.Handle(hostHello.Response)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(clientFinish.Response), handshakeHeaderSize+peercrypto.SignatureSize+sha256Size; got != want {
		t.Fatalf("client finish size = %d, want %d", got, want)
	}
	hostResult, err := host.Handle(clientFinish.Response)
	if err != nil {
		t.Fatal(err)
	}
	clientResult, err := client.Handle(hostResult.Response)
	if err != nil {
		t.Fatal(err)
	}
	if !hostResult.Authenticated || !clientResult.Authenticated || hostResult.TrafficKeys != clientResult.TrafficKeys {
		t.Fatal("peer membership handshake did not derive matching authenticated keys")
	}
}

func TestPeerMembershipAuthenticatorRejectsWrongIdentityAndSignature(t *testing.T) {
	clientIdentity, hostIdentity, genesisToken, clientMembership, hostMembership, now := peerAuthDocuments(t)
	wrongIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPeerMembershipAuthenticator(wrongIdentity, RoleClient, genesisToken, clientMembership, hostMembership, now); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("wrong local identity error = %v", err)
	}
	clientAuth, err := NewPeerMembershipAuthenticator(clientIdentity, RoleClient, genesisToken, clientMembership, hostMembership, now)
	if err != nil {
		t.Fatal(err)
	}
	hostAuth, err := NewPeerMembershipAuthenticator(hostIdentity, RoleHost, genesisToken, clientMembership, hostMembership, now)
	if err != nil {
		t.Fatal(err)
	}
	_, transcript, _, _ := testTranscript(t)
	proof, err := clientAuth.BuildProof(transcript, RoleClient)
	if err != nil {
		t.Fatal(err)
	}
	proof[0] ^= 1
	if err := hostAuth.VerifyProof(transcript, RoleClient, proof); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("mutated peer signature error = %v", err)
	}
	if _, err := clientAuth.BuildProof(transcript, RoleHost); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("client built host proof: %v", err)
	}
}

func peerAuthDocuments(t *testing.T) (clientIdentity, hostIdentity *peercrypto.Identity, genesisToken, clientMembership, hostMembership string, now time.Time) {
	t.Helper()
	now = time.Unix(1_800_000_000, 0)
	var err error
	hostIdentity, err = peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hostWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesisToken, hostMembership, err = peerproto.NewSpace(hostIdentity, hostWrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	hostGrant, err := peerproto.VerifyGrant(hostMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := peerproto.NewInvitationBatch(hostIdentity, genesis, hostGrant, now, peerproto.InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err = peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	requestToken, err := peerproto.NewJoinRequest(clientIdentity, clientWrapping.PublicBytes(), invitations[0], now)
	if err != nil {
		t.Fatal(err)
	}
	request, err := peerproto.VerifyJoinRequest(requestToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	clientMembership, err = peerproto.IssueMembership(hostIdentity, genesis, hostGrant, request, now)
	if err != nil {
		t.Fatal(err)
	}
	return clientIdentity, hostIdentity, genesisToken, clientMembership, hostMembership, now
}
