// Package peerproto defines the signed, account-independent Peer Space
// documents. Signatures cover the exact JSON bytes carried by each token;
// verifiers never reserialize attacker-controlled input.
package peerproto

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const (
	Version = 1

	genesisPrefix     = "apg1"
	membershipPrefix  = "apm1"
	invitationPrefix  = "atp1"
	joinRequestPrefix = "apj1"
	maxTokenBytes     = 64 << 10
)

var (
	ErrInvalidDocument = errors.New("peerproto: invalid document")
	ErrExpired         = errors.New("peerproto: document expired")
)

type Permission string

const (
	PermissionView    Permission = "view"
	PermissionControl Permission = "control"
	PermissionFull    Permission = "full"
)

func (p Permission) valid() bool {
	return p == PermissionView || p == PermissionControl || p == PermissionFull
}

func permissionRank(p Permission) int {
	switch p {
	case PermissionView:
		return 1
	case PermissionControl:
		return 2
	case PermissionFull:
		return 3
	default:
		return 0
	}
}

// SpaceGenesis is the immutable trust anchor for one Peer Space.
type SpaceGenesis struct {
	V                int    `json:"v"`
	SpaceID          string `json:"space_id"`
	CreatorPeerID    string `json:"creator_peer_id"`
	CreatorPublicKey string `json:"creator_public_key"`
	CreatedAt        int64  `json:"created_at"`
}

// DeviceGrant is a signed membership certificate. IssuerMembership is empty
// for the creator's self-membership and carries the issuer certificate for a
// delegated member, keeping verification independent of an online directory.
type DeviceGrant struct {
	V                 int        `json:"v"`
	Serial            string     `json:"serial"`
	SpaceID           string     `json:"space_id"`
	SpaceGenesisHash  string     `json:"space_genesis_hash"`
	SubjectPeerID     string     `json:"subject_peer_id"`
	SubjectPublicKey  string     `json:"subject_public_key"`
	IssuerPeerID      string     `json:"issuer_peer_id"`
	IssuerMembership  string     `json:"issuer_membership,omitempty"`
	IssuedAt          int64      `json:"issued_at"`
	ExpiresAt         int64      `json:"expires_at,omitempty"`
	Permission        Permission `json:"permission"`
	AllowedSessionIDs []string   `json:"allowed_session_ids"`
	CanInvite         bool       `json:"can_invite"`
	CanSyncSecrets    bool       `json:"can_sync_secrets"`
	DelegationDepth   int        `json:"delegation_depth"`
}

// CapabilityTicket is route-independent and may be created before the
// eventual connection address is known. The pairing secret makes possession
// of the token part of redemption authentication.
type CapabilityTicket struct {
	V                 int        `json:"v"`
	InviteID          string     `json:"invite_id"`
	BatchID           string     `json:"batch_id"`
	SpaceID           string     `json:"space_id"`
	RedemptionPeerID  string     `json:"redemption_peer_id"`
	SpaceGenesisHash  string     `json:"space_genesis_hash"`
	IssuerPeerID      string     `json:"issuer_peer_id"`
	IssuerMembership  string     `json:"issuer_membership"`
	PairingSecret     string     `json:"pairing_secret"`
	IssuedAt          int64      `json:"issued_at"`
	ExpiresAt         int64      `json:"expires_at"`
	MaxUses           int        `json:"max_uses"`
	Permission        Permission `json:"permission"`
	AllowedSessionIDs []string   `json:"allowed_session_ids"`
	CanInvite         bool       `json:"can_invite"`
	CanSyncSecrets    bool       `json:"can_sync_secrets"`
}

// JoinRequest proves that the device redeeming an invitation owns the new
// subject key. The complete invitation is embedded so a gateway can forward
// one self-contained opaque token to the designated redemption peer.
type JoinRequest struct {
	V                int    `json:"v"`
	Invitation       string `json:"invitation"`
	SubjectPeerID    string `json:"subject_peer_id"`
	SubjectPublicKey string `json:"subject_public_key"`
	Nonce            string `json:"nonce"`
	CreatedAt        int64  `json:"created_at"`
}

// VerifiedGenesis retains the exact signed token and decoded public key.
type VerifiedGenesis struct {
	Token     string
	Document  SpaceGenesis
	PublicKey []byte
	Hash      string
}

// VerifiedGrant is a membership chain verified back to a genesis document.
type VerifiedGrant struct {
	Token     string
	Document  DeviceGrant
	PublicKey []byte
}

// VerifiedJoinRequest contains the invitation chain and subject key verified
// from one signed join request.
type VerifiedJoinRequest struct {
	Token     string
	Document  JoinRequest
	Ticket    CapabilityTicket
	Issuer    VerifiedGrant
	PublicKey []byte
}

// NewSpace creates a genesis document and creator self-membership.
func NewSpace(identity *peercrypto.Identity, now time.Time) (genesisToken, membershipToken string, err error) {
	genesis := SpaceGenesis{
		V:                Version,
		SpaceID:          uuid.NewString(),
		CreatorPeerID:    identity.PeerID(),
		CreatorPublicKey: encode(identity.PublicBytes()),
		CreatedAt:        now.Unix(),
	}
	genesisToken, err = signDocument(genesisPrefix, genesis, identity)
	if err != nil {
		return "", "", err
	}
	verified, err := VerifyGenesis(genesisToken)
	if err != nil {
		return "", "", err
	}
	membership := DeviceGrant{
		V:                 Version,
		Serial:            uuid.NewString(),
		SpaceID:           genesis.SpaceID,
		SpaceGenesisHash:  verified.Hash,
		SubjectPeerID:     identity.PeerID(),
		SubjectPublicKey:  encode(identity.PublicBytes()),
		IssuerPeerID:      identity.PeerID(),
		IssuedAt:          now.Unix(),
		Permission:        PermissionFull,
		AllowedSessionIDs: []string{},
		CanInvite:         true,
		CanSyncSecrets:    true,
		DelegationDepth:   0,
	}
	membershipToken, err = signDocument(membershipPrefix, membership, identity)
	if err != nil {
		return "", "", err
	}
	return genesisToken, membershipToken, nil
}

// VerifyGenesis verifies the self-signed immutable trust anchor.
func VerifyGenesis(token string) (VerifiedGenesis, error) {
	var doc SpaceGenesis
	raw, sig, err := parseDocument(genesisPrefix, token, &doc)
	if err != nil {
		return VerifiedGenesis{}, err
	}
	pub, err := decodeSized(doc.CreatorPublicKey, peercrypto.PublicKeySize, "creator public key")
	if err != nil {
		return VerifiedGenesis{}, err
	}
	if err := validateGenesis(doc, pub); err != nil {
		return VerifiedGenesis{}, err
	}
	if err := peercrypto.Verify(pub, raw, sig); err != nil {
		return VerifiedGenesis{}, fmt.Errorf("%w: genesis signature: %v", ErrInvalidDocument, err)
	}
	hash := sha256.Sum256([]byte(token))
	return VerifiedGenesis{
		Token: token, Document: doc, PublicKey: pub,
		Hash: encode(hash[:]),
	}, nil
}

// VerifyGrant validates a membership chain against its immutable genesis.
func VerifyGrant(token string, genesis VerifiedGenesis, now time.Time) (VerifiedGrant, error) {
	return verifyGrant(token, genesis, now, 0)
}

func verifyGrant(token string, genesis VerifiedGenesis, now time.Time, chainDepth int) (VerifiedGrant, error) {
	if chainDepth > 8 {
		return VerifiedGrant{}, fmt.Errorf("%w: membership chain too deep", ErrInvalidDocument)
	}
	var doc DeviceGrant
	raw, sig, err := parseDocument(membershipPrefix, token, &doc)
	if err != nil {
		return VerifiedGrant{}, err
	}
	pub, err := decodeSized(doc.SubjectPublicKey, peercrypto.PublicKeySize, "subject public key")
	if err != nil {
		return VerifiedGrant{}, err
	}
	if err := validateGrant(doc, genesis, pub, now); err != nil {
		return VerifiedGrant{}, err
	}

	issuerPub := genesis.PublicKey
	issuerPermission := PermissionFull
	issuerCanInvite := true
	issuerCanSyncSecrets := true
	issuerSessions := []string(nil)
	issuerDepth := -1
	if doc.IssuerPeerID != genesis.Document.CreatorPeerID {
		if doc.IssuerMembership == "" {
			return VerifiedGrant{}, fmt.Errorf("%w: missing issuer membership", ErrInvalidDocument)
		}
		issuer, err := verifyGrant(doc.IssuerMembership, genesis, now, chainDepth+1)
		if err != nil {
			return VerifiedGrant{}, fmt.Errorf("%w: issuer membership: %v", ErrInvalidDocument, err)
		}
		if issuer.Document.SubjectPeerID != doc.IssuerPeerID {
			return VerifiedGrant{}, fmt.Errorf("%w: issuer peer mismatch", ErrInvalidDocument)
		}
		issuerPub = issuer.PublicKey
		issuerPermission = issuer.Document.Permission
		issuerCanInvite = issuer.Document.CanInvite
		issuerCanSyncSecrets = issuer.Document.CanSyncSecrets
		issuerSessions = issuer.Document.AllowedSessionIDs
		issuerDepth = issuer.Document.DelegationDepth
	}
	if !issuerCanInvite || permissionRank(doc.Permission) > permissionRank(issuerPermission) || (doc.CanSyncSecrets && !issuerCanSyncSecrets) {
		return VerifiedGrant{}, fmt.Errorf("%w: grant broadens issuer capabilities", ErrInvalidDocument)
	}
	if issuerDepth >= 0 && doc.DelegationDepth != issuerDepth+1 {
		return VerifiedGrant{}, fmt.Errorf("%w: invalid delegation depth", ErrInvalidDocument)
	}
	if issuerDepth < 0 && doc.IssuerPeerID != doc.SubjectPeerID && doc.DelegationDepth != 1 {
		return VerifiedGrant{}, fmt.Errorf("%w: creator child depth must be 1", ErrInvalidDocument)
	}
	if !scopeSubset(doc.AllowedSessionIDs, issuerSessions) {
		return VerifiedGrant{}, fmt.Errorf("%w: grant broadens session scope", ErrInvalidDocument)
	}
	if err := peercrypto.Verify(issuerPub, raw, sig); err != nil {
		return VerifiedGrant{}, fmt.Errorf("%w: membership signature: %v", ErrInvalidDocument, err)
	}
	return VerifiedGrant{Token: token, Document: doc, PublicKey: pub}, nil
}

// InvitationOptions controls one pre-signed batch. Count defaults to five and
// validity defaults to 24 hours when their zero values are supplied.
type InvitationOptions struct {
	Count             int
	ValidFor          time.Duration
	Permission        Permission
	AllowedSessionIDs []string
	CanInvite         bool
	CanSyncSecrets    bool
}

// NewInvitationBatch creates independent one-use tickets sharing only a batch
// id. No route URL is included, so tickets survive tunnel rotation.
func NewInvitationBatch(identity *peercrypto.Identity, genesis VerifiedGenesis, membership VerifiedGrant, now time.Time, opts InvitationOptions) ([]string, error) {
	if membership.Document.SubjectPeerID != identity.PeerID() {
		return nil, fmt.Errorf("%w: identity does not own membership", ErrInvalidDocument)
	}
	if !membership.Document.CanInvite {
		return nil, fmt.Errorf("%w: membership cannot invite", ErrInvalidDocument)
	}
	if opts.Count == 0 {
		opts.Count = 5
	}
	if opts.Count < 1 || opts.Count > 100 {
		return nil, fmt.Errorf("%w: invitation count must be 1..100", ErrInvalidDocument)
	}
	if opts.ValidFor == 0 {
		opts.ValidFor = 24 * time.Hour
	}
	if opts.ValidFor < time.Minute || opts.ValidFor > 30*24*time.Hour {
		return nil, fmt.Errorf("%w: invitation validity out of range", ErrInvalidDocument)
	}
	if opts.Permission == "" {
		opts.Permission = PermissionControl
	}
	if !opts.Permission.valid() || permissionRank(opts.Permission) > permissionRank(membership.Document.Permission) {
		return nil, fmt.Errorf("%w: invitation permission broadens membership", ErrInvalidDocument)
	}
	if opts.CanSyncSecrets && !membership.Document.CanSyncSecrets {
		return nil, fmt.Errorf("%w: invitation secret sync broadens membership", ErrInvalidDocument)
	}
	if !scopeSubset(opts.AllowedSessionIDs, membership.Document.AllowedSessionIDs) {
		return nil, fmt.Errorf("%w: invitation broadens session scope", ErrInvalidDocument)
	}
	if err := validateSessionIDs(opts.AllowedSessionIDs); err != nil {
		return nil, err
	}

	batchID := uuid.NewString()
	tokens := make([]string, 0, opts.Count)
	for range opts.Count {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate pairing secret: %w", err)
		}
		doc := CapabilityTicket{
			V:                 Version,
			InviteID:          uuid.NewString(),
			BatchID:           batchID,
			SpaceID:           genesis.Document.SpaceID,
			RedemptionPeerID:  identity.PeerID(),
			SpaceGenesisHash:  genesis.Hash,
			IssuerPeerID:      identity.PeerID(),
			IssuerMembership:  membership.Token,
			PairingSecret:     encode(secret),
			IssuedAt:          now.Unix(),
			ExpiresAt:         now.Add(opts.ValidFor).Unix(),
			MaxUses:           1,
			Permission:        opts.Permission,
			AllowedSessionIDs: append([]string(nil), opts.AllowedSessionIDs...),
			CanInvite:         opts.CanInvite && membership.Document.CanInvite,
			CanSyncSecrets:    opts.CanSyncSecrets,
		}
		token, err := signDocument(invitationPrefix, doc, identity)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// VerifyInvitation authenticates a ticket and the embedded issuer membership.
func VerifyInvitation(token string, genesis VerifiedGenesis, now time.Time) (CapabilityTicket, VerifiedGrant, error) {
	var doc CapabilityTicket
	raw, sig, err := parseDocument(invitationPrefix, token, &doc)
	if err != nil {
		return CapabilityTicket{}, VerifiedGrant{}, err
	}
	issuer, err := VerifyGrant(doc.IssuerMembership, genesis, now)
	if err != nil {
		return CapabilityTicket{}, VerifiedGrant{}, err
	}
	if err := validateInvitation(doc, genesis, issuer, now); err != nil {
		return CapabilityTicket{}, VerifiedGrant{}, err
	}
	if err := peercrypto.Verify(issuer.PublicKey, raw, sig); err != nil {
		return CapabilityTicket{}, VerifiedGrant{}, fmt.Errorf("%w: invitation signature: %v", ErrInvalidDocument, err)
	}
	return doc, issuer, nil
}

// NewJoinRequest creates a short-lived proof of possession for a new device.
func NewJoinRequest(identity *peercrypto.Identity, invitation string, now time.Time) (string, error) {
	if identity == nil || len(invitation) == 0 || len(invitation) > maxTokenBytes || !strings.HasPrefix(invitation, invitationPrefix+".") {
		return "", fmt.Errorf("%w: join request input", ErrInvalidDocument)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate join nonce: %w", err)
	}
	doc := JoinRequest{
		V:                Version,
		Invitation:       invitation,
		SubjectPeerID:    identity.PeerID(),
		SubjectPublicKey: encode(identity.PublicBytes()),
		Nonce:            encode(nonce),
		CreatedAt:        now.Unix(),
	}
	token, err := signDocument(joinRequestPrefix, doc, identity)
	if err != nil {
		return "", err
	}
	if len(token) > maxTokenBytes {
		return "", fmt.Errorf("%w: join request too large", ErrInvalidDocument)
	}
	return token, nil
}

// VerifyJoinRequest validates the subject proof and the embedded invitation.
func VerifyJoinRequest(token string, genesis VerifiedGenesis, now time.Time) (VerifiedJoinRequest, error) {
	var doc JoinRequest
	raw, sig, err := parseDocument(joinRequestPrefix, token, &doc)
	if err != nil {
		return VerifiedJoinRequest{}, err
	}
	if doc.V != Version || doc.CreatedAt <= 0 || doc.CreatedAt < now.Add(-5*time.Minute).Unix() || doc.CreatedAt > now.Add(5*time.Minute).Unix() {
		return VerifiedJoinRequest{}, fmt.Errorf("%w: join request time", ErrInvalidDocument)
	}
	pub, err := decodeSized(doc.SubjectPublicKey, peercrypto.PublicKeySize, "join subject public key")
	if err != nil {
		return VerifiedJoinRequest{}, err
	}
	if peercrypto.PeerID(pub) != doc.SubjectPeerID {
		return VerifiedJoinRequest{}, fmt.Errorf("%w: join subject peer id", ErrInvalidDocument)
	}
	if _, err := decodeSized(doc.Nonce, 32, "join nonce"); err != nil {
		return VerifiedJoinRequest{}, err
	}
	if err := peercrypto.Verify(pub, raw, sig); err != nil {
		return VerifiedJoinRequest{}, fmt.Errorf("%w: join signature: %v", ErrInvalidDocument, err)
	}
	ticket, issuer, err := VerifyInvitation(doc.Invitation, genesis, now)
	if err != nil {
		return VerifiedJoinRequest{}, err
	}
	return VerifiedJoinRequest{
		Token: token, Document: doc, Ticket: ticket, Issuer: issuer, PublicKey: pub,
	}, nil
}

// IssueMembership signs the durable membership created by one verified join.
// The invitation capabilities are copied exactly; redemption cannot widen
// permission, session scope, invite delegation, or secret-sync access.
func IssueMembership(identity *peercrypto.Identity, genesis VerifiedGenesis, issuer VerifiedGrant, join VerifiedJoinRequest, now time.Time) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("%w: membership issuer", ErrInvalidDocument)
	}
	verifiedIssuer, err := VerifyGrant(issuer.Token, genesis, now)
	if err != nil {
		return "", fmt.Errorf("%w: membership issuer: %v", ErrInvalidDocument, err)
	}
	verifiedJoin, err := VerifyJoinRequest(join.Token, genesis, now)
	if err != nil {
		return "", fmt.Errorf("%w: membership join: %v", ErrInvalidDocument, err)
	}
	issuer = verifiedIssuer
	join = verifiedJoin
	if issuer.Document.SubjectPeerID != identity.PeerID() || join.Ticket.IssuerPeerID != identity.PeerID() || join.Ticket.RedemptionPeerID != identity.PeerID() {
		return "", fmt.Errorf("%w: membership issuer", ErrInvalidDocument)
	}
	if join.Ticket.SpaceID != genesis.Document.SpaceID || join.Ticket.SpaceGenesisHash != genesis.Hash || join.Issuer.Token != issuer.Token {
		return "", fmt.Errorf("%w: membership join anchor", ErrInvalidDocument)
	}
	if issuer.Document.DelegationDepth >= 8 {
		return "", fmt.Errorf("%w: membership delegation depth", ErrInvalidDocument)
	}
	doc := DeviceGrant{
		V:                 Version,
		Serial:            uuid.NewString(),
		SpaceID:           genesis.Document.SpaceID,
		SpaceGenesisHash:  genesis.Hash,
		SubjectPeerID:     join.Document.SubjectPeerID,
		SubjectPublicKey:  join.Document.SubjectPublicKey,
		IssuerPeerID:      identity.PeerID(),
		IssuerMembership:  issuer.Token,
		IssuedAt:          now.Unix(),
		Permission:        join.Ticket.Permission,
		AllowedSessionIDs: append([]string(nil), join.Ticket.AllowedSessionIDs...),
		CanInvite:         join.Ticket.CanInvite,
		CanSyncSecrets:    join.Ticket.CanSyncSecrets,
		DelegationDepth:   issuer.Document.DelegationDepth + 1,
	}
	return signDocument(membershipPrefix, doc, identity)
}

func signDocument(prefix string, doc any, identity *peercrypto.Identity) (string, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal peer document: %w", err)
	}
	sig, err := identity.Sign(raw)
	if err != nil {
		return "", err
	}
	return prefix + "." + encode(raw) + "." + encode(sig), nil
}

func parseDocument(prefix, token string, out any) ([]byte, []byte, error) {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return nil, nil, fmt.Errorf("%w: token size", ErrInvalidDocument)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return nil, nil, fmt.Errorf("%w: token prefix", ErrInvalidDocument)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: payload encoding", ErrInvalidDocument)
	}
	sig, err := decodeSized(parts[2], peercrypto.SignatureSize, "signature")
	if err != nil {
		return nil, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return nil, nil, fmt.Errorf("%w: payload JSON: %v", ErrInvalidDocument, err)
	}
	if dec.More() {
		return nil, nil, fmt.Errorf("%w: trailing payload", ErrInvalidDocument)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, nil, fmt.Errorf("%w: trailing payload", ErrInvalidDocument)
	} else if !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%w: trailing payload", ErrInvalidDocument)
	}
	return raw, sig, nil
}

func validateGenesis(doc SpaceGenesis, pub []byte) error {
	if doc.V != Version || uuid.Validate(doc.SpaceID) != nil || doc.CreatedAt <= 0 {
		return fmt.Errorf("%w: genesis fields", ErrInvalidDocument)
	}
	if peercrypto.PeerID(pub) != doc.CreatorPeerID {
		return fmt.Errorf("%w: creator peer id", ErrInvalidDocument)
	}
	return validateDigest(doc.CreatorPeerID, "creator peer id")
}

func validateGrant(doc DeviceGrant, genesis VerifiedGenesis, pub []byte, now time.Time) error {
	if doc.V != Version || uuid.Validate(doc.Serial) != nil || doc.SpaceID != genesis.Document.SpaceID || doc.SpaceGenesisHash != genesis.Hash {
		return fmt.Errorf("%w: membership anchor", ErrInvalidDocument)
	}
	if peercrypto.PeerID(pub) != doc.SubjectPeerID || validateDigest(doc.IssuerPeerID, "issuer peer id") != nil {
		return fmt.Errorf("%w: membership peer id", ErrInvalidDocument)
	}
	if doc.IssuedAt <= 0 || doc.IssuedAt > now.Add(5*time.Minute).Unix() || (doc.ExpiresAt != 0 && doc.ExpiresAt <= doc.IssuedAt) {
		return fmt.Errorf("%w: membership time", ErrInvalidDocument)
	}
	if doc.ExpiresAt != 0 && now.Unix() >= doc.ExpiresAt {
		return ErrExpired
	}
	if !doc.Permission.valid() || doc.DelegationDepth < 0 || doc.DelegationDepth > 8 {
		return fmt.Errorf("%w: membership capabilities", ErrInvalidDocument)
	}
	return validateSessionIDs(doc.AllowedSessionIDs)
}

func validateInvitation(doc CapabilityTicket, genesis VerifiedGenesis, issuer VerifiedGrant, now time.Time) error {
	if doc.V != Version || uuid.Validate(doc.InviteID) != nil || uuid.Validate(doc.BatchID) != nil || doc.SpaceID != genesis.Document.SpaceID || doc.SpaceGenesisHash != genesis.Hash {
		return fmt.Errorf("%w: invitation anchor", ErrInvalidDocument)
	}
	if doc.IssuerPeerID != issuer.Document.SubjectPeerID || doc.RedemptionPeerID != doc.IssuerPeerID || !issuer.Document.CanInvite {
		return fmt.Errorf("%w: invitation issuer", ErrInvalidDocument)
	}
	if _, err := decodeSized(doc.PairingSecret, 32, "pairing secret"); err != nil {
		return err
	}
	if doc.MaxUses != 1 || doc.IssuedAt <= 0 || doc.ExpiresAt <= doc.IssuedAt {
		return fmt.Errorf("%w: invitation lifetime", ErrInvalidDocument)
	}
	if now.Unix() >= doc.ExpiresAt {
		return ErrExpired
	}
	if !doc.Permission.valid() || permissionRank(doc.Permission) > permissionRank(issuer.Document.Permission) || (doc.CanSyncSecrets && !issuer.Document.CanSyncSecrets) {
		return fmt.Errorf("%w: invitation broadens issuer capabilities", ErrInvalidDocument)
	}
	if !scopeSubset(doc.AllowedSessionIDs, issuer.Document.AllowedSessionIDs) {
		return fmt.Errorf("%w: invitation broadens session scope", ErrInvalidDocument)
	}
	return validateSessionIDs(doc.AllowedSessionIDs)
}

func validateDigest(value, name string) error {
	_, err := decodeSized(value, 32, name)
	return err
}

func validateSessionIDs(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if uuid.Validate(id) != nil {
			return fmt.Errorf("%w: invalid session id", ErrInvalidDocument)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate session id", ErrInvalidDocument)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func scopeSubset(child, parent []string) bool {
	if len(parent) == 0 {
		return true
	}
	if len(child) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(parent))
	for _, id := range parent {
		allowed[id] = struct{}{}
	}
	for _, id := range child {
		if _, ok := allowed[id]; !ok {
			return false
		}
	}
	return true
}

func encode(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeSized(value string, size int, name string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != size {
		return nil, fmt.Errorf("%w: invalid %s", ErrInvalidDocument, name)
	}
	return decoded, nil
}
