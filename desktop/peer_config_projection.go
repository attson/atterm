package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/attson/atterm/internal/configsync"
)

// projectLocalConfig applies the current canonical winners to an independent
// config snapshot. The caller decides when that snapshot may replace local
// state, after pending local imports have been captured durably.
func (r *peerConfigReplica) projectLocalConfig(base appConfig) (appConfig, []error) {
	projected := detachMaps(base)
	var errs []error
	for _, spec := range configsync.RelayKeySpecs() {
		var err error
		switch spec.Key {
		case "profiles_encrypted":
			err = r.projectProfiles(&projected)
		case "ssh_hosts_encrypted":
			err = r.projectSSHMetadata(&projected)
		default:
			err = r.projectPortableKey(&projected, spec)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("project Peer config %s: %w", spec.Key, err))
		}
	}
	return projected, errs
}

func (r *peerConfigReplica) projectPortableKey(projected *appConfig, spec configsync.RelayKeySpec) error {
	var records []configsync.Record
	found := false
	for _, collection := range spec.Collections {
		for _, record := range r.replica.Records(collection) {
			if spec.Mode == configsync.RelayScalar && record.RecordID != spec.Key {
				continue
			}
			found = true
			records = append(records, record)
		}
	}
	if !found {
		return nil
	}
	value, ok, err := configsync.MaterializeRelayValue(spec.Key, records, r.resolveEpochKey, nil)
	if err != nil {
		return err
	}
	if !ok {
		if spec.Mode != configsync.RelayScalar || !clearPortableConfigValue(projected, spec.Key) {
			return fmt.Errorf("canonical tombstone cannot clear key")
		}
		return nil
	}
	handled, err := applyPortableConfigValue(projected, spec.Key, value.Value)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("portable key is unsupported")
	}
	return nil
}

func (r *peerConfigReplica) projectProfiles(projected *appConfig) error {
	records, found, err := r.openLocalProjectionRecords([]string{
		configsync.CollectionProfiles,
		configsync.CollectionProfileConfig,
		configsync.CollectionProfileEnv,
	})
	if err != nil || !found {
		return err
	}
	var profiles []positionedProfile
	environments := make(map[string]map[string]string)
	defaultID := ""
	for _, record := range records {
		switch record.Collection {
		case configsync.CollectionProfiles:
			var profile SessionProfile
			if err := json.Unmarshal(record.Value, &profile); err != nil {
				return err
			}
			profiles = append(profiles, positionedProfile{position: record.Position, profile: profile})
		case configsync.CollectionProfileEnv:
			var environment profileEnvRecord
			if err := json.Unmarshal(record.Value, &environment); err != nil {
				return err
			}
			environments[record.RecordID] = cloneStringMap(environment.Env)
		case configsync.CollectionProfileConfig:
			var config profileConfigRecord
			if err := json.Unmarshal(record.Value, &config); err != nil {
				return err
			}
			defaultID = config.DefaultProfileID
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		return orderedBefore(profiles[i].position, profiles[i].profile.ID, profiles[j].position, profiles[j].profile.ID)
	})
	localByID := make(map[string]SessionProfile, len(projected.Profiles))
	for _, profile := range projected.Profiles {
		localByID[profile.ID] = profile
	}
	_, canOpenVault := r.keys[configsync.KeyClassVault]
	incoming := make([]SessionProfile, 0, len(profiles))
	for _, positioned := range profiles {
		profile := positioned.profile
		local, hasLocal := localByID[profile.ID]
		if !profile.SyncEnv || !canOpenVault {
			if hasLocal {
				profile.Env = cloneStringMap(local.Env)
			} else {
				profile.Env = nil
			}
		} else if environment, ok := environments[profile.ID]; ok {
			profile.Env = environment
		} else {
			// With vault access, absence means the canonical env record was
			// deleted rather than merely hidden by this device's capability.
			profile.Env = nil
		}
		incoming = append(incoming, profile)
	}
	incoming = filterValidProfiles(incoming)
	projected.Profiles = incoming
	projected.DefaultProfileID = resolveDefaultProfileID(defaultID, incoming)
	return nil
}

func (r *peerConfigReplica) projectSSHMetadata(projected *appConfig) error {
	records, found, err := r.openLocalProjectionRecords([]string{
		configsync.CollectionSSHHosts,
		configsync.CollectionSSHKeys,
	})
	if err != nil || !found {
		return err
	}
	var hosts []positionedSSHHost
	var keys []positionedSSHKey
	for _, record := range records {
		switch record.Collection {
		case configsync.CollectionSSHHosts:
			var host SSHHost
			if err := json.Unmarshal(record.Value, &host); err != nil {
				return err
			}
			hosts = append(hosts, positionedSSHHost{position: record.Position, host: host})
		case configsync.CollectionSSHKeys:
			var key SSHKey
			if err := json.Unmarshal(record.Value, &key); err != nil {
				return err
			}
			keys = append(keys, positionedSSHKey{position: record.Position, key: key})
		}
	}
	sort.Slice(hosts, func(i, j int) bool {
		return orderedBefore(hosts[i].position, hosts[i].host.ID, hosts[j].position, hosts[j].host.ID)
	})
	sort.Slice(keys, func(i, j int) bool {
		return orderedBefore(keys[i].position, keys[i].key.ID, keys[j].position, keys[j].key.ID)
	})
	projected.SSHHosts = make([]SSHHost, len(hosts))
	for i := range hosts {
		projected.SSHHosts[i] = hosts[i].host
	}
	projected.SSHKeys = make([]SSHKey, len(keys))
	for i := range keys {
		projected.SSHKeys[i] = keys[i].key
	}
	return nil
}

// openLocalProjectionRecords skips a capability class this device does not
// own, but never substitutes a current key for a different record epoch.
func (r *peerConfigReplica) openLocalProjectionRecords(collections []string) ([]configsync.PlainRecord, bool, error) {
	var plain []configsync.PlainRecord
	found := false
	for _, collection := range collections {
		for _, record := range r.replica.Records(collection) {
			key, available := r.keys[record.KeyClass]
			if !available {
				continue
			}
			found = true
			if record.Deleted {
				continue
			}
			if key.Epoch != record.KeyEpoch {
				return nil, false, fmt.Errorf("%w: epoch %s/%d unavailable", configsync.ErrInvalidEpochKey, record.KeyClass, record.KeyEpoch)
			}
			opened, err := configsync.OpenPlainRecord(record, key)
			if err != nil {
				return nil, false, err
			}
			plain = append(plain, opened)
		}
	}
	return plain, found, nil
}

func (r *peerConfigReplica) resolveEpochKey(class configsync.KeyClass, epoch uint64) (configsync.EpochKey, bool) {
	key, ok := r.keys[class]
	return key, ok && key.Epoch == epoch
}
