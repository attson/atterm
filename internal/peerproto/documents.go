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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const (
	Version = 1

	// DefaultMembershipValidity bounds newly issued and renewed non-root
	// memberships. The creator's genesis-bound self membership remains
	// non-expiring so renewal never depends on a circular root grant.
	DefaultMembershipValidity = 90 * 24 * time.Hour
	// MaxMembershipValidity caps caller-selected renewal lifetimes.
	MaxMembershipValidity = 365 * 24 * time.Hour

	genesisPrefix     = "apg1"
	membershipPrefix  = "apm1"
	invitationPrefix  = "atp1"
	joinRequestPrefix = "apj1"
	connectionPrefix  = "atc1"
	maxTokenBytes     = 64 << 10
)

var (
	ErrInvalidDocument = errors.New("peerproto: invalid document")
	ErrExpired         = errors.New("peerproto: document expired")
	ErrRenewalDenied   = errors.New("peerproto: membership renewal denied")
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
	V                        int        `json:"v"`
	Serial                   string     `json:"serial"`
	SpaceID                  string     `json:"space_id"`
	SpaceGenesisHash         string     `json:"space_genesis_hash"`
	SubjectPeerID            string     `json:"subject_peer_id"`
	SubjectPublicKey         string     `json:"subject_public_key"`
	SubjectWrappingPublicKey string     `json:"subject_wrapping_public_key"`
	IssuerPeerID             string     `json:"issuer_peer_id"`
	IssuerMembership         string     `json:"issuer_membership,omitempty"`
	IssuedAt                 int64      `json:"issued_at"`
	ExpiresAt                int64      `json:"expires_at,omitempty"`
	Permission               Permission `json:"permission"`
	AllowedSessionIDs        []string   `json:"allowed_session_ids"`
	CanInvite                bool       `json:"can_invite"`
	CanSyncSecrets           bool       `json:"can_sync_secrets"`
	DelegationDepth          int        `json:"delegation_depth"`
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
	V                        int    `json:"v"`
	Invitation               string `json:"invitation"`
	SubjectPeerID            string `json:"subject_peer_id"`
	SubjectPublicKey         string `json:"subject_public_key"`
	SubjectWrappingPublicKey string `json:"subject_wrapping_public_key"`
	Nonce                    string `json:"nonce"`
	CreatedAt                int64  `json:"created_at"`
}

type RouteKind string

const (
	RouteQuickTunnel RouteKind = "quick_tunnel"
	RouteRendezvous  RouteKind = "rendezvous"
	RouteManualLAN   RouteKind = "manual_lan"
)

// ConnectionRoute is a replaceable reachability hint. It never grants access
// by itself and is kept outside CapabilityTicket so route rotation does not
// rotate trust.
type ConnectionRoute struct {
	Kind  RouteKind `json:"kind"`
	URL   string    `json:"url"`
	Topic string    `json:"topic,omitempty"`
}

// ConnectionBundle combines an existing invitation with currently reachable
// routes. The redemption peer signs the complete bundle.
type ConnectionBundle struct {
	V            int               `json:"v"`
	BundleID     string            `json:"bundle_id"`
	Genesis      string            `json:"genesis"`
	Ticket       string            `json:"ticket,omitempty"`
	IssuerPeerID string            `json:"issuer_peer_id"`
	IssuerGrant  string            `json:"issuer_membership"`
	Routes       []ConnectionRoute `json:"routes"`
	CreatedAt    int64             `json:"created_at"`
	ExpiresAt    int64             `json:"expires_at"`
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
	Token             string
	Document          DeviceGrant
	PublicKey         []byte
	WrappingPublicKey []byte
}

// VerifiedJoinRequest contains the invitation chain and subject key verified
// from one signed join request.
type VerifiedJoinRequest struct {
	Token             string
	Document          JoinRequest
	Ticket            CapabilityTicket
	Issuer            VerifiedGrant
	PublicKey         []byte
	WrappingPublicKey []byte
}

// VerifiedConnectionBundle retains the authenticated invitation and issuer.
type VerifiedConnectionBundle struct {
	Token    string
	Document ConnectionBundle
	Genesis  VerifiedGenesis
	Ticket   *CapabilityTicket
	Issuer   VerifiedGrant
}

// NewSpace creates a genesis document and creator self-membership.
func NewSpace(identity *peercrypto.Identity, wrappingPublicKey []byte, now time.Time) (genesisToken, membershipToken string, err error) {
	if identity == nil {
		return "", "", fmt.Errorf("%w: space identity", ErrInvalidDocument)
	}
	if _, err := peercrypto.ValidateWrappingPublicKey(wrappingPublicKey); err != nil {
		return "", "", fmt.Errorf("%w: creator wrapping public key: %v", ErrInvalidDocument, err)
	}
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
		V:                        Version,
		Serial:                   uuid.NewString(),
		SpaceID:                  genesis.SpaceID,
		SpaceGenesisHash:         verified.Hash,
		SubjectPeerID:            identity.PeerID(),
		SubjectPublicKey:         encode(identity.PublicBytes()),
		SubjectWrappingPublicKey: encode(wrappingPublicKey),
		IssuerPeerID:             identity.PeerID(),
		IssuedAt:                 now.Unix(),
		Permission:               PermissionFull,
		AllowedSessionIDs:        []string{},
		CanInvite:                true,
		CanSyncSecrets:           true,
		DelegationDepth:          0,
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

// VerifyGrantAtIssuance verifies an immutable membership at its signed issue
// time. It is used only to retain historical directory entries; callers must
// still use VerifyGrant with the current time before granting access.
func VerifyGrantAtIssuance(token string, genesis VerifiedGenesis) (VerifiedGrant, error) {
	var doc DeviceGrant
	if _, _, err := parseDocument(membershipPrefix, token, &doc); err != nil {
		return VerifiedGrant{}, err
	}
	if doc.IssuedAt <= 0 {
		return VerifiedGrant{}, fmt.Errorf("%w: membership time", ErrInvalidDocument)
	}
	return verifyGrant(token, genesis, time.Unix(doc.IssuedAt, 0), 0)
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
	wrappingPub, err := decodeWrappingPublicKey(doc.SubjectWrappingPublicKey, "subject wrapping public key")
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
			if errors.Is(err, ErrExpired) {
				return VerifiedGrant{}, fmt.Errorf("issuer membership: %w", err)
			}
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
	return VerifiedGrant{Token: token, Document: doc, PublicKey: pub, WrappingPublicKey: wrappingPub}, nil
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

// NewConnectionBundle signs replaceable route hints around an existing
// invitation. Validity defaults to ten minutes and is capped by the ticket.
func NewConnectionBundle(identity *peercrypto.Identity, genesis VerifiedGenesis, invitation string, routes []ConnectionRoute, now time.Time, validFor time.Duration) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("%w: connection bundle issuer", ErrInvalidDocument)
	}
	ticket, issuer, err := VerifyInvitation(invitation, genesis, now)
	if err != nil {
		return "", err
	}
	if ticket.RedemptionPeerID != identity.PeerID() || issuer.Document.SubjectPeerID != identity.PeerID() {
		return "", fmt.Errorf("%w: connection bundle issuer", ErrInvalidDocument)
	}
	return newConnectionBundle(identity, genesis, issuer, invitation, routes, now, validFor, ticket.ExpiresAt)
}

// NewMemberConnectionBundle publishes refreshed routes after invitation
// redemption. The durable issuer membership replaces the one-time ticket as
// the signing chain; the connecting client still proves its own membership in
// the transport handshake.
func NewMemberConnectionBundle(identity *peercrypto.Identity, genesis VerifiedGenesis, issuerMembership string, routes []ConnectionRoute, now time.Time, validFor time.Duration) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("%w: connection bundle issuer", ErrInvalidDocument)
	}
	issuer, err := VerifyGrant(issuerMembership, genesis, now)
	if err != nil {
		return "", err
	}
	if issuer.Document.SubjectPeerID != identity.PeerID() {
		return "", fmt.Errorf("%w: connection bundle issuer", ErrInvalidDocument)
	}
	maxExpiresAt := int64(0)
	if issuer.Document.ExpiresAt != 0 {
		maxExpiresAt = issuer.Document.ExpiresAt
	}
	return newConnectionBundle(identity, genesis, issuer, "", routes, now, validFor, maxExpiresAt)
}

func newConnectionBundle(identity *peercrypto.Identity, genesis VerifiedGenesis, issuer VerifiedGrant, invitation string, routes []ConnectionRoute, now time.Time, validFor time.Duration, maxExpiresAt int64) (string, error) {
	if validFor == 0 {
		validFor = 10 * time.Minute
	}
	if validFor < time.Minute || validFor > 24*time.Hour {
		return "", fmt.Errorf("%w: connection bundle validity", ErrInvalidDocument)
	}
	if err := validateConnectionRoutes(routes); err != nil {
		return "", err
	}
	expiresAt := now.Add(validFor).Unix()
	if maxExpiresAt != 0 && expiresAt > maxExpiresAt {
		expiresAt = maxExpiresAt
	}
	if expiresAt <= now.Unix() {
		return "", ErrExpired
	}
	doc := ConnectionBundle{
		V:            Version,
		BundleID:     uuid.NewString(),
		Genesis:      genesis.Token,
		Ticket:       invitation,
		IssuerPeerID: identity.PeerID(),
		IssuerGrant:  issuer.Token,
		Routes:       append([]ConnectionRoute(nil), routes...),
		CreatedAt:    now.Unix(),
		ExpiresAt:    expiresAt,
	}
	token, err := signDocument(connectionPrefix, doc, identity)
	if err != nil {
		return "", err
	}
	if len(token) > maxTokenBytes {
		return "", fmt.Errorf("%w: connection bundle too large", ErrInvalidDocument)
	}
	return token, nil
}

// VerifyConnectionBundle authenticates both the replaceable route wrapper and
// its route-independent invitation chain.
func VerifyConnectionBundle(token string, now time.Time) (VerifiedConnectionBundle, error) {
	var doc ConnectionBundle
	raw, signature, err := parseDocument(connectionPrefix, token, &doc)
	if err != nil {
		return VerifiedConnectionBundle{}, err
	}
	genesis, err := VerifyGenesis(doc.Genesis)
	if err != nil {
		return VerifiedConnectionBundle{}, fmt.Errorf("%w: connection bundle genesis: %v", ErrInvalidDocument, err)
	}
	issuer, err := VerifyGrant(doc.IssuerGrant, genesis, now)
	if err != nil {
		return VerifiedConnectionBundle{}, err
	}
	var ticket *CapabilityTicket
	maxExpiresAt := issuer.Document.ExpiresAt
	minCreatedAt := issuer.Document.IssuedAt
	if doc.Ticket != "" {
		verifiedTicket, ticketIssuer, err := VerifyInvitation(doc.Ticket, genesis, now)
		if err != nil {
			return VerifiedConnectionBundle{}, err
		}
		if ticketIssuer.Token != issuer.Token || verifiedTicket.RedemptionPeerID != doc.IssuerPeerID {
			return VerifiedConnectionBundle{}, fmt.Errorf("%w: connection bundle ticket issuer", ErrInvalidDocument)
		}
		ticket = &verifiedTicket
		maxExpiresAt = verifiedTicket.ExpiresAt
		if verifiedTicket.IssuedAt > minCreatedAt {
			minCreatedAt = verifiedTicket.IssuedAt
		}
	}
	if doc.V != Version || uuid.Validate(doc.BundleID) != nil || doc.IssuerPeerID != issuer.Document.SubjectPeerID {
		return VerifiedConnectionBundle{}, fmt.Errorf("%w: connection bundle anchor", ErrInvalidDocument)
	}
	if doc.CreatedAt < minCreatedAt || doc.CreatedAt > now.Add(5*time.Minute).Unix() || doc.ExpiresAt <= doc.CreatedAt || maxExpiresAt != 0 && doc.ExpiresAt > maxExpiresAt {
		return VerifiedConnectionBundle{}, fmt.Errorf("%w: connection bundle lifetime", ErrInvalidDocument)
	}
	if now.Unix() >= doc.ExpiresAt {
		return VerifiedConnectionBundle{}, ErrExpired
	}
	if err := validateConnectionRoutes(doc.Routes); err != nil {
		return VerifiedConnectionBundle{}, err
	}
	if err := peercrypto.Verify(issuer.PublicKey, raw, signature); err != nil {
		return VerifiedConnectionBundle{}, fmt.Errorf("%w: connection bundle signature: %v", ErrInvalidDocument, err)
	}
	return VerifiedConnectionBundle{Token: token, Document: doc, Genesis: genesis, Ticket: ticket, Issuer: issuer}, nil
}

// NewJoinRequest creates a short-lived proof of possession for a new device.
func NewJoinRequest(identity *peercrypto.Identity, wrappingPublicKey []byte, invitation string, now time.Time) (string, error) {
	if identity == nil || len(invitation) == 0 || len(invitation) > maxTokenBytes || !strings.HasPrefix(invitation, invitationPrefix+".") {
		return "", fmt.Errorf("%w: join request input", ErrInvalidDocument)
	}
	if _, err := peercrypto.ValidateWrappingPublicKey(wrappingPublicKey); err != nil {
		return "", fmt.Errorf("%w: join wrapping public key: %v", ErrInvalidDocument, err)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate join nonce: %w", err)
	}
	doc := JoinRequest{
		V:                        Version,
		Invitation:               invitation,
		SubjectPeerID:            identity.PeerID(),
		SubjectPublicKey:         encode(identity.PublicBytes()),
		SubjectWrappingPublicKey: encode(wrappingPublicKey),
		Nonce:                    encode(nonce),
		CreatedAt:                now.Unix(),
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
	wrappingPub, err := decodeWrappingPublicKey(doc.SubjectWrappingPublicKey, "join subject wrapping public key")
	if err != nil {
		return VerifiedJoinRequest{}, err
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
		Token: token, Document: doc, Ticket: ticket, Issuer: issuer, PublicKey: pub, WrappingPublicKey: wrappingPub,
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
		V:                        Version,
		Serial:                   uuid.NewString(),
		SpaceID:                  genesis.Document.SpaceID,
		SpaceGenesisHash:         genesis.Hash,
		SubjectPeerID:            join.Document.SubjectPeerID,
		SubjectPublicKey:         join.Document.SubjectPublicKey,
		SubjectWrappingPublicKey: join.Document.SubjectWrappingPublicKey,
		IssuerPeerID:             identity.PeerID(),
		IssuerMembership:         issuer.Token,
		IssuedAt:                 now.Unix(),
		ExpiresAt:                membershipExpiry(now, issuer, DefaultMembershipValidity),
		Permission:               join.Ticket.Permission,
		AllowedSessionIDs:        append([]string(nil), join.Ticket.AllowedSessionIDs...),
		CanInvite:                join.Ticket.CanInvite,
		CanSyncSecrets:           join.Ticket.CanSyncSecrets,
		DelegationDepth:          issuer.Document.DelegationDepth + 1,
	}
	return signDocument(membershipPrefix, doc, identity)
}

// RenewMembership replaces one still-active non-root grant without changing
// the subject's signing key, wrapping key, or capabilities. A fresh serial
// makes renewal auditable and keeps old grant revocations unambiguous.
func RenewMembership(identity *peercrypto.Identity, genesis VerifiedGenesis, issuer, subject VerifiedGrant, now time.Time, validFor time.Duration) (string, error) {
	if identity == nil || identity.PeerID() == subject.Document.SubjectPeerID {
		return "", ErrRenewalDenied
	}
	verifiedIssuer, err := VerifyGrant(issuer.Token, genesis, now)
	if err != nil || verifiedIssuer.Document.SubjectPeerID != identity.PeerID() {
		return "", ErrRenewalDenied
	}
	verifiedSubject, err := VerifyGrant(subject.Token, genesis, now)
	if err != nil || verifiedSubject.Document.SubjectPeerID == genesis.Document.CreatorPeerID {
		return "", ErrRenewalDenied
	}
	issuer = verifiedIssuer
	subject = verifiedSubject
	if !issuer.Document.CanInvite || issuer.Document.DelegationDepth >= 8 ||
		permissionRank(subject.Document.Permission) > permissionRank(issuer.Document.Permission) ||
		(subject.Document.CanSyncSecrets && !issuer.Document.CanSyncSecrets) ||
		!scopeSubset(subject.Document.AllowedSessionIDs, issuer.Document.AllowedSessionIDs) {
		return "", ErrRenewalDenied
	}
	if validFor == 0 {
		validFor = DefaultMembershipValidity
	}
	if validFor < 24*time.Hour || validFor > MaxMembershipValidity {
		return "", fmt.Errorf("%w: renewal validity", ErrInvalidDocument)
	}
	expiresAt := membershipExpiry(now, issuer, validFor)
	if expiresAt <= now.Unix() {
		return "", ErrRenewalDenied
	}
	doc := DeviceGrant{
		V:                        Version,
		Serial:                   uuid.NewString(),
		SpaceID:                  genesis.Document.SpaceID,
		SpaceGenesisHash:         genesis.Hash,
		SubjectPeerID:            subject.Document.SubjectPeerID,
		SubjectPublicKey:         subject.Document.SubjectPublicKey,
		SubjectWrappingPublicKey: subject.Document.SubjectWrappingPublicKey,
		IssuerPeerID:             identity.PeerID(),
		IssuerMembership:         issuer.Token,
		IssuedAt:                 now.Unix(),
		ExpiresAt:                expiresAt,
		Permission:               subject.Document.Permission,
		AllowedSessionIDs:        append([]string(nil), subject.Document.AllowedSessionIDs...),
		CanInvite:                subject.Document.CanInvite,
		CanSyncSecrets:           subject.Document.CanSyncSecrets,
		DelegationDepth:          issuer.Document.DelegationDepth + 1,
	}
	token, err := signDocument(membershipPrefix, doc, identity)
	if err != nil {
		return "", err
	}
	if _, err := VerifyGrant(token, genesis, now); err != nil {
		return "", fmt.Errorf("verify renewed membership: %w", err)
	}
	return token, nil
}

func membershipExpiry(now time.Time, issuer VerifiedGrant, validFor time.Duration) int64 {
	expiresAt := now.Add(validFor).Unix()
	if issuer.Document.ExpiresAt != 0 && issuer.Document.ExpiresAt < expiresAt {
		return issuer.Document.ExpiresAt
	}
	return expiresAt
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

func validateConnectionRoutes(routes []ConnectionRoute) error {
	if len(routes) == 0 || len(routes) > 8 {
		return fmt.Errorf("%w: connection route count", ErrInvalidDocument)
	}
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if len(route.URL) == 0 || len(route.URL) > 2048 {
			return fmt.Errorf("%w: connection route URL", ErrInvalidDocument)
		}
		parsed, err := url.Parse(route.URL)
		if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%w: connection route URL", ErrInvalidDocument)
		}
		switch route.Kind {
		case RouteQuickTunnel:
			host := strings.ToLower(parsed.Hostname())
			if parsed.Scheme != "https" || parsed.Port() != "" || parsed.Path != "" && parsed.Path != "/" || parsed.ForceQuery || parsed.RawFragment != "" || route.Topic != "" || !validQuickTunnelHost(host) {
				return fmt.Errorf("%w: Quick Tunnel route", ErrInvalidDocument)
			}
		case RouteRendezvous:
			if parsed.Scheme != "https" && parsed.Scheme != "wss" ||
				parsed.Hostname() == "" ||
				parsed.Path != "" && parsed.Path != "/" || parsed.RawPath != "" || parsed.ForceQuery {
				return fmt.Errorf("%w: Rendezvous route", ErrInvalidDocument)
			}
			if _, err := decodeSized(route.Topic, 32, "Rendezvous topic"); err != nil {
				return err
			}
		case RouteManualLAN:
			if parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.Port() == "" ||
				parsed.Path != "" && parsed.Path != "/" || parsed.RawPath != "" || parsed.ForceQuery ||
				parsed.RawFragment != "" || route.Topic != "" {
				return fmt.Errorf("%w: manual LAN route", ErrInvalidDocument)
			}
			port, err := strconv.Atoi(parsed.Port())
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("%w: manual LAN route port", ErrInvalidDocument)
			}
		default:
			return fmt.Errorf("%w: connection route kind", ErrInvalidDocument)
		}
		key := string(route.Kind) + "\x00" + route.URL + "\x00" + route.Topic
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate connection route", ErrInvalidDocument)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validQuickTunnelHost(host string) bool {
	const suffix = ".trycloudflare.com"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	label := strings.TrimSuffix(host, suffix)
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
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

func decodeWrappingPublicKey(value, name string) ([]byte, error) {
	decoded, err := decodeSized(value, peercrypto.WrappingPublicKeySize, name)
	if err != nil {
		return nil, err
	}
	if _, err := peercrypto.ValidateWrappingPublicKey(decoded); err != nil {
		return nil, fmt.Errorf("%w: invalid %s", ErrInvalidDocument, name)
	}
	return decoded, nil
}
