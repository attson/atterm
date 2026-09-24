package peertransport

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

const (
	peerProofDomain   = "atterm-peer-membership-proof-v1"
	peerBindingDomain = "atterm-peer-membership-binding-v1"
)

// PeerMembershipAuthenticator binds the existing direct handshake to two
// membership chains instead of a Relay account key. Signatures prove the
// local identity role; the authenticated genesis and membership hashes bind
// the ephemeral-ECDH traffic key schedule to both members.
type PeerMembershipAuthenticator struct {
	identity         *peercrypto.Identity
	localRole        Role
	genesisHash      string
	clientMembership peerproto.VerifiedGrant
	hostMembership   peerproto.VerifiedGrant
}

// NewPeerMembershipAuthenticator verifies every supplied document again so a
// caller cannot forge or mutate a VerifiedGrant value before the handshake.
func NewPeerMembershipAuthenticator(identity *peercrypto.Identity, localRole Role, genesisToken, clientMembershipToken, hostMembershipToken string, now time.Time) (*PeerMembershipAuthenticator, error) {
	if identity == nil || localRole != RoleClient && localRole != RoleHost {
		return nil, fmt.Errorf("%w: peer membership role", ErrInvalidKey)
	}
	genesis, err := peerproto.VerifyGenesis(genesisToken)
	if err != nil {
		return nil, fmt.Errorf("%w: peer genesis: %v", ErrInvalidKey, err)
	}
	clientMembership, err := peerproto.VerifyGrant(clientMembershipToken, genesis, now)
	if err != nil {
		return nil, fmt.Errorf("%w: client membership: %v", ErrInvalidKey, err)
	}
	hostMembership, err := peerproto.VerifyGrant(hostMembershipToken, genesis, now)
	if err != nil {
		return nil, fmt.Errorf("%w: host membership: %v", ErrInvalidKey, err)
	}
	if clientMembership.Document.SubjectPeerID == hostMembership.Document.SubjectPeerID {
		return nil, fmt.Errorf("%w: peer roles use the same identity", ErrInvalidKey)
	}
	localMembership := clientMembership
	if localRole == RoleHost {
		localMembership = hostMembership
	}
	if localMembership.Document.SubjectPeerID != identity.PeerID() {
		return nil, fmt.Errorf("%w: identity does not own local membership", ErrInvalidKey)
	}
	auth := &PeerMembershipAuthenticator{
		identity:         identity,
		localRole:        localRole,
		genesisHash:      genesis.Hash,
		clientMembership: clientMembership,
		hostMembership:   hostMembership,
	}
	return auth, nil
}

func (a *PeerMembershipAuthenticator) ProofSize() int { return peercrypto.SignatureSize }

func (a *PeerMembershipAuthenticator) BuildProof(transcript []byte, role Role) ([]byte, error) {
	if a == nil || a.identity == nil || role != a.localRole {
		return nil, fmt.Errorf("%w: peer proof role", ErrAuthentication)
	}
	return a.identity.Sign(peerProofPayload(transcript, role))
}

func (a *PeerMembershipAuthenticator) VerifyProof(transcript []byte, role Role, proof []byte) error {
	if a == nil || a.identity == nil || role == a.localRole || role != RoleClient && role != RoleHost {
		return fmt.Errorf("%w: peer proof role", ErrAuthentication)
	}
	publicKey := a.clientMembership.PublicKey
	if role == RoleHost {
		publicKey = a.hostMembership.PublicKey
	}
	if err := peercrypto.Verify(publicKey, peerProofPayload(transcript, role), proof); err != nil {
		return fmt.Errorf("%w: peer signature: %v", ErrAuthentication, err)
	}
	return nil
}

func (a *PeerMembershipAuthenticator) KeyBinding(transcript []byte) ([]byte, error) {
	if a == nil || a.identity == nil {
		return nil, fmt.Errorf("%w: nil peer authenticator", ErrInvalidKey)
	}
	clientHash := sha256.Sum256([]byte(a.clientMembership.Token))
	hostHash := sha256.Sum256([]byte(a.hostMembership.Token))
	payload := make([]byte, 0, len(peerBindingDomain)+len(a.genesisHash)+2*sha256Size+len(transcript))
	payload = append(payload, peerBindingDomain...)
	payload = append(payload, a.genesisHash...)
	payload = append(payload, clientHash[:]...)
	payload = append(payload, hostHash[:]...)
	payload = append(payload, transcript...)
	binding := sha256.Sum256(payload)
	return binding[:], nil
}

func peerProofPayload(transcript []byte, role Role) []byte {
	payload := make([]byte, 0, len(peerProofDomain)+1+len(transcript))
	payload = append(payload, peerProofDomain...)
	payload = append(payload, byte(role))
	return append(payload, transcript...)
}
