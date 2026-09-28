package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
)

type peerEpochKeyRecord struct {
	Epoch uint64 `json:"epoch"`
	Key   []byte `json:"key"`
}

func peerEpochKeyService() string {
	return "com.atterm.peer-epoch-key.v1" + appdir.KeychainSuffix()
}

func peerEpochKeySlot(spaceID string, class configsync.KeyClass) keychainSlot[peerEpochKeyRecord] {
	return keychainSlot[peerEpochKeyRecord]{
		service: peerEpochKeyService(), account: spaceID + "|" + string(class),
		codec: jsonCodec[peerEpochKeyRecord](func(record peerEpochKeyRecord) bool {
			return record.Epoch == 0 || len(record.Key) == 0
		}),
	}
}

func loadPeerEpochKey(spaceID string, class configsync.KeyClass) (configsync.EpochKey, error) {
	record, err := peerEpochKeySlot(spaceID, class).Load()
	if err != nil {
		return configsync.EpochKey{}, err
	}
	if record.Epoch == 0 && len(record.Key) == 0 {
		return configsync.EpochKey{}, configsync.ErrInvalidEpochKey
	}
	return configsync.ParseEpochKey(class, record.Epoch, record.Key)
}

func savePeerEpochKey(spaceID string, key configsync.EpochKey) error {
	if len(key.Bytes()) != configsync.EpochKeySize {
		return configsync.ErrInvalidEpochKey
	}
	return peerEpochKeySlot(spaceID, key.Class).Save(peerEpochKeyRecord{Epoch: key.Epoch, Key: key.Bytes()})
}

// ensureInitialEpochState migrates old Spaces and initializes new ones. The
// caller holds peerSpaceManager's cross-process bootstrap lock.
func (m *peerSpaceManager) ensureInitialEpochState() error {
	state, err := m.store.Load()
	if err != nil {
		return err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return fmt.Errorf("verify peer genesis for epochs: %w", err)
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return err
	}
	wrapping, err := m.loadWrappingIdentity()
	if err != nil {
		return err
	}
	now := m.now()
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil || membership.Document.SubjectPeerID != identity.PeerID() || !bytes.Equal(membership.WrappingPublicKey, wrapping.PublicBytes()) {
		return errors.New("local identity does not own Peer Space epoch recipient")
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return err
	}
	if len(state.EpochRotations) == 0 {
		syncKey, err := configsync.GenerateEpochKey(configsync.KeyClassSync, 1)
		if err != nil {
			return err
		}
		vaultKey, err := configsync.GenerateEpochKey(configsync.KeyClassVault, 1)
		if err != nil {
			return err
		}
		syncRotation, err := configsync.NewEpochRotation(identity, genesis, membership, nil, syncKey, active, now)
		if err != nil {
			return err
		}
		vaultRotation, err := configsync.NewEpochRotation(identity, genesis, membership, nil, vaultKey, active, now)
		if err != nil {
			return err
		}
		if err := m.store.InitializeEpochRotations([]string{syncRotation.Token, vaultRotation.Token}, now); err != nil && !errors.Is(err, peerstore.ErrAlreadyExists) {
			return err
		}
		state, err = m.store.Load()
		if err != nil {
			return err
		}
	}

	rotations, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		return err
	}
	syncRotation, ok := rotations[configsync.KeyClassSync]
	if !ok {
		return errors.New("Peer Space sync epoch rotation is missing")
	}
	syncKey, err := configsync.OpenRotationEpochKey(syncRotation, genesis, identity.PeerID(), wrapping)
	if err != nil {
		return err
	}
	if err := savePeerEpochKey(genesis.Document.SpaceID, syncKey); err != nil {
		return fmt.Errorf("save Peer Space sync epoch key: %w", err)
	}
	if membership.Document.CanSyncSecrets {
		vaultRotation, ok := rotations[configsync.KeyClassVault]
		if !ok {
			return errors.New("Peer Space vault epoch rotation is missing")
		}
		vaultKey, err := configsync.OpenRotationEpochKey(vaultRotation, genesis, identity.PeerID(), wrapping)
		if err != nil {
			return err
		}
		if err := savePeerEpochKey(genesis.Document.SpaceID, vaultKey); err != nil {
			return fmt.Errorf("save Peer Space vault epoch key: %w", err)
		}
	}
	return nil
}

func activePeerMemberships(state peerstore.State, genesis peerproto.VerifiedGenesis, now time.Time) ([]peerproto.VerifiedGrant, error) {
	tokens := append([]string(nil), state.Memberships...)
	tokens = append(tokens, state.LocalMembership)
	for _, invitation := range state.Invitations {
		if invitation.IssuedMembership != "" {
			tokens = append(tokens, invitation.IssuedMembership)
		}
	}
	byPeer := make(map[string]peerproto.VerifiedGrant, len(tokens))
	seenTokens := make(map[string]struct{}, len(tokens))
	for len(tokens) != 0 {
		token := tokens[0]
		tokens = tokens[1:]
		if _, duplicate := seenTokens[token]; duplicate {
			continue
		}
		seenTokens[token] = struct{}{}
		membership, err := peerproto.VerifyGrant(token, genesis, now)
		if err != nil {
			if errors.Is(err, peerproto.ErrExpired) {
				continue
			}
			return nil, fmt.Errorf("verify Peer Space member for epochs: %w", err)
		}
		peerID := membership.Document.SubjectPeerID
		if current, exists := byPeer[peerID]; !exists || membershipIsCanonicalAfter(membership, current) {
			byPeer[peerID] = membership
		}
		if membership.Document.IssuerMembership != "" {
			tokens = append(tokens, membership.Document.IssuerMembership)
		}
	}
	memberships := make([]peerproto.VerifiedGrant, 0, len(byPeer))
	for _, membership := range byPeer {
		memberships = append(memberships, membership)
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
	return revocations.FilterActiveMemberships(memberships, now)
}

func membershipIsCanonicalAfter(candidate, current peerproto.VerifiedGrant) bool {
	if candidate.Document.IssuedAt != current.Document.IssuedAt {
		return candidate.Document.IssuedAt > current.Document.IssuedAt
	}
	if candidate.Document.Serial != current.Document.Serial {
		return candidate.Document.Serial < current.Document.Serial
	}
	return candidate.Token < current.Token
}

func (m *peerSpaceManager) applyPeerEpochRotations(tokens []string) (bool, error) {
	if len(tokens) == 0 {
		return false, nil
	}
	now := m.now()
	authorize := func(state peerstore.State, newTokens []string) error {
		genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
		if err != nil {
			return err
		}
		active, err := activePeerMemberships(state, genesis, now)
		if err != nil {
			return err
		}
		resolvers := make(map[configsync.KeyClass]*configsync.EpochRotationResolver, 2)
		for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
			resolver, err := configsync.NewEpochRotationResolver(genesis, class, func(configsync.VerifiedEpochRotation) error { return nil })
			if err != nil {
				return err
			}
			resolvers[class] = resolver
		}
		for _, token := range state.EpochRotations {
			rotation, err := configsync.VerifyEpochRotation(token, genesis)
			if err != nil {
				return err
			}
			if _, err := resolvers[rotation.Document.KeyClass].Apply(token); err != nil {
				return err
			}
		}
		for _, token := range newTokens {
			rotation, err := configsync.VerifyEpochRotation(token, genesis)
			if err != nil {
				return err
			}
			if err := configsync.AuthorizeEpochRotation(rotation, genesis, active, now); err != nil {
				return err
			}
			if _, err := resolvers[rotation.Document.KeyClass].Apply(token); err != nil {
				return err
			}
		}
		return nil
	}
	stored, err := m.store.ApplyEpochRotations(tokens, now, authorize)
	if err != nil {
		return false, err
	}
	if err := m.ensureInitialEpochState(); err != nil {
		return false, err
	}
	m.configMu.Lock()
	m.configReplica = nil
	m.configMu.Unlock()
	return stored != 0, nil
}

func currentEpochRotations(tokens []string, genesis peerproto.VerifiedGenesis) (map[configsync.KeyClass]configsync.VerifiedEpochRotation, error) {
	resolvers := make(map[configsync.KeyClass]*configsync.EpochRotationResolver, 2)
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		// Tokens are authorized against membership/revocation state before
		// entering peerstore. Rebuild still verifies every exact signature and
		// DAG edge; re-running recipient authorization against today's member
		// set would incorrectly reject historical rotations after membership
		// changes.
		resolver, err := configsync.NewEpochRotationResolver(genesis, class, func(configsync.VerifiedEpochRotation) error { return nil })
		if err != nil {
			return nil, err
		}
		resolvers[class] = resolver
	}
	for _, token := range tokens {
		rotation, err := configsync.VerifyEpochRotation(token, genesis)
		if err != nil {
			return nil, errors.New("Peer Space epoch rotation is invalid")
		}
		resolver := resolvers[rotation.Document.KeyClass]
		if resolver == nil {
			return nil, errors.New("Peer Space epoch rotation class is invalid")
		}
		if _, err := resolver.Apply(token); err != nil {
			return nil, err
		}
	}
	rotations := make(map[configsync.KeyClass]configsync.VerifiedEpochRotation, 2)
	for class, resolver := range resolvers {
		if current, ok := resolver.Current(); ok {
			rotations[class] = current
		}
	}
	return rotations, nil
}
