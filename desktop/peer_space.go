package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/quicktunnel"
)

const activePeerSpaceAccount = "active"

var errPeerIdentityMissing = errors.New("peer identity is missing")
var errPeerWrappingIdentityMissing = errors.New("peer wrapping identity is missing")

type PeerSpaceStatus struct {
	Configured      bool   `json:"configured"`
	PeerID          string `json:"peer_id,omitempty"`
	SpaceID         string `json:"space_id,omitempty"`
	GenesisHash     string `json:"genesis_hash,omitempty"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	OpenInvitations int    `json:"open_invitations"`
	UsedInvitations int    `json:"used_invitations"`
	RevokedInvites  int    `json:"revoked_invitations"`
	ExpiredInvites  int    `json:"expired_invitations"`
}

type CreatePeerInvitationsReq struct {
	Count             int      `json:"count"`
	ValidForHours     int      `json:"valid_for_hours"`
	Permission        string   `json:"permission"`
	AllowedSessionIDs []string `json:"allowed_session_ids"`
	CanInvite         bool     `json:"can_invite"`
	CanSyncSecrets    bool     `json:"can_sync_secrets"`
}

type PeerInvitation struct {
	InviteID         string `json:"invite_id"`
	BatchID          string `json:"batch_id"`
	Token            string `json:"token"`
	ExpiresAt        int64  `json:"expires_at"`
	ConsumedAt       int64  `json:"consumed_at,omitempty"`
	ConsumedByPeerID string `json:"consumed_by_peer_id,omitempty"`
	RevokedAt        int64  `json:"revoked_at,omitempty"`
}

type peerJoinResult struct {
	GenesisToken    string
	MembershipToken string
	Memberships     []string
	Revocations     []string
	EpochRotations  []string
	EpochEnvelopes  []string
}

type peerSpaceManager struct {
	store             *peerstore.Store
	bootstrapLockPath string
	configRoot        string
	now               func() time.Time
	joinQuickTunnel   func(context.Context, quicktunnel.JoinClientConfig) (quicktunnel.JoinBootstrap, error)

	configMu      sync.Mutex
	configReplica *peerConfigReplica
}

func peerIdentityService() string {
	return "com.atterm.peer-identity.v1" + appdir.KeychainSuffix()
}

func peerStoreKeyService() string {
	return "com.atterm.peer-store-key.v1" + appdir.KeychainSuffix()
}

func peerWrappingIdentityService() string {
	return "com.atterm.peer-wrapping-identity.v1" + appdir.KeychainSuffix()
}

func peerIdentitySlot() keychainSlot[[]byte] {
	return keychainSlot[[]byte]{service: peerIdentityService(), account: activePeerSpaceAccount, codec: bytesCodec}
}

func peerStoreKeySlot() keychainSlot[[]byte] {
	return keychainSlot[[]byte]{service: peerStoreKeyService(), account: activePeerSpaceAccount, codec: bytesCodec}
}

func peerWrappingIdentitySlot() keychainSlot[[]byte] {
	return keychainSlot[[]byte]{service: peerWrappingIdentityService(), account: activePeerSpaceAccount, codec: bytesCodec}
}

func newPeerSpaceManager() (*peerSpaceManager, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("peer space config directory: %w", err)
	}
	storePath := filepath.Join(dir, "peer-space.json")
	m := &peerSpaceManager{
		now:               time.Now,
		bootstrapLockPath: storePath + ".bootstrap.lock",
		configRoot:        filepath.Join(dir, "peer-spaces"),
	}
	m.store = peerstore.New(storePath, func() ([]byte, error) {
		key, err := peerStoreKeySlot().Load()
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, errors.New("peer space storage key is missing")
		}
		return key, nil
	})
	return m, nil
}

func (a *App) peerManager() (*peerSpaceManager, error) {
	a.peerSpaceMu.Lock()
	defer a.peerSpaceMu.Unlock()
	if a.peerSpace != nil {
		return a.peerSpace, nil
	}
	manager, err := newPeerSpaceManager()
	if err != nil {
		return nil, err
	}
	a.peerSpace = manager
	return manager, nil
}

// GetPeerSpaceStatus reports only non-secret identity and invitation counts.
func (a *App) GetPeerSpaceStatus() (PeerSpaceStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	return manager.readyStatus()
}

func (m *peerSpaceManager) readyStatus() (PeerSpaceStatus, error) {
	release, err := m.acquireBootstrapLock()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	defer release()
	status, err := m.status()
	if err != nil || !status.Configured {
		return status, err
	}
	if err := m.ensureInitialEpochState(); err != nil {
		return PeerSpaceStatus{}, err
	}
	return m.status()
}

// CreatePeerSpace creates the one active Peer Space for this installation.
// Repeated calls return the existing status and never rotate identity.
func (a *App) CreatePeerSpace() (PeerSpaceStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	status, err := manager.createSpace()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	if _, _, err := a.bootstrapPeerConfig(manager); err != nil {
		return status, err
	}
	if a.cfgStore != nil {
		a.reconcilePeerRendezvous(a.cfgStore.Get())
		a.reconcilePeerLAN(a.cfgStore.Get())
	}
	return status, nil
}

func (m *peerSpaceManager) createSpace() (PeerSpaceStatus, error) {
	release, err := m.acquireBootstrapLock()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	defer release()

	if status, err := m.status(); err == nil && status.Configured {
		if err := m.ensureInitialEpochState(); err != nil {
			return PeerSpaceStatus{}, err
		}
		if _, err := m.ensureConfigReplica(); err != nil {
			return PeerSpaceStatus{}, err
		}
		return status, nil
	} else if err != nil && !errors.Is(err, peerstore.ErrNotInitialized) {
		return PeerSpaceStatus{}, err
	}
	identity, err := m.ensureIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	wrappingIdentity, err := m.ensureWrappingIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	if err := m.ensureStoreKey(); err != nil {
		return PeerSpaceStatus{}, err
	}
	now := m.now()
	genesis, membership, err := peerproto.NewSpace(identity, wrappingIdentity.PublicBytes(), now)
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	if err := m.store.Initialize(peerstore.State{
		GenesisToken:    genesis,
		LocalMembership: membership,
		CreatedAt:       now.Unix(),
	}); err != nil && !errors.Is(err, peerstore.ErrAlreadyExists) {
		return PeerSpaceStatus{}, err
	}
	if err := m.ensureInitialEpochState(); err != nil {
		return PeerSpaceStatus{}, err
	}
	if _, err := m.ensureConfigReplica(); err != nil {
		return PeerSpaceStatus{}, err
	}
	return m.status()
}

// CreatePeerInvitations pre-signs route-independent, one-use tickets.
func (a *App) CreatePeerInvitations(req CreatePeerInvitationsReq) ([]PeerInvitation, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	return manager.createInvitations(req)
}

func (a *App) ListPeerInvitations() ([]PeerInvitation, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	return publicInvitations(state.Invitations, manager.now().Unix()), nil
}

func (a *App) RevokePeerInvitation(inviteID string) error {
	manager, err := a.peerManager()
	if err != nil {
		return err
	}
	return manager.store.RevokeInvitation(inviteID, manager.now())
}

func (a *App) RevokePeerInvitationBatch(batchID string) error {
	manager, err := a.peerManager()
	if err != nil {
		return err
	}
	return manager.revokeInvitationBatch(batchID)
}

func (m *peerSpaceManager) revokeInvitationBatch(batchID string) error {
	state, err := m.store.Load()
	if err != nil {
		return err
	}
	found := false
	hasOpenInvitation := false
	for _, invitation := range state.Invitations {
		if invitation.BatchID == batchID {
			found = true
			if invitation.ConsumedAt == 0 && invitation.RevokedAt == 0 {
				hasOpenInvitation = true
			}
		}
	}
	if !found {
		return peerstore.ErrInviteInvalid
	}
	if !hasOpenInvitation {
		return nil
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return err
	}
	now := m.now()
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		return err
	}
	revocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationInvitationBatch, batchID, now)
	if err != nil {
		return err
	}
	_, err = m.store.ApplyRevocations([]string{revocation.Token}, now)
	return err
}

func (m *peerSpaceManager) status() (PeerSpaceStatus, error) {
	state, err := m.store.Load()
	if errors.Is(err, peerstore.ErrNotInitialized) {
		return PeerSpaceStatus{Configured: false}, nil
	}
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("verify peer genesis: %w", err)
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	wrappingIdentity, err := m.loadWrappingIdentity()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, m.now())
	if err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("verify local membership: %w", err)
	}
	if membership.Document.SubjectPeerID != identity.PeerID() {
		return PeerSpaceStatus{}, errors.New("peer identity does not own local membership")
	}
	if !bytes.Equal(membership.WrappingPublicKey, wrappingIdentity.PublicBytes()) {
		return PeerSpaceStatus{}, errors.New("peer wrapping identity does not own local membership")
	}
	status := PeerSpaceStatus{
		Configured:  true,
		PeerID:      identity.PeerID(),
		SpaceID:     genesis.Document.SpaceID,
		GenesisHash: genesis.Hash,
		CreatedAt:   state.CreatedAt,
	}
	now := m.now().Unix()
	for _, invite := range state.Invitations {
		switch {
		case invite.RevokedAt != 0:
			status.RevokedInvites++
		case invite.ConsumedAt != 0:
			status.UsedInvitations++
		case now >= invite.ExpiresAt:
			status.ExpiredInvites++
		default:
			status.OpenInvitations++
		}
	}
	return status, nil
}

func (m *peerSpaceManager) createInvitations(req CreatePeerInvitationsReq) ([]PeerInvitation, error) {
	state, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	now := m.now()
	if req.ValidForHours < 0 || req.ValidForHours > 30*24 {
		return nil, errors.New("peer invitation validity must be 0 (default) or 1..720 hours")
	}
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		return nil, err
	}
	validFor := time.Duration(req.ValidForHours) * time.Hour
	tokens, err := peerproto.NewInvitationBatch(identity, genesis, membership, now, peerproto.InvitationOptions{
		Count:             req.Count,
		ValidFor:          validFor,
		Permission:        peerproto.Permission(req.Permission),
		AllowedSessionIDs: req.AllowedSessionIDs,
		CanInvite:         req.CanInvite,
		CanSyncSecrets:    req.CanSyncSecrets,
	})
	if err != nil {
		return nil, err
	}
	records := make([]peerstore.Invitation, 0, len(tokens))
	for _, token := range tokens {
		doc, _, err := peerproto.VerifyInvitation(token, genesis, now)
		if err != nil {
			return nil, err
		}
		records = append(records, peerstore.Invitation{
			InviteID: doc.InviteID, BatchID: doc.BatchID, Token: token, ExpiresAt: doc.ExpiresAt,
		})
	}
	if err := m.store.AddInvitations(records, now); err != nil {
		return nil, err
	}
	return publicInvitations(records, now.Unix()), nil
}

// redeemJoinRequest is the transport-independent authorization core used by
// future Quick Tunnel and Rendezvous gateways. It deliberately has no route or
// Relay inputs.
func (m *peerSpaceManager) redeemJoinRequest(requestToken string) (peerJoinResult, error) {
	state, err := m.store.Load()
	if err != nil {
		return peerJoinResult{}, err
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return peerJoinResult{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return peerJoinResult{}, fmt.Errorf("verify peer genesis: %w", err)
	}
	now := m.now()
	issuer, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		return peerJoinResult{}, fmt.Errorf("verify local membership: %w", err)
	}
	if issuer.Document.SubjectPeerID != identity.PeerID() {
		return peerJoinResult{}, errors.New("peer identity does not own local membership")
	}
	join, err := peerproto.VerifyJoinRequest(requestToken, genesis, now)
	if err != nil {
		return peerJoinResult{}, fmt.Errorf("verify peer join request: %w", err)
	}
	if join.Ticket.RedemptionPeerID != identity.PeerID() {
		return peerJoinResult{}, errors.New("peer invitation belongs to a different redemption device")
	}
	issued, err := peerproto.IssueMembership(identity, genesis, issuer, join, now)
	if err != nil {
		return peerJoinResult{}, fmt.Errorf("issue peer membership: %w", err)
	}
	membership, err := m.store.RedeemInvitation(join.Ticket.InviteID, join.Document.SubjectPeerID, issued, now)
	if err != nil {
		return peerJoinResult{}, err
	}
	state, err = m.store.Load()
	if err != nil {
		return peerJoinResult{}, err
	}
	joined, err := peerproto.VerifyGrant(membership, genesis, now)
	if err != nil {
		return peerJoinResult{}, fmt.Errorf("verify issued peer membership: %w", err)
	}
	envelopes, err := m.bootstrapEpochEnvelopes(state, genesis, joined)
	if err != nil {
		return peerJoinResult{}, err
	}
	return peerJoinResult{
		GenesisToken: state.GenesisToken, MembershipToken: membership,
		Memberships:    append([]string(nil), state.Memberships...),
		Revocations:    append([]string(nil), state.Revocations...),
		EpochRotations: append([]string(nil), state.EpochRotations...),
		EpochEnvelopes: envelopes,
	}, nil
}

func (m *peerSpaceManager) invitationPairingSecret(inviteID string) ([]byte, error) {
	state, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	for _, invitation := range state.Invitations {
		if invitation.InviteID != inviteID {
			continue
		}
		ticket, _, err := peerproto.VerifyInvitation(invitation.Token, genesis, m.now())
		if err != nil || ticket.InviteID != inviteID || invitation.RevokedAt != 0 {
			return nil, peerstore.ErrInviteInvalid
		}
		secret, err := base64.RawURLEncoding.Strict().DecodeString(ticket.PairingSecret)
		if err != nil || len(secret) != 32 {
			return nil, peerstore.ErrInviteInvalid
		}
		return secret, nil
	}
	return nil, peerstore.ErrInviteInvalid
}

func (m *peerSpaceManager) loadIdentity() (*peercrypto.Identity, error) {
	raw, err := peerIdentitySlot().Load()
	if err != nil {
		return nil, fmt.Errorf("load peer identity: %w", err)
	}
	if len(raw) == 0 {
		return nil, errPeerIdentityMissing
	}
	identity, err := peercrypto.ParseIdentity(raw)
	if err != nil {
		return nil, fmt.Errorf("parse peer identity: %w", err)
	}
	return identity, nil
}

func (m *peerSpaceManager) ensureIdentity() (*peercrypto.Identity, error) {
	identity, err := m.loadIdentity()
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, errPeerIdentityMissing) {
		return nil, err
	}
	identity, err = peercrypto.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	if err := peerIdentitySlot().Save(identity.PrivateBytes()); err != nil {
		return nil, fmt.Errorf("save peer identity: %w", err)
	}
	return identity, nil
}

func (m *peerSpaceManager) loadWrappingIdentity() (*peercrypto.WrappingIdentity, error) {
	raw, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		return nil, fmt.Errorf("load peer wrapping identity: %w", err)
	}
	if len(raw) == 0 {
		return nil, errPeerWrappingIdentityMissing
	}
	identity, err := peercrypto.ParseWrappingIdentity(raw)
	if err != nil {
		return nil, fmt.Errorf("parse peer wrapping identity: %w", err)
	}
	return identity, nil
}

func (m *peerSpaceManager) ensureWrappingIdentity() (*peercrypto.WrappingIdentity, error) {
	identity, err := m.loadWrappingIdentity()
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, errPeerWrappingIdentityMissing) {
		return nil, err
	}
	identity, err = peercrypto.GenerateWrappingIdentity()
	if err != nil {
		return nil, err
	}
	if err := peerWrappingIdentitySlot().Save(identity.PrivateBytes()); err != nil {
		return nil, fmt.Errorf("save peer wrapping identity: %w", err)
	}
	return identity, nil
}

func (m *peerSpaceManager) ensureStoreKey() error {
	key, err := peerStoreKeySlot().Load()
	if err != nil {
		return fmt.Errorf("load peer store key: %w", err)
	}
	if len(key) == 32 {
		return nil
	}
	if len(key) != 0 {
		return fmt.Errorf("peer store key has invalid length %d", len(key))
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate peer store key: %w", err)
	}
	if err := peerStoreKeySlot().Save(key); err != nil {
		return fmt.Errorf("save peer store key: %w", err)
	}
	return nil
}

func (m *peerSpaceManager) acquireBootstrapLock() (func(), error) {
	if m.bootstrapLockPath == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(m.bootstrapLockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create peer bootstrap directory: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := os.Mkdir(m.bootstrapLockPath, 0o700); err == nil {
			return func() { _ = os.Remove(m.bootstrapLockPath) }, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire peer bootstrap lock: %w", err)
		}
		if info, err := os.Stat(m.bootstrapLockPath); err == nil && time.Since(info.ModTime()) > 2*time.Minute {
			_ = os.Remove(m.bootstrapLockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for peer space bootstrap")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func publicInvitations(records []peerstore.Invitation, now int64) []PeerInvitation {
	out := make([]PeerInvitation, 0, len(records))
	for _, invite := range records {
		token := invite.Token
		if invite.ConsumedAt != 0 || invite.RevokedAt != 0 || now >= invite.ExpiresAt {
			token = ""
		}
		out = append(out, PeerInvitation{
			InviteID: invite.InviteID, BatchID: invite.BatchID, Token: token,
			ExpiresAt: invite.ExpiresAt, ConsumedAt: invite.ConsumedAt,
			ConsumedByPeerID: invite.ConsumedByPeerID, RevokedAt: invite.RevokedAt,
		})
	}
	return out
}
