package main

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
)

var errCannotRevokeLocalPeer = errors.New("cannot revoke the local Peer member")

// PeerMember is the non-secret directory projection exposed to Settings.
type PeerMember struct {
	PeerID            string   `json:"peer_id"`
	GrantSerial       string   `json:"grant_serial"`
	IssuerPeerID      string   `json:"issuer_peer_id"`
	Permission        string   `json:"permission"`
	AllowedSessionIDs []string `json:"allowed_session_ids"`
	CanInvite         bool     `json:"can_invite"`
	CanSyncSecrets    bool     `json:"can_sync_secrets"`
	IssuedAt          int64    `json:"issued_at"`
	ExpiresAt         int64    `json:"expires_at,omitempty"`
	RevokedAt         int64    `json:"revoked_at,omitempty"`
	Status            string   `json:"status"`
	Local             bool     `json:"local"`
	CanRevoke         bool     `json:"can_revoke"`
	LastExchangeAt    int64    `json:"last_exchange_at,omitempty"`
}

// ListPeerMembers returns signed grant metadata without returning any grant,
// revocation, or epoch token to the renderer.
func (a *App) ListPeerMembers() ([]PeerMember, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	return manager.listMembers()
}

// RevokePeerMember permanently removes a remote member and rotates both key
// classes before the governance mutation becomes visible.
func (a *App) RevokePeerMember(peerID string) error {
	manager, err := a.peerManager()
	if err != nil {
		return err
	}
	if err := manager.revokeMember(peerID); err != nil {
		return err
	}
	a.revalidatePeerQuickTunnelAttempts()
	if a.cfgStore != nil {
		a.reconcilePeerRendezvous(a.cfgStore.Get())
	}
	return nil
}

func (m *peerSpaceManager) listMembers() ([]PeerMember, error) {
	state, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return nil, err
	}
	now := m.now()
	canonical, err := canonicalPeerMemberships(state, genesis)
	if err != nil {
		return nil, err
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return nil, err
	}
	activeByPeer := make(map[string]string, len(active))
	for _, membership := range active {
		activeByPeer[membership.Document.SubjectPeerID] = membership.Token
	}
	revocations, err := peerproto.NewRevocationSet(genesis)
	if err != nil {
		return nil, err
	}
	for _, token := range state.Revocations {
		if _, err := revocations.Apply(token); err != nil {
			return nil, err
		}
	}
	local := membershipForPeerID(active, identity.PeerID())
	localCanRevoke := local != nil && local.Document.Permission == peerproto.PermissionFull && local.Document.CanInvite
	members := make([]PeerMember, 0, len(canonical))
	for _, membership := range canonical {
		doc := membership.Document
		member := PeerMember{
			PeerID: doc.SubjectPeerID, GrantSerial: doc.Serial, IssuerPeerID: doc.IssuerPeerID,
			Permission: string(doc.Permission), AllowedSessionIDs: append([]string(nil), doc.AllowedSessionIDs...),
			CanInvite: doc.CanInvite, CanSyncSecrets: doc.CanSyncSecrets,
			IssuedAt: doc.IssuedAt, ExpiresAt: doc.ExpiresAt,
			Local: doc.SubjectPeerID == identity.PeerID(),
		}
		if revokedAt, revoked := revocations.MemberRevoked(doc.SubjectPeerID); revoked {
			member.Status, member.RevokedAt = "revoked", revokedAt
		} else if revokedAt, revoked := revocations.GrantRevoked(doc.Serial); revoked {
			member.Status, member.RevokedAt = "revoked", revokedAt
		} else if activeByPeer[doc.SubjectPeerID] == membership.Token {
			member.Status = "active"
		} else {
			member.Status = "expired"
		}
		member.CanRevoke = localCanRevoke && !member.Local && member.Status == "active"
		if exchange, ok := state.ConfigSyncPeers[doc.SubjectPeerID]; ok {
			member.LastExchangeAt = exchange.LastExchangeAt
		}
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Local != members[j].Local {
			return members[i].Local
		}
		return members[i].PeerID < members[j].PeerID
	})
	return members, nil
}

func canonicalPeerMemberships(state peerstore.State, genesis peerproto.VerifiedGenesis) ([]peerproto.VerifiedGrant, error) {
	tokens := append([]string(nil), state.Memberships...)
	tokens = append(tokens, state.LocalMembership)
	byPeer := make(map[string]peerproto.VerifiedGrant, len(tokens))
	for _, token := range tokens {
		membership, err := peerproto.VerifyGrantAtIssuance(token, genesis)
		if err != nil {
			return nil, fmt.Errorf("verify Peer Space member directory: %w", err)
		}
		peerID := membership.Document.SubjectPeerID
		if current, exists := byPeer[peerID]; !exists || membershipIsCanonicalAfter(membership, current) {
			byPeer[peerID] = membership
		}
	}
	memberships := make([]peerproto.VerifiedGrant, 0, len(byPeer))
	for _, membership := range byPeer {
		memberships = append(memberships, membership)
	}
	return memberships, nil
}

func (m *peerSpaceManager) revokeMember(peerID string) error {
	state, err := m.store.Load()
	if err != nil {
		return err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return err
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return err
	}
	if peerID == identity.PeerID() {
		return errCannotRevokeLocalPeer
	}
	now := m.now()
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return err
	}
	actor := membershipForPeerID(active, identity.PeerID())
	target := membershipForPeerID(active, peerID)
	if target == nil {
		if state.RevokedMembers[peerID] != 0 {
			return nil
		}
		return errors.New("Peer member is not active")
	}
	if actor == nil {
		return errors.New("local Peer membership is not active")
	}
	revocation, err := peerproto.NewRevocation(identity, genesis, *actor, peerproto.RevocationMember, peerID, now)
	if err != nil {
		return err
	}
	_, _, err = m.store.ApplyGovernanceChange([]string{revocation.Token}, now, func(updated peerstore.State) ([]string, error) {
		return m.buildEpochRotations(updated, identity.PeerID(), now)
	})
	if err != nil {
		return err
	}
	if err := m.ensureInitialEpochState(); err != nil {
		return err
	}
	m.configMu.Lock()
	m.configReplica = nil
	m.configMu.Unlock()
	return nil
}

func (m *peerSpaceManager) buildEpochRotations(state peerstore.State, actorPeerID string, now time.Time) ([]string, error) {
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return nil, err
	}
	if identity.PeerID() != actorPeerID {
		return nil, errors.New("Peer epoch actor identity changed")
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return nil, err
	}
	actor := membershipForPeerID(active, actorPeerID)
	if actor == nil {
		return nil, errors.New("local Peer membership is not active")
	}
	current, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		return nil, err
	}
	tokens := make([]string, 0, 2)
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		previous, ok := current[class]
		if !ok {
			return nil, fmt.Errorf("Peer Space %s epoch rotation is missing", class)
		}
		key, err := configsync.GenerateEpochKey(class, previous.Document.Epoch+1)
		if err != nil {
			return nil, err
		}
		rotation, err := configsync.NewEpochRotation(identity, genesis, *actor, &previous, key, active, now)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, rotation.Token)
	}
	if err := authorizePeerEpochRotations(state, tokens, now); err != nil {
		return nil, err
	}
	return tokens, nil
}
