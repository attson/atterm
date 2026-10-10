package configsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/google/uuid"
)

const (
	epochRotationVersion       = 1
	epochRotationPrefix        = "akr1"
	maxEpochRotationBytes      = 1 << 20
	maxEpochRotationRecipients = 256
	maxEpochRotationCandidates = 4096
)

var (
	ErrInvalidEpochRotation = errors.New("configsync: invalid epoch rotation")
	ErrEpochRotationDenied  = errors.New("configsync: epoch rotation denied")
	ErrEpochRotationFork    = errors.New("configsync: epoch rotation id fork")
	ErrEpochRotationLimit   = errors.New("configsync: epoch rotation candidate limit reached")
)

// EpochRotationRecipient binds an epoch envelope to the membership grant and
// wrapping key that were active when the rotation was issued.
type EpochRotationRecipient struct {
	PeerID            string `json:"peer_id"`
	GrantSerial       string `json:"grant_serial"`
	WrappingPublicKey string `json:"wrapping_public_key"`
	Envelope          string `json:"envelope"`
}

// EpochRotation is a signed, immutable candidate for one key generation.
// PreviousRotationHash prevents equal epoch numbers with different keys from
// being accidentally chained after a partition heals.
type EpochRotation struct {
	V                    int                      `json:"v"`
	RotationID           string                   `json:"rotation_id"`
	SpaceID              string                   `json:"space_id"`
	KeyClass             KeyClass                 `json:"key_class"`
	PreviousEpoch        uint64                   `json:"previous_epoch"`
	PreviousRotationHash string                   `json:"previous_rotation_hash,omitempty"`
	Epoch                uint64                   `json:"epoch"`
	KeyCommitment        string                   `json:"key_commitment"`
	ActorPeerID          string                   `json:"actor_peer_id"`
	ActorMembership      string                   `json:"actor_membership"`
	Recipients           []EpochRotationRecipient `json:"recipients"`
	CreatedAt            int64                    `json:"created_at"`
}

// VerifiedEpochRotation retains the exact signed token and its deterministic
// operation hash. Lower hashes win among candidates with the same parent.
type VerifiedEpochRotation struct {
	Token     string
	Document  EpochRotation
	PublicKey []byte
	Hash      string
}

// NewEpochRotation creates envelopes for every active member eligible for the
// key class. Active memberships must already have deny-wins revocations
// applied by the governance owner.
func NewEpochRotation(
	identity *peercrypto.Identity,
	genesis peerproto.VerifiedGenesis,
	actor peerproto.VerifiedGrant,
	previous *VerifiedEpochRotation,
	key EpochKey,
	activeMemberships []peerproto.VerifiedGrant,
	now time.Time,
) (VerifiedEpochRotation, error) {
	if identity == nil || !key.valid || now.Unix() <= 0 {
		return VerifiedEpochRotation{}, ErrInvalidEpochRotation
	}
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil {
		return VerifiedEpochRotation{}, fmt.Errorf("%w: genesis", ErrInvalidEpochRotation)
	}
	actor, err = peerproto.VerifyGrant(actor.Token, verifiedGenesis, now)
	if err != nil || actor.Document.SubjectPeerID != identity.PeerID() || !canRotateEpoch(actor) {
		return VerifiedEpochRotation{}, ErrEpochRotationDenied
	}
	previousEpoch := uint64(0)
	previousHash := ""
	if previous != nil {
		verifiedPrevious, err := VerifyEpochRotation(previous.Token, verifiedGenesis)
		if err != nil || verifiedPrevious.Document.KeyClass != key.Class || verifiedPrevious.Document.SpaceID != verifiedGenesis.Document.SpaceID || verifiedPrevious.Document.CreatedAt > now.Unix() {
			return VerifiedEpochRotation{}, ErrInvalidEpochRotation
		}
		previousEpoch = verifiedPrevious.Document.Epoch
		previousHash = verifiedPrevious.Hash
	}
	if key.Epoch != previousEpoch+1 {
		return VerifiedEpochRotation{}, ErrInvalidEpochRotation
	}
	members, err := verifiedActiveMemberships(verifiedGenesis, activeMemberships, now)
	if err != nil {
		return VerifiedEpochRotation{}, err
	}
	if activeActor, ok := members[actor.Document.SubjectPeerID]; !ok || activeActor.Token != actor.Token {
		return VerifiedEpochRotation{}, ErrEpochRotationDenied
	}
	recipients, err := sealRotationRecipients(verifiedGenesis.Document.SpaceID, key, members)
	if err != nil {
		return VerifiedEpochRotation{}, err
	}
	commitment := sha256.Sum256(key.Bytes())
	doc := EpochRotation{
		V: epochRotationVersion, RotationID: uuid.NewString(),
		SpaceID: verifiedGenesis.Document.SpaceID, KeyClass: key.Class,
		PreviousEpoch: previousEpoch, PreviousRotationHash: previousHash, Epoch: key.Epoch,
		KeyCommitment: encode(commitment[:]), ActorPeerID: identity.PeerID(),
		ActorMembership: actor.Token, Recipients: recipients, CreatedAt: now.Unix(),
	}
	token, err := signEpochRotation(doc, identity)
	if err != nil {
		return VerifiedEpochRotation{}, err
	}
	return VerifyEpochRotation(token, verifiedGenesis)
}

// VerifyEpochRotation validates document structure, actor authority, every
// envelope binding, and the exact-byte signature. Revocation freshness and
// the expected active recipient set are checked separately by AuthorizeEpochRotation.
func VerifyEpochRotation(token string, genesis peerproto.VerifiedGenesis) (VerifiedEpochRotation, error) {
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil {
		return VerifiedEpochRotation{}, fmt.Errorf("%w: genesis", ErrInvalidEpochRotation)
	}
	var doc EpochRotation
	raw, signature, err := parseEpochRotation(token, &doc)
	if err != nil {
		return VerifiedEpochRotation{}, err
	}
	if doc.SpaceID != verifiedGenesis.Document.SpaceID {
		return VerifiedEpochRotation{}, ErrInvalidEpochRotation
	}
	actor, err := peerproto.VerifyGrant(doc.ActorMembership, verifiedGenesis, time.Unix(doc.CreatedAt, 0))
	if err != nil || actor.Document.SubjectPeerID != doc.ActorPeerID || actor.Document.IssuedAt > doc.CreatedAt || !canRotateEpoch(actor) {
		return VerifiedEpochRotation{}, ErrEpochRotationDenied
	}
	if err := peercrypto.Verify(actor.PublicKey, raw, signature); err != nil {
		return VerifiedEpochRotation{}, fmt.Errorf("%w: signature", ErrInvalidEpochRotation)
	}
	if err := validateEpochRotationDocument(doc); err != nil {
		return VerifiedEpochRotation{}, err
	}
	hash := sha256.Sum256([]byte(token))
	return VerifiedEpochRotation{
		Token: token, Document: cloneEpochRotation(doc),
		PublicKey: append([]byte(nil), actor.PublicKey...), Hash: encode(hash[:]),
	}, nil
}

// AuthorizeEpochRotation checks the actor and exact recipient set against the
// active membership view after deny-wins revocations have been applied.
func AuthorizeEpochRotation(rotation VerifiedEpochRotation, genesis peerproto.VerifiedGenesis, activeMemberships []peerproto.VerifiedGrant, at time.Time) error {
	if at.Unix() <= 0 {
		return ErrInvalidEpochRotation
	}
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil {
		return ErrInvalidEpochRotation
	}
	verified, err := VerifyEpochRotation(rotation.Token, verifiedGenesis)
	if err != nil {
		return err
	}
	if verified.Document.CreatedAt > at.Add(5*time.Minute).Unix() {
		return ErrInvalidEpochRotation
	}
	members, err := verifiedActiveMemberships(verifiedGenesis, activeMemberships, at)
	if err != nil {
		return err
	}
	actor, ok := members[verified.Document.ActorPeerID]
	if !ok || actor.Token != verified.Document.ActorMembership || !canRotateEpoch(actor) {
		return ErrEpochRotationDenied
	}
	expected := eligibleRotationMembers(verified.Document.KeyClass, members)
	if len(expected) != len(verified.Document.Recipients) {
		return ErrEpochRotationDenied
	}
	for index, member := range expected {
		recipient := verified.Document.Recipients[index]
		if recipient.PeerID != member.Document.SubjectPeerID || recipient.GrantSerial != member.Document.Serial || recipient.WrappingPublicKey != encode(member.WrappingPublicKey) {
			return ErrEpochRotationDenied
		}
	}
	return nil
}

// OpenRotationEpochKey unwraps this device's envelope and verifies that every
// accepted plaintext key matches the signed rotation commitment.
func OpenRotationEpochKey(rotation VerifiedEpochRotation, genesis peerproto.VerifiedGenesis, peerID string, identity *peercrypto.WrappingIdentity) (EpochKey, error) {
	if identity == nil {
		return EpochKey{}, ErrInvalidEpochRotation
	}
	verified, err := VerifyEpochRotation(rotation.Token, genesis)
	if err != nil {
		return EpochKey{}, err
	}
	for _, recipient := range verified.Document.Recipients {
		if recipient.PeerID != peerID {
			continue
		}
		key, err := OpenEpochKey(recipient.Envelope, verified.Document.SpaceID, peerID, identity)
		if err != nil {
			return EpochKey{}, err
		}
		if err := ValidateEpochKeyForRotation(key, verified); err != nil {
			return EpochKey{}, ErrInvalidEpochRotation
		}
		return key, nil
	}
	return EpochKey{}, ErrEpochRotationDenied
}

// ValidateEpochKeyForRotation binds a key recovered from secure storage or a
// bootstrap envelope to the signed canonical rotation that committed it.
func ValidateEpochKeyForRotation(key EpochKey, rotation VerifiedEpochRotation) error {
	if !key.valid || key.Class != rotation.Document.KeyClass || key.Epoch != rotation.Document.Epoch {
		return ErrInvalidEpochKey
	}
	commitment := sha256.Sum256(key.Bytes())
	if encode(commitment[:]) != rotation.Document.KeyCommitment {
		return ErrInvalidEpochKey
	}
	return nil
}

// EpochRotationAuthorizer resolves revocations and the active membership view
// at the rotation's governance point. It must fail closed.
type EpochRotationAuthorizer func(VerifiedEpochRotation) error

// EpochRotationResolver deterministically selects one branch while retaining
// losing and out-of-order candidates for later anti-entropy/rebase decisions.
type EpochRotationResolver struct {
	mu        sync.RWMutex
	genesis   peerproto.VerifiedGenesis
	keyClass  KeyClass
	authorize EpochRotationAuthorizer
	byHash    map[string]VerifiedEpochRotation
	byID      map[string]string
	canonical []string
}

// EpochRotationApplyResult reports whether the candidate changed the selected
// branch. Current is empty until a root candidate is available.
type EpochRotationApplyResult struct {
	Stored           bool
	Duplicate        bool
	CanonicalChanged bool
	Current          *VerifiedEpochRotation
}

// NewEpochRotationResolver creates a fail-closed resolver for one key class.
// The authorizer must evaluate current deny-wins membership state on every
// candidate, including candidates received through anti-entropy.
func NewEpochRotationResolver(genesis peerproto.VerifiedGenesis, class KeyClass, authorize EpochRotationAuthorizer) (*EpochRotationResolver, error) {
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil || !class.valid() || authorize == nil {
		return nil, ErrInvalidEpochRotation
	}
	return &EpochRotationResolver{
		genesis: verifiedGenesis, keyClass: class, authorize: authorize,
		byHash: make(map[string]VerifiedEpochRotation), byID: make(map[string]string),
	}, nil
}

// Apply verifies and authorizes a candidate before storing it. A conflicting
// token reusing rotation_id is rejected even if its signature is valid.
func (r *EpochRotationResolver) Apply(token string) (EpochRotationApplyResult, error) {
	verified, err := VerifyEpochRotation(token, r.genesis)
	if err != nil {
		return EpochRotationApplyResult{}, err
	}
	if verified.Document.KeyClass != r.keyClass {
		return EpochRotationApplyResult{}, ErrInvalidEpochRotation
	}
	if err := r.authorize(verified); err != nil {
		return EpochRotationApplyResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingHash, ok := r.byID[verified.Document.RotationID]; ok {
		if existingHash != verified.Hash {
			return EpochRotationApplyResult{}, ErrEpochRotationFork
		}
		current := r.currentLocked()
		return EpochRotationApplyResult{Duplicate: true, Current: current}, nil
	}
	if len(r.byHash) >= maxEpochRotationCandidates {
		return EpochRotationApplyResult{}, ErrEpochRotationLimit
	}
	before := append([]string(nil), r.canonical...)
	r.byHash[verified.Hash] = cloneVerifiedEpochRotation(verified)
	r.byID[verified.Document.RotationID] = verified.Hash
	r.recomputeCanonicalLocked()
	return EpochRotationApplyResult{
		Stored: true, CanonicalChanged: !equalStrings(before, r.canonical), Current: r.currentLocked(),
	}, nil
}

// Current returns the selected head. The returned value is detached.
func (r *EpochRotationResolver) Current() (VerifiedEpochRotation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	current := r.currentLocked()
	if current == nil {
		return VerifiedEpochRotation{}, false
	}
	return cloneVerifiedEpochRotation(*current), true
}

// Tokens returns every retained candidate in deterministic epoch/hash order.
// Losing branches remain available so peers can independently reach the same
// winner and diagnose which branch needs rebasing.
func (r *EpochRotationResolver) Tokens() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	candidates := make([]VerifiedEpochRotation, 0, len(r.byHash))
	for _, candidate := range r.byHash {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Document.Epoch != candidates[j].Document.Epoch {
			return candidates[i].Document.Epoch < candidates[j].Document.Epoch
		}
		return candidates[i].Hash < candidates[j].Hash
	})
	tokens := make([]string, len(candidates))
	for index := range candidates {
		tokens[index] = candidates[index].Token
	}
	return tokens
}

func (r *EpochRotationResolver) recomputeCanonicalLocked() {
	children := make(map[string][]VerifiedEpochRotation)
	for _, candidate := range r.byHash {
		parent := candidate.Document.PreviousRotationHash
		if candidate.Document.PreviousEpoch == 0 {
			if parent == "" && candidate.Document.Epoch == 1 {
				children[""] = append(children[""], candidate)
			}
			continue
		}
		parentRotation, ok := r.byHash[parent]
		if !ok || parentRotation.Document.Epoch != candidate.Document.PreviousEpoch || candidate.Document.Epoch != candidate.Document.PreviousEpoch+1 || candidate.Document.CreatedAt < parentRotation.Document.CreatedAt {
			continue
		}
		children[parent] = append(children[parent], candidate)
	}
	r.canonical = r.canonical[:0]
	parent := ""
	for {
		options := children[parent]
		if len(options) == 0 {
			return
		}
		sort.Slice(options, func(i, j int) bool { return options[i].Hash < options[j].Hash })
		winner := options[0]
		r.canonical = append(r.canonical, winner.Hash)
		parent = winner.Hash
	}
}

func (r *EpochRotationResolver) currentLocked() *VerifiedEpochRotation {
	if len(r.canonical) == 0 {
		return nil
	}
	current := cloneVerifiedEpochRotation(r.byHash[r.canonical[len(r.canonical)-1]])
	return &current
}

func sealRotationRecipients(spaceID string, key EpochKey, members map[string]peerproto.VerifiedGrant) ([]EpochRotationRecipient, error) {
	eligible := eligibleRotationMembers(key.Class, members)
	if len(eligible) == 0 || len(eligible) > maxEpochRotationRecipients {
		return nil, ErrInvalidEpochRotation
	}
	recipients := make([]EpochRotationRecipient, 0, len(eligible))
	for _, member := range eligible {
		envelope, err := SealEpochKey(spaceID, key, EpochRecipient{
			PeerID: member.Document.SubjectPeerID, WrappingPublicKey: member.WrappingPublicKey,
			CanSyncSecrets: member.Document.CanSyncSecrets,
		})
		if err != nil {
			return nil, err
		}
		recipients = append(recipients, EpochRotationRecipient{
			PeerID: member.Document.SubjectPeerID, GrantSerial: member.Document.Serial,
			WrappingPublicKey: encode(member.WrappingPublicKey), Envelope: envelope,
		})
	}
	return recipients, nil
}

func eligibleRotationMembers(class KeyClass, members map[string]peerproto.VerifiedGrant) []peerproto.VerifiedGrant {
	out := make([]peerproto.VerifiedGrant, 0, len(members))
	for _, member := range members {
		if class == KeyClassVault && !member.Document.CanSyncSecrets {
			continue
		}
		out = append(out, member)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Document.SubjectPeerID < out[j].Document.SubjectPeerID })
	return out
}

func verifiedActiveMemberships(genesis peerproto.VerifiedGenesis, memberships []peerproto.VerifiedGrant, at time.Time) (map[string]peerproto.VerifiedGrant, error) {
	if len(memberships) == 0 || len(memberships) > maxEpochRotationRecipients {
		return nil, ErrInvalidEpochRotation
	}
	out := make(map[string]peerproto.VerifiedGrant, len(memberships))
	for _, candidate := range memberships {
		verified, err := peerproto.VerifyGrant(candidate.Token, genesis, at)
		if err != nil {
			return nil, ErrInvalidEpochRotation
		}
		peerID := verified.Document.SubjectPeerID
		if _, duplicate := out[peerID]; duplicate {
			return nil, ErrInvalidEpochRotation
		}
		out[peerID] = verified
	}
	return out, nil
}

func canRotateEpoch(grant peerproto.VerifiedGrant) bool {
	return grant.Document.Permission == peerproto.PermissionFull && grant.Document.CanInvite
}

func signEpochRotation(doc EpochRotation, identity *peercrypto.Identity) (string, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal epoch rotation: %w", err)
	}
	signature, err := identity.Sign(raw)
	if err != nil {
		return "", err
	}
	token := epochRotationPrefix + "." + encode(raw) + "." + encode(signature)
	if len(token) > maxEpochRotationBytes {
		return "", ErrInvalidEpochRotation
	}
	return token, nil
}

func parseEpochRotation(token string, doc *EpochRotation) ([]byte, []byte, error) {
	if len(token) == 0 || len(token) > maxEpochRotationBytes {
		return nil, nil, ErrInvalidEpochRotation
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != epochRotationPrefix {
		return nil, nil, ErrInvalidEpochRotation
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || encode(raw) != parts[1] {
		return nil, nil, ErrInvalidEpochRotation
	}
	signature, err := decodeSized(parts[2], peercrypto.SignatureSize, "rotation signature")
	if err != nil {
		return nil, nil, ErrInvalidEpochRotation
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(doc); err != nil {
		return nil, nil, ErrInvalidEpochRotation
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil || !errors.Is(err, io.EOF) {
		return nil, nil, ErrInvalidEpochRotation
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, nil, ErrInvalidEpochRotation
	}
	return raw, signature, nil
}

func validateEpochRotationDocument(doc EpochRotation) error {
	if doc.V != epochRotationVersion || uuid.Validate(doc.RotationID) != nil || !validSpaceID(doc.SpaceID) || !doc.KeyClass.valid() || doc.CreatedAt <= 0 || !validPeerID(doc.ActorPeerID) {
		return ErrInvalidEpochRotation
	}
	if doc.Epoch != doc.PreviousEpoch+1 || doc.Epoch == 0 {
		return ErrInvalidEpochRotation
	}
	if doc.PreviousEpoch == 0 {
		if doc.PreviousRotationHash != "" {
			return ErrInvalidEpochRotation
		}
	} else if err := validateDigest(doc.PreviousRotationHash); err != nil {
		return ErrInvalidEpochRotation
	}
	if err := validateDigest(doc.KeyCommitment); err != nil || len(doc.Recipients) == 0 || len(doc.Recipients) > maxEpochRotationRecipients {
		return ErrInvalidEpochRotation
	}
	previousPeerID := ""
	for _, recipient := range doc.Recipients {
		if !validPeerID(recipient.PeerID) || uuid.Validate(recipient.GrantSerial) != nil || recipient.PeerID <= previousPeerID {
			return ErrInvalidEpochRotation
		}
		wrappingKey, err := decodeSized(recipient.WrappingPublicKey, peercrypto.WrappingPublicKeySize, "rotation wrapping public key")
		if err != nil {
			return ErrInvalidEpochRotation
		}
		if _, err := peercrypto.ValidateWrappingPublicKey(wrappingKey); err != nil {
			return ErrInvalidEpochRotation
		}
		info, err := InspectEpochEnvelope(recipient.Envelope)
		if err != nil || info.SpaceID != doc.SpaceID || info.KeyClass != doc.KeyClass || info.Epoch != doc.Epoch || info.RecipientPeerID != recipient.PeerID || !bytes.Equal(info.RecipientWrappingKey, wrappingKey) {
			return ErrInvalidEpochRotation
		}
		previousPeerID = recipient.PeerID
	}
	return nil
}

func validateDigest(value string) error {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != sha256.Size || encode(decoded) != value {
		return ErrInvalidEpochRotation
	}
	return nil
}

func cloneEpochRotation(doc EpochRotation) EpochRotation {
	doc.Recipients = append([]EpochRotationRecipient(nil), doc.Recipients...)
	return doc
}

func cloneVerifiedEpochRotation(rotation VerifiedEpochRotation) VerifiedEpochRotation {
	rotation.PublicKey = append([]byte(nil), rotation.PublicKey...)
	rotation.Document = cloneEpochRotation(rotation.Document)
	return rotation
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
