package main

import (
	"fmt"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
)

// bootstrapPeerConfig migrates existing local configuration only for the
// device that created the Space. A joining device must first adopt the Space
// and later present its local customizations as explicit pending imports.
func (a *App) bootstrapPeerConfig(manager *peerSpaceManager) (recordCount int, seeded bool, err error) {
	runtime, err := manager.ensureConfigReplica()
	if err != nil {
		return 0, false, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return 0, false, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return 0, false, fmt.Errorf("verify peer genesis for local config bootstrap: %w", err)
	}
	if genesis.Document.CreatorPeerID != runtime.identity.PeerID() {
		return 0, false, nil
	}

	cfg := appConfig{}
	if a.cfgStore != nil {
		cfg = a.cfgStore.Get()
	}
	records, err := peerBootstrapRecords(cfg)
	if err != nil {
		return 0, false, err
	}
	mutations := make([]configsync.EncryptedMutation, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		canonicalID := record.Collection + "\x00" + record.RecordID
		if _, duplicate := seen[canonicalID]; duplicate {
			return 0, false, fmt.Errorf("duplicate Peer bootstrap record %s/%s", record.Collection, record.RecordID)
		}
		seen[canonicalID] = struct{}{}
		key, ok := runtime.keys[record.KeyClass]
		if !ok {
			return 0, false, fmt.Errorf("Peer bootstrap key class %s is unavailable", record.KeyClass)
		}
		mutation, err := configsync.SetRecordMutation(record)
		if err != nil {
			return 0, false, err
		}
		mutations = append(mutations, configsync.EncryptedMutation{Key: key, Mutation: configsync.Mutation{
			SchemaVersion: configsync.SchemaVersion,
			Collection:    mutation.Collection,
			RecordID:      mutation.RecordID,
			Kind:          mutation.Kind,
			Payload:       mutation.Payload,
		}})
	}
	_, _, seeded, err = runtime.replica.BootstrapEncrypted(runtime.identity, mutations)
	if err != nil {
		return 0, false, err
	}
	return len(records), seeded, nil
}

func peerBootstrapRecords(cfg appConfig) ([]configsync.PlainRecord, error) {
	records, err := peerBootstrapLegacyRecords(cfg)
	if err != nil {
		return nil, err
	}

	profiles, err := peerLocalRecordsForKey(cfg, "profiles_encrypted")
	if err != nil {
		return nil, err
	}
	records = append(records, profiles...)
	ssh, err := peerLocalRecordsForKey(cfg, "ssh_hosts_encrypted")
	if err != nil {
		return nil, err
	}
	records = append(records, ssh...)
	return records, nil
}

// peerLocalRecordsForKey translates one local Relay-compatible preference
// into canonical records without opening Relay account-key envelopes. SSH
// secrets stay in the legacy account/keychain path until users can opt in to
// a dedicated Peer secret-sync policy.
func peerLocalRecordsForKey(cfg appConfig, key string) ([]configsync.PlainRecord, error) {
	if _, ok := configsync.RelaySpec(key); !ok {
		return nil, fmt.Errorf("unknown Peer config key %q", key)
	}
	switch key {
	case "profiles_encrypted":
		return peerLocalProfileRecords(cfg)
	case "ssh_hosts_encrypted":
		return peerLocalSSHMetadataRecords(cfg)
	default:
		store := &configStore{cfg: cfg}
		value, ok := newAppConfigAdapter(store, nil).ReadValue(key)
		if !ok {
			return nil, nil
		}
		records, err := configsync.DecodeRelayRecords(configsync.RelayValue{Key: key, Value: value}, nil)
		if err != nil {
			return nil, fmt.Errorf("prepare local Peer config %s: %w", key, err)
		}
		return records, nil
	}
}

func peerLocalProfileRecords(cfg appConfig) ([]configsync.PlainRecord, error) {
	var records []configsync.PlainRecord
	profiles := filterValidProfiles(cfg.Profiles)
	for index, source := range profiles {
		profile := source
		environment := profile.Env
		profile.Env = nil
		value, err := configsync.CanonicalEntityJSON(profile.ID, profile)
		if err != nil {
			return nil, err
		}
		records = append(records, configsync.PlainRecord{
			Collection: configsync.CollectionProfiles, RecordID: profile.ID,
			KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
		})
		if profile.SyncEnv && len(environment) != 0 {
			envValue, err := configsync.CanonicalEntityJSON(profile.ID, profileEnvRecord{ID: profile.ID, Env: environment})
			if err != nil {
				return nil, err
			}
			records = append(records, configsync.PlainRecord{
				Collection: configsync.CollectionProfileEnv, RecordID: profile.ID,
				KeyClass: configsync.KeyClassVault, Value: envValue,
			})
		}
	}
	if len(profiles) != 0 {
		value, err := configsync.CanonicalEntityJSON(profileConfigRecordID, profileConfigRecord{
			ID: profileConfigRecordID, DefaultProfileID: resolveDefaultProfileID(cfg.DefaultProfileID, profiles),
		})
		if err != nil {
			return nil, err
		}
		records = append(records, configsync.PlainRecord{
			Collection: configsync.CollectionProfileConfig, RecordID: profileConfigRecordID,
			KeyClass: configsync.KeyClassSync, Value: value,
		})
	}
	return records, nil
}

func peerLocalSSHMetadataRecords(cfg appConfig) ([]configsync.PlainRecord, error) {
	records := make([]configsync.PlainRecord, 0, len(cfg.SSHHosts)+len(cfg.SSHKeys))
	for index, host := range cfg.SSHHosts {
		value, err := configsync.CanonicalEntityJSON(host.ID, host)
		if err != nil {
			return nil, err
		}
		records = append(records, configsync.PlainRecord{
			Collection: configsync.CollectionSSHHosts, RecordID: host.ID,
			KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
		})
	}
	for index, key := range cfg.SSHKeys {
		value, err := configsync.CanonicalEntityJSON(key.ID, key)
		if err != nil {
			return nil, err
		}
		records = append(records, configsync.PlainRecord{
			Collection: configsync.CollectionSSHKeys, RecordID: key.ID,
			KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
		})
	}
	return records, nil
}

func peerBootstrapLegacyRecords(cfg appConfig) ([]configsync.PlainRecord, error) {
	customized := isPrefCustomized(cfg)
	var records []configsync.PlainRecord
	for _, spec := range configsync.RelayKeySpecs() {
		if spec.Mode == configsync.RelaySealedBundle || !customized(spec.Key) {
			continue
		}
		decoded, err := peerLocalRecordsForKey(cfg, spec.Key)
		if err != nil {
			return nil, err
		}
		records = append(records, decoded...)
	}
	return records, nil
}
