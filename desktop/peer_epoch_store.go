package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
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
	syncKey, err := m.resolvePeerEpochKey(state, genesis, syncRotation, identity.PeerID(), wrapping)
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
		vaultKey, err := m.resolvePeerEpochKey(state, genesis, vaultRotation, identity.PeerID(), wrapping)
		if err != nil {
			return err
		}
		if err := savePeerEpochKey(genesis.Document.SpaceID, vaultKey); err != nil {
			return fmt.Errorf("save Peer Space vault epoch key: %w", err)
		}
	}
	return nil
}

func (m *peerSpaceManager) resolvePeerEpochKey(
	state peerstore.State,
	genesis peerproto.VerifiedGenesis,
	rotation configsync.VerifiedEpochRotation,
	peerID string,
	wrapping *peercrypto.WrappingIdentity,
) (configsync.EpochKey, error) {
	if stored, err := loadPeerEpochKey(genesis.Document.SpaceID, rotation.Document.KeyClass); err == nil {
		if configsync.ValidateEpochKeyForRotation(stored, rotation) == nil {
			return stored, nil
		}
	}
	if key, err := configsync.OpenRotationEpochKey(rotation, genesis, peerID, wrapping); err == nil {
		return key, nil
	}
	for _, envelope := range state.EpochEnvelopes {
		info, err := configsync.InspectEpochEnvelope(envelope)
		if err != nil || info.SpaceID != genesis.Document.SpaceID || info.KeyClass != rotation.Document.KeyClass || info.Epoch != rotation.Document.Epoch || info.RecipientPeerID != peerID {
			continue
		}
		key, err := configsync.OpenEpochKey(envelope, genesis.Document.SpaceID, peerID, wrapping)
		if err == nil && configsync.ValidateEpochKeyForRotation(key, rotation) == nil {
			return key, nil
		}
	}
	return configsync.EpochKey{}, errors.New("Peer Space epoch key is unavailable to this device")
}

func (m *peerSpaceManager) bootstrapEpochEnvelopes(state peerstore.State, genesis peerproto.VerifiedGenesis, member peerproto.VerifiedGrant) ([]string, error) {
	rotations, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		return nil, err
	}
	classes := []configsync.KeyClass{configsync.KeyClassSync}
	if member.Document.CanSyncSecrets {
		classes = append(classes, configsync.KeyClassVault)
	}
	envelopes := make([]string, 0, len(classes))
	for _, class := range classes {
		rotation, ok := rotations[class]
		if !ok {
			return nil, fmt.Errorf("Peer Space %s epoch rotation is missing", class)
		}
		key, err := loadPeerEpochKey(genesis.Document.SpaceID, class)
		if err != nil || configsync.ValidateEpochKeyForRotation(key, rotation) != nil {
			return nil, fmt.Errorf("load current Peer Space %s epoch key", class)
		}
		envelope, err := configsync.SealEpochKey(genesis.Document.SpaceID, key, configsync.EpochRecipient{
			PeerID: member.Document.SubjectPeerID, WrappingPublicKey: member.WrappingPublicKey,
			CanSyncSecrets: member.Document.CanSyncSecrets,
		})
		if err != nil {
			return nil, fmt.Errorf("seal Peer Space %s bootstrap epoch key: %w", class, err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
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
	stored, err := m.store.ApplyEpochRotations(tokens, now, func(state peerstore.State, newTokens []string) error {
		return authorizePeerEpochRotations(state, newTokens, now)
	})
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

func authorizePeerEpochRotations(state peerstore.State, newTokens []string, now time.Time) error {
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
