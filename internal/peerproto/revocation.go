package peerproto

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const (
	revocationPrefix        = "arv1"
	maxRevocationCandidates = 4096
)

var (
	ErrRevocationDenied = errors.New("peerproto: revocation denied")
	ErrRevocationFork   = errors.New("peerproto: revocation id fork")
	ErrRevocationLimit  = errors.New("peerproto: revocation candidate limit reached")
)

// RevocationKind identifies an irreversible Peer Space governance denial.
type RevocationKind string

const (
	RevocationMember          RevocationKind = "member"
	RevocationGrant           RevocationKind = "grant"
	RevocationInvitationBatch RevocationKind = "invitation_batch"
)

func (kind RevocationKind) valid() bool {
	return kind == RevocationMember || kind == RevocationGrant || kind == RevocationInvitationBatch
}

// Revocation is an immutable deny-wins governance operation. Invitation batch
// targets are scoped to ActorPeerID, which is also the ticket issuer.
type Revocation struct {
	V                int            `json:"v"`
	RevocationID     string         `json:"revocation_id"`
	SpaceID          string         `json:"space_id"`
	SpaceGenesisHash string         `json:"space_genesis_hash"`
	Kind             RevocationKind `json:"kind"`
	TargetID         string         `json:"target_id"`
	ActorPeerID      string         `json:"actor_peer_id"`
	ActorMembership  string         `json:"actor_membership"`
	CreatedAt        int64          `json:"created_at"`
}

// VerifiedRevocation retains the exact signed token and its stable hash.
type VerifiedRevocation struct {
	Token     string
	Document  Revocation
	PublicKey []byte
	Hash      string
}

// NewRevocation creates one signed, irreversible governance denial. Member and
// grant revocations require an admin membership; an inviter may revoke only
// its own invitation batch.
func NewRevocation(identity *peercrypto.Identity, genesis VerifiedGenesis, actor VerifiedGrant, kind RevocationKind, targetID string, now time.Time) (VerifiedRevocation, error) {
	if identity == nil || now.Unix() <= 0 {
		return VerifiedRevocation{}, ErrInvalidDocument
	}
	verifiedGenesis, err := VerifyGenesis(genesis.Token)
	if err != nil {
		return VerifiedRevocation{}, fmt.Errorf("%w: revocation genesis", ErrInvalidDocument)
	}
	verifiedActor, err := VerifyGrant(actor.Token, verifiedGenesis, now)
	if err != nil || verifiedActor.Document.SubjectPeerID != identity.PeerID() || !canRevoke(kind, verifiedActor) {
		return VerifiedRevocation{}, ErrRevocationDenied
	}
	doc := Revocation{
		V: Version, RevocationID: uuid.NewString(),
		SpaceID: verifiedGenesis.Document.SpaceID, SpaceGenesisHash: verifiedGenesis.Hash,
		Kind: kind, TargetID: targetID, ActorPeerID: identity.PeerID(),
		ActorMembership: verifiedActor.Token, CreatedAt: now.Unix(),
	}
	if err := validateRevocation(doc); err != nil {
		return VerifiedRevocation{}, err
	}
	token, err := signDocument(revocationPrefix, doc, identity)
	if err != nil {
		return VerifiedRevocation{}, err
	}
	return VerifyRevocation(token, verifiedGenesis)
}

// VerifyRevocation validates structure, immutable membership authority, and
// the exact-byte signature. Effective denials are additive and never undone by
// later configuration or membership documents.
func VerifyRevocation(token string, genesis VerifiedGenesis) (VerifiedRevocation, error) {
	verifiedGenesis, err := VerifyGenesis(genesis.Token)
	if err != nil {
		return VerifiedRevocation{}, fmt.Errorf("%w: revocation genesis", ErrInvalidDocument)
	}
	var doc Revocation
	raw, signature, err := parseDocument(revocationPrefix, token, &doc)
	if err != nil {
		return VerifiedRevocation{}, err
	}
	if err := validateRevocation(doc); err != nil || doc.SpaceID != verifiedGenesis.Document.SpaceID || doc.SpaceGenesisHash != verifiedGenesis.Hash {
		return VerifiedRevocation{}, ErrInvalidDocument
	}
	actor, err := VerifyGrant(doc.ActorMembership, verifiedGenesis, time.Unix(doc.CreatedAt, 0))
	if err != nil || actor.Document.SubjectPeerID != doc.ActorPeerID || actor.Document.IssuedAt > doc.CreatedAt || !canRevoke(doc.Kind, actor) {
		return VerifiedRevocation{}, ErrRevocationDenied
	}
	if err := peercrypto.Verify(actor.PublicKey, raw, signature); err != nil {
		return VerifiedRevocation{}, fmt.Errorf("%w: revocation signature", ErrInvalidDocument)
	}
	hash := sha256.Sum256([]byte(token))
	return VerifiedRevocation{
		Token: token, Document: doc, PublicKey: append([]byte(nil), actor.PublicKey...), Hash: encode(hash[:]),
	}, nil
}

// RevocationSet is a convergent grow-only set of verified denials. It retains
// every candidate token for anti-entropy while materializing constant-time
// member, grant, and issuer-scoped batch checks.
type RevocationSet struct {
	mu      sync.RWMutex
	genesis VerifiedGenesis
	byHash  map[string]VerifiedRevocation
	byID    map[string]string
	members map[string]int64
	grants  map[string]int64
	batches map[string]int64
}

// RevocationApplyResult reports whether a token added new durable state.
type RevocationApplyResult struct {
	Stored    bool
	Duplicate bool
}

// NewRevocationSet creates an empty resolver anchored to one Space genesis.
func NewRevocationSet(genesis VerifiedGenesis) (*RevocationSet, error) {
	verifiedGenesis, err := VerifyGenesis(genesis.Token)
	if err != nil {
		return nil, ErrInvalidDocument
	}
	return &RevocationSet{
		genesis: verifiedGenesis,
		byHash:  make(map[string]VerifiedRevocation), byID: make(map[string]string),
		members: make(map[string]int64), grants: make(map[string]int64), batches: make(map[string]int64),
	}, nil
}

// Apply verifies and materializes one denial. Reusing a revocation_id with a
// different valid token is rejected rather than treated as a duplicate.
func (set *RevocationSet) Apply(token string) (RevocationApplyResult, error) {
	verified, err := VerifyRevocation(token, set.genesis)
	if err != nil {
		return RevocationApplyResult{}, err
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	if existingHash, ok := set.byID[verified.Document.RevocationID]; ok {
		if existingHash != verified.Hash {
			return RevocationApplyResult{}, ErrRevocationFork
		}
		return RevocationApplyResult{Duplicate: true}, nil
	}
	if len(set.byHash) >= maxRevocationCandidates {
		return RevocationApplyResult{}, ErrRevocationLimit
	}
	set.byHash[verified.Hash] = cloneVerifiedRevocation(verified)
	set.byID[verified.Document.RevocationID] = verified.Hash
	set.materializeLocked(verified.Document)
	return RevocationApplyResult{Stored: true}, nil
}

// Tokens returns all retained candidates in stable creation-time/hash order.
func (set *RevocationSet) Tokens() []string {
	set.mu.RLock()
	defer set.mu.RUnlock()
	candidates := make([]VerifiedRevocation, 0, len(set.byHash))
	for _, candidate := range set.byHash {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Document.CreatedAt != candidates[j].Document.CreatedAt {
			return candidates[i].Document.CreatedAt < candidates[j].Document.CreatedAt
		}
		return candidates[i].Hash < candidates[j].Hash
	})
	tokens := make([]string, len(candidates))
	for index := range candidates {
		tokens[index] = candidates[index].Token
	}
	return tokens
}

// MemberRevoked reports an irreversible identity-wide denial and its earliest
// signed timestamp.
func (set *RevocationSet) MemberRevoked(peerID string) (int64, bool) {
	set.mu.RLock()
	defer set.mu.RUnlock()
	at, ok := set.members[peerID]
	return at, ok
}

// GrantRevoked reports an irreversible membership-serial denial and its
// earliest signed timestamp.
func (set *RevocationSet) GrantRevoked(serial string) (int64, bool) {
	set.mu.RLock()
	defer set.mu.RUnlock()
	at, ok := set.grants[serial]
	return at, ok
}

// InvitationBatchRevoked reports whether the issuer revoked its batch.
func (set *RevocationSet) InvitationBatchRevoked(issuerPeerID, batchID string) (int64, bool) {
	set.mu.RLock()
	defer set.mu.RUnlock()
	at, ok := set.batches[revokedBatchKey(issuerPeerID, batchID)]
	return at, ok
}

// FilterActiveMemberships re-verifies and removes member/grant denials. The
// returned values are detached from attacker-mutable caller structs.
func (set *RevocationSet) FilterActiveMemberships(memberships []VerifiedGrant, at time.Time) ([]VerifiedGrant, error) {
	set.mu.RLock()
	defer set.mu.RUnlock()
	active := make([]VerifiedGrant, 0, len(memberships))
	seen := make(map[string]struct{}, len(memberships))
	for _, membership := range memberships {
		verified, err := VerifyGrant(membership.Token, set.genesis, at)
		if err != nil {
			return nil, err
		}
		peerID := verified.Document.SubjectPeerID
		if _, duplicate := seen[peerID]; duplicate {
			return nil, ErrInvalidDocument
		}
		seen[peerID] = struct{}{}
		if _, denied := set.members[peerID]; denied {
			continue
		}
		if _, denied := set.grants[verified.Document.Serial]; denied {
			continue
		}
		active = append(active, verified)
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Document.SubjectPeerID < active[j].Document.SubjectPeerID
	})
	return active, nil
}

func (set *RevocationSet) materializeLocked(doc Revocation) {
	var target map[string]int64
	key := doc.TargetID
	switch doc.Kind {
	case RevocationMember:
		target = set.members
	case RevocationGrant:
		target = set.grants
	case RevocationInvitationBatch:
		target = set.batches
		key = revokedBatchKey(doc.ActorPeerID, doc.TargetID)
	}
	if current, exists := target[key]; !exists || doc.CreatedAt < current {
		target[key] = doc.CreatedAt
	}
}

func canRevoke(kind RevocationKind, actor VerifiedGrant) bool {
	if kind == RevocationInvitationBatch {
		return actor.Document.CanInvite
	}
	return actor.Document.Permission == PermissionFull && actor.Document.CanInvite
}

func validateRevocation(doc Revocation) error {
	if doc.V != Version || uuid.Validate(doc.RevocationID) != nil || uuid.Validate(doc.SpaceID) != nil || validateDigest(doc.SpaceGenesisHash, "revocation genesis hash") != nil || !doc.Kind.valid() || validateDigest(doc.ActorPeerID, "revocation actor") != nil || doc.ActorMembership == "" || doc.CreatedAt <= 0 {
		return ErrInvalidDocument
	}
	switch doc.Kind {
	case RevocationMember:
		if validateDigest(doc.TargetID, "revoked member") != nil {
			return ErrInvalidDocument
		}
	case RevocationGrant, RevocationInvitationBatch:
		if uuid.Validate(doc.TargetID) != nil {
			return ErrInvalidDocument
		}
	default:
		return ErrInvalidDocument
	}
	return nil
}

func revokedBatchKey(issuerPeerID, batchID string) string {
	return issuerPeerID + ":" + batchID
}

func cloneVerifiedRevocation(revocation VerifiedRevocation) VerifiedRevocation {
	revocation.PublicKey = append([]byte(nil), revocation.PublicKey...)
	return revocation
}
