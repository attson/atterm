package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/attson/atterm/internal/configsync"
)

const profileConfigRecordID = "default"

// desktopRelaySealedCodec is the only bridge allowed to open legacy Relay
// whole-value blobs. configsync receives typed plaintext records, never the
// Relay account_key itself.
type desktopRelaySealedCodec struct {
	accountKey func() []byte
}

type profileConfigRecord struct {
	ID               string `json:"id"`
	DefaultProfileID string `json:"default_profile_id,omitempty"`
}

type profileEnvRecord struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

type sshCredentialRecord struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

type sshKeySecretRecord struct {
	ID         string `json:"id"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

func newDesktopRelaySealedCodec(accountKey func() []byte) *desktopRelaySealedCodec {
	return &desktopRelaySealedCodec{accountKey: accountKey}
}

func (c *desktopRelaySealedCodec) DecodeSealedRelayValue(key string, normalized json.RawMessage) ([]configsync.PlainRecord, error) {
	accountKey, err := c.key()
	if err != nil {
		return nil, err
	}
	switch key {
	case "profiles_encrypted":
		profiles, defaultID, err := openProfiles(accountKey, normalized)
		if err != nil {
			return nil, err
		}
		profiles = filterValidProfiles(profiles)
		records := make([]configsync.PlainRecord, 0, len(profiles)*2+1)
		for index, profile := range profiles {
			env := profile.Env
			profile.Env = nil
			value, err := configsync.CanonicalEntityJSON(profile.ID, profile)
			if err != nil {
				return nil, err
			}
			records = append(records, configsync.PlainRecord{
				Collection: configsync.CollectionProfiles, RecordID: profile.ID,
				KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
			})
			if profile.SyncEnv && len(env) != 0 {
				envValue, err := configsync.CanonicalEntityJSON(profile.ID, profileEnvRecord{ID: profile.ID, Env: env})
				if err != nil {
					return nil, err
				}
				records = append(records, configsync.PlainRecord{
					Collection: configsync.CollectionProfileEnv, RecordID: profile.ID,
					KeyClass: configsync.KeyClassVault, Value: envValue,
				})
			}
		}
		configValue, err := configsync.CanonicalEntityJSON(profileConfigRecordID, profileConfigRecord{
			ID: profileConfigRecordID, DefaultProfileID: defaultID,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, configsync.PlainRecord{
			Collection: configsync.CollectionProfileConfig, RecordID: profileConfigRecordID,
			KeyClass: configsync.KeyClassSync, Value: configValue,
		})
		return records, nil

	case "ssh_hosts_encrypted":
		hosts, credentials, keys, keySecrets, err := openSSHHosts(accountKey, normalized)
		if err != nil {
			return nil, err
		}
		records := make([]configsync.PlainRecord, 0, len(hosts)*2+len(keys)*2)
		for index, host := range hosts {
			value, err := configsync.CanonicalEntityJSON(host.ID, host)
			if err != nil {
				return nil, err
			}
			records = append(records, configsync.PlainRecord{
				Collection: configsync.CollectionSSHHosts, RecordID: host.ID,
				KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
			})
			if credential := credentials[host.ID]; credential.Password != "" {
				secret, err := configsync.CanonicalEntityJSON(host.ID, sshCredentialRecord{ID: host.ID, Password: credential.Password})
				if err != nil {
					return nil, err
				}
				records = append(records, configsync.PlainRecord{
					Collection: configsync.CollectionSSHCredential, RecordID: host.ID,
					KeyClass: configsync.KeyClassVault, Value: secret,
				})
			}
		}
		for index, keyMetadata := range keys {
			value, err := configsync.CanonicalEntityJSON(keyMetadata.ID, keyMetadata)
			if err != nil {
				return nil, err
			}
			records = append(records, configsync.PlainRecord{
				Collection: configsync.CollectionSSHKeys, RecordID: keyMetadata.ID,
				KeyClass: configsync.KeyClassSync, Position: configsync.PositionForIndex(uint64(index)), Value: value,
			})
			if secret := keySecrets[keyMetadata.ID]; secret.PrivateKey != "" || secret.Passphrase != "" {
				secretValue, err := configsync.CanonicalEntityJSON(keyMetadata.ID, sshKeySecretRecord{
					ID: keyMetadata.ID, PrivateKey: secret.PrivateKey, Passphrase: secret.Passphrase,
				})
				if err != nil {
					return nil, err
				}
				records = append(records, configsync.PlainRecord{
					Collection: configsync.CollectionSSHKeySecret, RecordID: keyMetadata.ID,
					KeyClass: configsync.KeyClassVault, Value: secretValue,
				})
			}
		}
		return records, nil
	default:
		return nil, fmt.Errorf("unsupported sealed Relay key %q", key)
	}
}

func (c *desktopRelaySealedCodec) EncodeSealedRelayValue(key string, records []configsync.PlainRecord) (json.RawMessage, error) {
	accountKey, err := c.key()
	if err != nil {
		return nil, err
	}
	switch key {
	case "profiles_encrypted":
		var profiles []positionedProfile
		environments := make(map[string]map[string]string)
		defaultID := ""
		for _, record := range records {
			switch record.Collection {
			case configsync.CollectionProfiles:
				var profile SessionProfile
				if err := json.Unmarshal(record.Value, &profile); err != nil {
					return nil, err
				}
				profiles = append(profiles, positionedProfile{position: record.Position, profile: profile})
			case configsync.CollectionProfileEnv:
				var env profileEnvRecord
				if err := json.Unmarshal(record.Value, &env); err != nil {
					return nil, err
				}
				environments[record.RecordID] = env.Env
			case configsync.CollectionProfileConfig:
				var config profileConfigRecord
				if err := json.Unmarshal(record.Value, &config); err != nil {
					return nil, err
				}
				defaultID = config.DefaultProfileID
			}
		}
		sort.Slice(profiles, func(i, j int) bool {
			return orderedBefore(profiles[i].position, profiles[i].profile.ID, profiles[j].position, profiles[j].profile.ID)
		})
		plainProfiles := make([]SessionProfile, len(profiles))
		for i, positioned := range profiles {
			profile := positioned.profile
			if profile.SyncEnv {
				profile.Env = cloneStringMap(environments[profile.ID])
			}
			plainProfiles[i] = profile
		}
		return sealProfiles(accountKey, plainProfiles, resolveDefaultProfileID(defaultID, plainProfiles))

	case "ssh_hosts_encrypted":
		var hosts []positionedSSHHost
		var keys []positionedSSHKey
		credentials := make(map[string]sshCredential)
		keySecrets := make(map[string]sshKeySecret)
		for _, record := range records {
			switch record.Collection {
			case configsync.CollectionSSHHosts:
				var host SSHHost
				if err := json.Unmarshal(record.Value, &host); err != nil {
					return nil, err
				}
				hosts = append(hosts, positionedSSHHost{position: record.Position, host: host})
			case configsync.CollectionSSHKeys:
				var keyMetadata SSHKey
				if err := json.Unmarshal(record.Value, &keyMetadata); err != nil {
					return nil, err
				}
				keys = append(keys, positionedSSHKey{position: record.Position, key: keyMetadata})
			case configsync.CollectionSSHCredential:
				var credential sshCredentialRecord
				if err := json.Unmarshal(record.Value, &credential); err != nil {
					return nil, err
				}
				credentials[record.RecordID] = sshCredential{Password: credential.Password}
			case configsync.CollectionSSHKeySecret:
				var secret sshKeySecretRecord
				if err := json.Unmarshal(record.Value, &secret); err != nil {
					return nil, err
				}
				keySecrets[record.RecordID] = sshKeySecret{PrivateKey: secret.PrivateKey, Passphrase: secret.Passphrase}
			}
		}
		sort.Slice(hosts, func(i, j int) bool {
			return orderedBefore(hosts[i].position, hosts[i].host.ID, hosts[j].position, hosts[j].host.ID)
		})
		sort.Slice(keys, func(i, j int) bool {
			return orderedBefore(keys[i].position, keys[i].key.ID, keys[j].position, keys[j].key.ID)
		})
		plainHosts := make([]SSHHost, len(hosts))
		for i := range hosts {
			plainHosts[i] = hosts[i].host
		}
		plainKeys := make([]SSHKey, len(keys))
		for i := range keys {
			plainKeys[i] = keys[i].key
		}
		return sealSSHHosts(accountKey, plainHosts, credentials, plainKeys, keySecrets)
	default:
		return nil, fmt.Errorf("unsupported sealed Relay key %q", key)
	}
}

type positionedProfile struct {
	position string
	profile  SessionProfile
}

type positionedSSHHost struct {
	position string
	host     SSHHost
}

type positionedSSHKey struct {
	position string
	key      SSHKey
}

func orderedBefore(leftPosition, leftID, rightPosition, rightID string) bool {
	left, leftErr := configsync.IndexFromPosition(leftPosition)
	right, rightErr := configsync.IndexFromPosition(rightPosition)
	if leftErr != nil || rightErr != nil {
		return leftPosition < rightPosition || leftPosition == rightPosition && leftID < rightID
	}
	return left < right || left == right && leftID < rightID
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func (c *desktopRelaySealedCodec) key() ([]byte, error) {
	if c == nil || c.accountKey == nil {
		return nil, errors.New("Relay account key is unavailable")
	}
	key := c.accountKey()
	if len(key) == 0 {
		return nil, errors.New("Relay account key is unavailable")
	}
	return append([]byte(nil), key...), nil
}
