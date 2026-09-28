package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

// peerConfigReplica keeps the local signing authority and current encryption
// epochs next to the durable replica. It is never exposed to the frontend.
type peerConfigReplica struct {
	spaceID  string
	replica  *configsync.DurableReplica
	identity *peercrypto.Identity
	keys     map[configsync.KeyClass]configsync.EpochKey
}

func (m *peerSpaceManager) ensureConfigReplica() (*peerConfigReplica, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()

	state, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, fmt.Errorf("verify peer genesis for config replica: %w", err)
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return nil, err
	}
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, m.now())
	if err != nil {
		return nil, fmt.Errorf("verify local membership for config replica: %w", err)
	}
	if membership.Document.SubjectPeerID != identity.PeerID() {
		return nil, errors.New("peer identity does not own config replica membership")
	}

	keys := make(map[configsync.KeyClass]configsync.EpochKey, 2)
	syncKey, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		return nil, fmt.Errorf("load Peer Space sync epoch key: %w", err)
	}
	keys[configsync.KeyClassSync] = syncKey
	if membership.Document.CanSyncSecrets {
		vaultKey, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassVault)
		if err != nil {
			return nil, fmt.Errorf("load Peer Space vault epoch key: %w", err)
		}
		keys[configsync.KeyClassVault] = vaultKey
	}

	if current := m.configReplica; current != nil && current.spaceID == genesis.Document.SpaceID && samePeerEpochs(current.keys, keys) {
		return current, nil
	}
	if m.configRoot == "" {
		return nil, errors.New("Peer Space config replica root is missing")
	}
	path := filepath.Join(m.configRoot, genesis.Document.SpaceID, "config-replica.json")
	replica, err := configsync.OpenDurableReplica(path, genesis.Document.SpaceID, configsync.SchemaVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("open Peer Space config replica: %w", err)
	}
	runtime := &peerConfigReplica{
		spaceID: genesis.Document.SpaceID, replica: replica, identity: identity, keys: keys,
	}
	m.configReplica = runtime
	return runtime, nil
}

func samePeerEpochs(left, right map[configsync.KeyClass]configsync.EpochKey) bool {
	if len(left) != len(right) {
		return false
	}
	for class, leftKey := range left {
		rightKey, ok := right[class]
		if !ok || leftKey.Epoch != rightKey.Epoch || !bytes.Equal(leftKey.Bytes(), rightKey.Bytes()) {
			return false
		}
	}
	return true
}

func (m *peerSpaceManager) relayCompatibilityStore(realmID string) (*configsync.DurableRelayCompatibility, error) {
	runtime, err := m.ensureConfigReplica()
	if err != nil {
		return nil, err
	}
	if realmID == "" {
		return nil, configsync.ErrInvalidRelayState
	}
	digest := sha256.Sum256([]byte(realmID))
	name := base64.RawURLEncoding.EncodeToString(digest[:]) + ".json"
	path := filepath.Join(m.configRoot, runtime.spaceID, "relay-compat", name)
	return configsync.OpenDurableRelayCompatibility(path, realmID)
}

// restorePeerConfigReplica prepares an already-configured Space during boot.
// Peer state must never become a dependency of the local terminal or Relay.
func (a *App) restorePeerConfigReplica() {
	manager, err := a.peerManager()
	if err != nil {
		logWarn("peer-config", "initialize manager: %v", err)
		return
	}
	status, err := manager.readyStatus()
	if err != nil {
		logWarn("peer-config", "restore Peer Space: %v", err)
		return
	}
	if !status.Configured {
		return
	}
	if _, err := manager.ensureConfigReplica(); err != nil {
		logWarn("peer-config", "restore config replica: %v", err)
		return
	}
	seededRecords, seeded, err := a.bootstrapPeerConfig(manager)
	if err != nil {
		logWarn("peer-config", "bootstrap local config: %v", err)
		return
	}
	if seeded {
		logInfo("peer-config", "local config bootstrap complete (space=%s records=%d)", status.SpaceID, seededRecords)
	}
	logInfo("peer-config", "config replica ready (space=%s)", status.SpaceID)
}
