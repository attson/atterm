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
	manager  *peerSpaceManager
}

const (
	peerConfigCompactTailOperations = 512
	peerConfigCompactTailBytes      = 8 << 20
)

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
		spaceID: genesis.Document.SpaceID, replica: replica, identity: identity, keys: keys, manager: m,
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

func (r *peerConfigReplica) materializeRelayValues(codec configsync.SealedRelayCodec) ([]configsync.RelayValue, []error) {
	return r.materializeRelayValuesMatching(codec, func(string) bool { return true })
}

func (r *peerConfigReplica) materializeRelayValuesMatching(codec configsync.SealedRelayCodec, include func(string) bool) ([]configsync.RelayValue, []error) {
	resolve := func(class configsync.KeyClass, epoch uint64) (configsync.EpochKey, bool) {
		key, ok := r.keys[class]
		return key, ok && key.Epoch == epoch
	}
	values := make([]configsync.RelayValue, 0, len(configsync.RelayKeySpecs()))
	var errs []error
	for _, spec := range configsync.RelayKeySpecs() {
		if include != nil && !include(spec.Key) {
			continue
		}
		var records []configsync.Record
		for _, collection := range spec.Collections {
			records = append(records, r.replica.Records(collection)...)
		}
		value, ok, err := configsync.MaterializeRelayValue(spec.Key, records, resolve, codec)
		if err != nil {
			errs = append(errs, fmt.Errorf("materialize Relay key %s: %w", spec.Key, err))
			continue
		}
		if ok {
			values = append(values, value)
		}
	}
	return values, errs
}

// planRelayExports records retryable export hashes for canonical values that
// can be represented losslessly by the legacy Relay schema. SSH stays out
// until its Relay-only secrets can be carried alongside Peer metadata.
func (r *peerConfigReplica) planRelayExports(store *configsync.DurableRelayCompatibility, codec configsync.SealedRelayCodec) ([]configsync.RelayValue, []error, error) {
	if store == nil {
		return nil, nil, configsync.ErrInvalidRelayState
	}
	values, materializeErrors := r.materializeRelayValuesMatching(codec, func(key string) bool {
		return key != "ssh_hosts_encrypted"
	})
	var exports []configsync.RelayValue
	var planErrors []error
	_, err := store.Update(func(compatibility *configsync.RelayCompatibility) error {
		exports, planErrors = compatibility.PlanExports(values)
		return nil
	})
	errs := append(materializeErrors, planErrors...)
	if err != nil {
		return nil, errs, err
	}
	return exports, errs, nil
}

// appendLocalRelayKey records a local whole-preference edit as the minimal
// canonical mutation batch. It is intentionally not called by setters until
// canonical materialization can replace the existing Relay sync writer.
func (r *peerConfigReplica) appendLocalRelayKey(cfg appConfig, relayKey string) (int, error) {
	spec, ok := configsync.RelaySpec(relayKey)
	if !ok {
		return 0, fmt.Errorf("unknown Peer config key %q", relayKey)
	}
	current, err := peerLocalRecordsForKey(cfg, relayKey)
	if err != nil {
		return 0, err
	}
	collections, err := peerLocalCollectionsForKey(relayKey)
	if err != nil {
		return 0, err
	}

	previous := make([]configsync.PlainRecord, 0)
	for _, collection := range collections {
		for _, record := range r.replica.Records(collection) {
			if spec.Mode == configsync.RelayScalar && record.RecordID != relayKey {
				continue
			}
			if record.Deleted {
				continue
			}
			key, ok := r.keys[record.KeyClass]
			if !ok {
				continue
			}
			if key.Epoch != record.KeyEpoch {
				return 0, fmt.Errorf("open Peer config %s/%s: %w: epoch %d unavailable", record.Collection, record.RecordID, configsync.ErrInvalidEpochKey, record.KeyEpoch)
			}
			plain, err := configsync.OpenPlainRecord(record, key)
			if err != nil {
				return 0, fmt.Errorf("open Peer config %s/%s: %w", record.Collection, record.RecordID, err)
			}
			previous = append(previous, plain)
		}
	}

	writable := current[:0]
	for _, record := range current {
		if _, ok := r.keys[record.KeyClass]; ok {
			writable = append(writable, record)
		}
	}
	mutations, err := configsync.PlanRecordReplacement(writable, previous)
	if err != nil {
		return 0, fmt.Errorf("plan local Peer config %s: %w", relayKey, err)
	}
	if len(mutations) == 0 {
		return 0, nil
	}
	return r.appendPeerConfigMutations("local Peer config "+relayKey, mutations)
}

// appendRelayValue imports one legacy Relay winner into the canonical
// replica. SSH credentials and private keys deliberately remain outside Peer
// sync until a separate opt-in policy and lossless Relay sidecar exist.
func (r *peerConfigReplica) appendRelayValue(value configsync.RelayValue, previous []configsync.RecordRef, codec configsync.SealedRelayCodec) ([]configsync.RecordRef, int, error) {
	filteredPrevious := make([]configsync.RecordRef, 0, len(previous))
	for _, ref := range previous {
		if r.canImportRelayRecord(value.Key, ref.Collection, ref.KeyClass) {
			filteredPrevious = append(filteredPrevious, ref)
		}
	}
	mutations, refs, err := configsync.PlanRelayRecordImport(value, filteredPrevious, codec)
	if err != nil {
		return nil, 0, err
	}
	filteredMutations := make([]configsync.RecordMutation, 0, len(mutations))
	for _, mutation := range mutations {
		if r.canImportRelayRecord(value.Key, mutation.Collection, mutation.KeyClass) {
			filteredMutations = append(filteredMutations, mutation)
		}
	}
	filteredRefs := make([]configsync.RecordRef, 0, len(refs))
	for _, ref := range refs {
		if r.canImportRelayRecord(value.Key, ref.Collection, ref.KeyClass) {
			filteredRefs = append(filteredRefs, ref)
		}
	}
	operations, err := r.appendPeerConfigMutations("Relay import "+value.Key, filteredMutations)
	if err != nil {
		return nil, 0, err
	}
	return filteredRefs, operations, nil
}

// importRelayValues persists compatibility hashes only after each accepted
// Relay value has reached the durable canonical replica. Per-key validation
// failures do not roll back successful siblings in the same Relay response.
func (r *peerConfigReplica) importRelayValues(store *configsync.DurableRelayCompatibility, values []configsync.RelayValue, codec configsync.SealedRelayCodec) (int, []error, error) {
	if store == nil {
		return 0, nil, configsync.ErrInvalidRelayState
	}
	operations := 0
	var importErrors []error
	_, err := store.Update(func(compatibility *configsync.RelayCompatibility) error {
		importErrors = compatibility.Import(values, func(value configsync.RelayValue, previous []configsync.RecordRef) ([]configsync.RecordRef, error) {
			refs, count, err := r.appendRelayValue(value, previous, codec)
			if err != nil {
				return nil, err
			}
			operations += count
			return refs, nil
		})
		return nil
	})
	if err != nil {
		return operations, importErrors, err
	}
	return operations, importErrors, nil
}

func (r *peerConfigReplica) canImportRelayRecord(relayKey, collection string, class configsync.KeyClass) bool {
	if relayKey == "ssh_hosts_encrypted" && (collection == configsync.CollectionSSHCredential || collection == configsync.CollectionSSHKeySecret) {
		return false
	}
	_, ok := r.keys[class]
	return ok
}

func (r *peerConfigReplica) appendPeerConfigMutations(label string, mutations []configsync.RecordMutation) (int, error) {
	if len(mutations) == 0 {
		return 0, nil
	}
	encrypted := make([]configsync.EncryptedMutation, 0, len(mutations))
	for _, mutation := range mutations {
		key, ok := r.keys[mutation.KeyClass]
		if !ok {
			return 0, fmt.Errorf("write %s: key class %s unavailable", label, mutation.KeyClass)
		}
		encrypted = append(encrypted, configsync.EncryptedMutation{Key: key, Mutation: configsync.Mutation{
			SchemaVersion: configsync.SchemaVersion,
			Collection:    mutation.Collection,
			RecordID:      mutation.RecordID,
			Kind:          mutation.Kind,
			Payload:       mutation.Payload,
		}})
	}
	operations, _, err := r.replica.AppendEncryptedBatch(r.identity, encrypted)
	if err != nil {
		return 0, fmt.Errorf("append %s: %w", label, err)
	}
	if _, err := r.compactIfNeeded(); err != nil {
		return len(operations), fmt.Errorf("compact %s: %w", label, err)
	}
	return len(operations), nil
}

func (r *peerConfigReplica) compactIfNeeded() (bool, error) {
	if !r.replica.NeedsCompaction(peerConfigCompactTailOperations, peerConfigCompactTailBytes) {
		return false, nil
	}
	stable, err := r.manager.stableConfigVector(r)
	if err != nil {
		return false, err
	}
	result, _, err := r.replica.CompactForBounds(
		r.identity, peerConfigCompactTailOperations, peerConfigCompactTailBytes, stable,
	)
	return result.Compacted, err
}

func (r *peerConfigReplica) pruneStableTombstones() (int, error) {
	stable, err := r.manager.stableConfigVector(r)
	if err != nil {
		return 0, err
	}
	if !r.replica.NeedsTombstonePruning(stable) {
		return 0, nil
	}
	result, _, err := r.replica.CompactForBounds(
		r.identity, peerConfigCompactTailOperations, peerConfigCompactTailBytes, stable,
	)
	return result.PrunedTombstones, err
}

func (m *peerSpaceManager) stableConfigVector(runtime *peerConfigReplica) (configsync.VersionVector, error) {
	state, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	active, err := activePeerMemberships(state, genesis, m.now())
	if err != nil {
		return nil, err
	}
	stable := runtime.replica.Vector()
	localPeerID := runtime.identity.PeerID()
	for _, membership := range active {
		peerID := membership.Document.SubjectPeerID
		if peerID == localPeerID {
			continue
		}
		acknowledged := state.ConfigSyncPeers[peerID].Acknowledged
		for actor, counter := range stable {
			if acknowledged[actor] == 0 {
				delete(stable, actor)
			} else if acknowledged[actor] < counter {
				stable[actor] = acknowledged[actor]
			}
		}
	}
	return stable, nil
}

func peerLocalCollectionsForKey(relayKey string) ([]string, error) {
	spec, ok := configsync.RelaySpec(relayKey)
	if !ok {
		return nil, fmt.Errorf("unknown Peer config key %q", relayKey)
	}
	if relayKey == "ssh_hosts_encrypted" {
		return []string{configsync.CollectionSSHHosts, configsync.CollectionSSHKeys}, nil
	}
	return spec.Collections, nil
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
