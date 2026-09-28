package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/quicktunnel"
)

const peerJoinTimeout = 30 * time.Second

var errPeerJoinRejected = errors.New("Peer Space join rejected")

// PeerConnectionPreview contains only authenticated, non-secret fields needed
// for explicit trust and capability confirmation.
type PeerConnectionPreview struct {
	SpaceID             string   `json:"space_id"`
	Fingerprint         string   `json:"fingerprint"`
	IssuerPeerID        string   `json:"issuer_peer_id"`
	Permission          string   `json:"permission"`
	AllowedSessionIDs   []string `json:"allowed_session_ids"`
	CanInvite           bool     `json:"can_invite"`
	CanSyncSecrets      bool     `json:"can_sync_secrets"`
	InvitationExpiresAt int64    `json:"invitation_expires_at"`
	BundleExpiresAt     int64    `json:"bundle_expires_at"`
	RouteKind           string   `json:"route_kind"`
	RouteURL            string   `json:"route_url"`
}

// JoinPeerSpaceReq binds one signed connection bundle to the trust fingerprint
// the user confirmed in the immediately preceding preview.
type JoinPeerSpaceReq struct {
	ConnectionBundle    string `json:"connection_bundle"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

// PreviewPeerConnectionBundle verifies a pasted token or fragment-only deep
// link without creating identity, contacting the host, or mutating Peer state.
func (a *App) PreviewPeerConnectionBundle(raw string) (PeerConnectionPreview, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerConnectionPreview{}, err
	}
	_, preview, err := manager.inspectConnectionBundle(raw)
	return preview, err
}

// JoinPeerSpace redeems one confirmed invitation and reports success only
// after membership, governance and recipient-bound epoch material are durable.
func (a *App) JoinPeerSpace(req JoinPeerSpaceReq) (PeerSpaceStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, peerJoinTimeout)
	defer cancel()
	return manager.joinSpace(ctx, req)
}

func (m *peerSpaceManager) inspectConnectionBundle(raw string) (peerproto.VerifiedConnectionBundle, PeerConnectionPreview, error) {
	token, err := normalizePeerConnectionBundle(raw)
	if err != nil {
		return peerproto.VerifiedConnectionBundle{}, PeerConnectionPreview{}, err
	}
	bundle, err := peerproto.VerifyConnectionBundle(token, m.now())
	if err != nil || bundle.Ticket == nil {
		return peerproto.VerifiedConnectionBundle{}, PeerConnectionPreview{}, errPeerJoinRejected
	}
	var route peerproto.ConnectionRoute
	for _, candidate := range bundle.Document.Routes {
		if candidate.Kind == peerproto.RouteQuickTunnel {
			route = candidate
			break
		}
	}
	if route.URL == "" {
		return peerproto.VerifiedConnectionBundle{}, PeerConnectionPreview{}, errPeerJoinRejected
	}
	ticket := bundle.Ticket
	return bundle, PeerConnectionPreview{
		SpaceID: bundle.Genesis.Document.SpaceID, Fingerprint: peerGenesisFingerprint(bundle.Genesis),
		IssuerPeerID: bundle.Document.IssuerPeerID, Permission: string(ticket.Permission),
		AllowedSessionIDs: append([]string(nil), ticket.AllowedSessionIDs...),
		CanInvite:         ticket.CanInvite, CanSyncSecrets: ticket.CanSyncSecrets,
		InvitationExpiresAt: ticket.ExpiresAt, BundleExpiresAt: bundle.Document.ExpiresAt,
		RouteKind: string(route.Kind), RouteURL: route.URL,
	}, nil
}

func normalizePeerConnectionBundle(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "atc1.") {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.RawQuery != "" || parsed.Fragment == "" {
		return "", errPeerJoinRejected
	}
	token := strings.TrimSpace(parsed.Fragment)
	if !strings.HasPrefix(token, "atc1.") {
		return "", errPeerJoinRejected
	}
	return token, nil
}

func peerGenesisFingerprint(genesis peerproto.VerifiedGenesis) string {
	return "SHA256:" + genesis.Hash
}

func (m *peerSpaceManager) joinSpace(ctx context.Context, req JoinPeerSpaceReq) (PeerSpaceStatus, error) {
	release, err := m.acquireBootstrapLock()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	defer release()

	if status, err := m.status(); err == nil && status.Configured {
		return PeerSpaceStatus{}, peerstore.ErrAlreadyExists
	} else if err != nil && !errors.Is(err, peerstore.ErrNotInitialized) {
		return PeerSpaceStatus{}, err
	}
	bundle, preview, err := m.inspectConnectionBundle(req.ConnectionBundle)
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	expected := strings.TrimSpace(req.ExpectedFingerprint)
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(preview.Fingerprint)) != 1 {
		return PeerSpaceStatus{}, errors.New("Peer Space fingerprint confirmation does not match")
	}
	identity, err := m.ensureIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	wrapping, err := m.ensureWrappingIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	if err := m.ensureStoreKey(); err != nil {
		return PeerSpaceStatus{}, err
	}
	join := m.joinQuickTunnel
	if join == nil {
		join = quicktunnel.Join
	}
	bootstrap, err := join(ctx, quicktunnel.JoinClientConfig{
		BundleToken: bundle.Token, Identity: identity, WrappingPublicKey: wrapping.PublicBytes(),
	})
	if err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("%w: %v", errPeerJoinRejected, err)
	}
	state, keys, err := validatePeerJoinBootstrap(bundle, bootstrap, identity, wrapping, m.now())
	if err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("%w: bootstrap validation", errPeerJoinRejected)
	}
	savedClasses := make([]configsync.KeyClass, 0, len(keys))
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		key, ok := keys[class]
		if !ok {
			continue
		}
		if err := savePeerEpochKey(bundle.Genesis.Document.SpaceID, key); err != nil {
			clearPeerJoinEpochKeys(bundle.Genesis.Document.SpaceID, savedClasses)
			return PeerSpaceStatus{}, fmt.Errorf("save Peer Space %s epoch key: %w", class, err)
		}
		savedClasses = append(savedClasses, class)
	}
	if err := m.store.Initialize(state); err != nil {
		clearPeerJoinEpochKeys(bundle.Genesis.Document.SpaceID, savedClasses)
		return PeerSpaceStatus{}, err
	}
	return m.status()
}

func validatePeerJoinBootstrap(
	bundle peerproto.VerifiedConnectionBundle,
	bootstrap quicktunnel.JoinBootstrap,
	identity *peercrypto.Identity,
	wrapping *peercrypto.WrappingIdentity,
	now time.Time,
) (peerstore.State, map[configsync.KeyClass]configsync.EpochKey, error) {
	if bootstrap.GenesisToken != bundle.Genesis.Token || bundle.Ticket == nil || identity == nil || wrapping == nil {
		return peerstore.State{}, nil, errPeerJoinRejected
	}
	genesis, err := peerproto.VerifyGenesis(bootstrap.GenesisToken)
	if err != nil || genesis.Hash != bundle.Genesis.Hash {
		return peerstore.State{}, nil, errPeerJoinRejected
	}
	membership, err := peerproto.VerifyGrant(bootstrap.MembershipToken, genesis, now)
	if err != nil || membership.Document.SubjectPeerID != identity.PeerID() || !bytes.Equal(membership.WrappingPublicKey, wrapping.PublicBytes()) {
		return peerstore.State{}, nil, errPeerJoinRejected
	}
	ticket := bundle.Ticket
	if membership.Document.IssuerPeerID != ticket.IssuerPeerID || membership.Document.IssuerMembership != ticket.IssuerMembership ||
		membership.Document.Permission != ticket.Permission || !slices.Equal(membership.Document.AllowedSessionIDs, ticket.AllowedSessionIDs) ||
		membership.Document.CanInvite != ticket.CanInvite || membership.Document.CanSyncSecrets != ticket.CanSyncSecrets {
		return peerstore.State{}, nil, errPeerJoinRejected
	}
	if err := validatePeerJoinGovernance(genesis, bootstrap, membership, now); err != nil {
		return peerstore.State{}, nil, err
	}
	rotations, err := currentEpochRotations(bootstrap.EpochRotations, genesis)
	if err != nil {
		return peerstore.State{}, nil, err
	}
	required := []configsync.KeyClass{configsync.KeyClassSync}
	if membership.Document.CanSyncSecrets {
		required = append(required, configsync.KeyClassVault)
	}
	if len(bootstrap.EpochEnvelopes) != len(required) {
		return peerstore.State{}, nil, errPeerJoinRejected
	}
	keys := make(map[configsync.KeyClass]configsync.EpochKey, len(required))
	for _, envelope := range bootstrap.EpochEnvelopes {
		info, err := configsync.InspectEpochEnvelope(envelope)
		if err != nil || info.SpaceID != genesis.Document.SpaceID || info.RecipientPeerID != identity.PeerID() || !bytes.Equal(info.RecipientWrappingKey, wrapping.PublicBytes()) {
			return peerstore.State{}, nil, errPeerJoinRejected
		}
		rotation, ok := rotations[info.KeyClass]
		if !ok || rotation.Document.Epoch != info.Epoch {
			return peerstore.State{}, nil, errPeerJoinRejected
		}
		if _, duplicate := keys[info.KeyClass]; duplicate {
			return peerstore.State{}, nil, errPeerJoinRejected
		}
		key, err := configsync.OpenEpochKey(envelope, genesis.Document.SpaceID, identity.PeerID(), wrapping)
		if err != nil || configsync.ValidateEpochKeyForRotation(key, rotation) != nil {
			return peerstore.State{}, nil, errPeerJoinRejected
		}
		keys[info.KeyClass] = key
	}
	for _, class := range required {
		if _, ok := keys[class]; !ok {
			return peerstore.State{}, nil, errPeerJoinRejected
		}
	}
	return peerstore.State{
		GenesisToken: bootstrap.GenesisToken, LocalMembership: bootstrap.MembershipToken,
		Memberships:    append([]string(nil), bootstrap.Memberships...),
		Revocations:    append([]string(nil), bootstrap.Revocations...),
		EpochRotations: append([]string(nil), bootstrap.EpochRotations...),
		EpochEnvelopes: append([]string(nil), bootstrap.EpochEnvelopes...),
		CreatedAt:      now.Unix(),
	}, keys, nil
}

func validatePeerJoinGovernance(
	genesis peerproto.VerifiedGenesis,
	bootstrap quicktunnel.JoinBootstrap,
	local peerproto.VerifiedGrant,
	now time.Time,
) error {
	tokens := append([]string(nil), bootstrap.Memberships...)
	tokens = append(tokens, bootstrap.MembershipToken)
	bySerial := make(map[string]string, len(tokens))
	for _, token := range tokens {
		issued, err := peerproto.VerifyGrantAtIssuance(token, genesis)
		if err != nil {
			return err
		}
		if existing, ok := bySerial[issued.Document.Serial]; ok && existing != token {
			return peerstore.ErrMembershipFork
		}
		bySerial[issued.Document.Serial] = token
	}
	for _, token := range bootstrap.Revocations {
		if _, err := peerproto.VerifyRevocation(token, genesis); err != nil {
			return err
		}
	}
	active, err := peerJoinActiveMembershipsAt(genesis, bootstrap, now)
	if err != nil {
		return err
	}
	for _, membership := range active {
		if membership.Document.SubjectPeerID == local.Document.SubjectPeerID && membership.Token == local.Token {
			return nil
		}
	}
	return errPeerJoinRejected
}

func peerJoinActiveMembershipsAt(genesis peerproto.VerifiedGenesis, bootstrap quicktunnel.JoinBootstrap, at time.Time) ([]peerproto.VerifiedGrant, error) {
	tokens := append([]string(nil), bootstrap.Memberships...)
	tokens = append(tokens, bootstrap.MembershipToken)
	byPeer := make(map[string]peerproto.VerifiedGrant, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if _, duplicate := seen[token]; duplicate {
			continue
		}
		seen[token] = struct{}{}
		issued, err := peerproto.VerifyGrantAtIssuance(token, genesis)
		if err != nil {
			return nil, err
		}
		if issued.Document.IssuedAt > at.Unix() {
			continue
		}
		current, err := peerproto.VerifyGrant(token, genesis, at)
		if err != nil {
			if errors.Is(err, peerproto.ErrExpired) {
				continue
			}
			return nil, err
		}
		peerID := current.Document.SubjectPeerID
		if previous, ok := byPeer[peerID]; !ok || membershipIsCanonicalAfter(current, previous) {
			byPeer[peerID] = current
		}
	}
	revocations, err := peerproto.NewRevocationSet(genesis)
	if err != nil {
		return nil, err
	}
	for _, token := range bootstrap.Revocations {
		revocation, err := peerproto.VerifyRevocation(token, genesis)
		if err != nil {
			return nil, err
		}
		if revocation.Document.CreatedAt > at.Unix() {
			continue
		}
		if _, err := revocations.Apply(token); err != nil {
			return nil, err
		}
	}
	candidates := make([]peerproto.VerifiedGrant, 0, len(byPeer))
	for _, membership := range byPeer {
		candidates = append(candidates, membership)
	}
	return revocations.FilterActiveMemberships(candidates, at)
}

func clearPeerJoinEpochKeys(spaceID string, classes []configsync.KeyClass) {
	for _, class := range classes {
		_ = peerEpochKeySlot(spaceID, class).Clear()
	}
}
