package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/attson/atterm/internal/configsync"
)

func testRelayAccountKey() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestDesktopRelayCodecSplitsProfilesMetadataDefaultAndVaultEnv(t *testing.T) {
	accountKey := testRelayAccountKey()
	profiles := []SessionProfile{
		{ID: "local-env", Name: "Local", Env: map[string]string{"LOCAL": "only"}, SyncEnv: false},
		{ID: "shared-env", Name: "Shared", Shell: "/bin/zsh", Env: map[string]string{"TOKEN": "secret"}, SyncEnv: true},
	}
	sealed, err := sealProfiles(accountKey, profiles, "shared-env")
	if err != nil {
		t.Fatal(err)
	}
	codec := newDesktopRelaySealedCodec(func() []byte { return accountKey })
	records, err := configsync.DecodeRelayRecords(configsync.RelayValue{Key: "profiles_encrypted", Value: sealed}, codec)
	if err != nil {
		t.Fatal(err)
	}
	if got := countRecordCollections(records); !reflect.DeepEqual(got, map[string]int{
		configsync.CollectionProfiles: 2, configsync.CollectionProfileConfig: 1, configsync.CollectionProfileEnv: 1,
	}) {
		t.Fatalf("collections=%v records=%+v", got, records)
	}
	for _, record := range records {
		if record.Collection == configsync.CollectionProfiles && string(record.Value) == "" {
			t.Fatal("empty profile metadata")
		}
		if record.Collection == configsync.CollectionProfiles {
			var profile SessionProfile
			if err := json.Unmarshal(record.Value, &profile); err != nil {
				t.Fatal(err)
			}
			if len(profile.Env) != 0 {
				t.Fatalf("profile metadata leaked env: %+v", profile)
			}
		}
	}
	resealed, refs, err := configsync.EncodeRelayRecords("profiles_encrypted", records, codec)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != len(records) {
		t.Fatalf("refs=%d records=%d", len(refs), len(records))
	}
	opened, defaultID, err := openProfiles(accountKey, resealed)
	if err != nil {
		t.Fatal(err)
	}
	if defaultID != "shared-env" || len(opened) != 2 {
		t.Fatalf("opened=%+v default=%q", opened, defaultID)
	}
	if len(opened[0].Env) != 0 || !reflect.DeepEqual(opened[1].Env, map[string]string{"TOKEN": "secret"}) {
		t.Fatalf("env split did not round-trip: %+v", opened)
	}
}

func TestDesktopRelayCodecSplitsSSHMetadataAndVaultSecrets(t *testing.T) {
	accountKey := testRelayAccountKey()
	hosts := []SSHHost{
		{ID: "host-b", Alias: "B", Host: "b.example", User: "root", AuthKind: "password"},
		{ID: "host-a", Alias: "A", Host: "a.example", User: "dev", AuthKind: "key", KeyID: "key-1"},
	}
	credentials := map[string]sshCredential{"host-b": {Password: "password"}, "host-a": {}}
	keys := []SSHKey{{ID: "key-1", Name: "Primary", KeyType: "ED25519"}}
	secrets := map[string]sshKeySecret{"key-1": {PrivateKey: "private-pem", Passphrase: "passphrase"}}
	sealed, err := sealSSHHosts(accountKey, hosts, credentials, keys, secrets)
	if err != nil {
		t.Fatal(err)
	}
	codec := newDesktopRelaySealedCodec(func() []byte { return accountKey })
	records, err := configsync.DecodeRelayRecords(configsync.RelayValue{Key: "ssh_hosts_encrypted", Value: sealed}, codec)
	if err != nil {
		t.Fatal(err)
	}
	if got := countRecordCollections(records); !reflect.DeepEqual(got, map[string]int{
		configsync.CollectionSSHHosts: 2, configsync.CollectionSSHKeys: 1,
		configsync.CollectionSSHCredential: 1, configsync.CollectionSSHKeySecret: 1,
	}) {
		t.Fatalf("collections=%v records=%+v", got, records)
	}
	// Reorder input to prove positions, rather than codec input order, govern
	// the legacy arrays.
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	resealed, _, err := configsync.EncodeRelayRecords("ssh_hosts_encrypted", records, codec)
	if err != nil {
		t.Fatal(err)
	}
	openedHosts, openedCredentials, openedKeys, openedSecrets, err := openSSHHosts(accountKey, resealed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(openedHosts, hosts) || !reflect.DeepEqual(openedKeys, keys) ||
		!reflect.DeepEqual(openedCredentials, credentials) || !reflect.DeepEqual(openedSecrets, secrets) {
		t.Fatalf("SSH round-trip hosts=%+v creds=%+v keys=%+v secrets=%+v", openedHosts, openedCredentials, openedKeys, openedSecrets)
	}
}

func TestDesktopRelayCodecRequiresAccountKey(t *testing.T) {
	codec := newDesktopRelaySealedCodec(func() []byte { return nil })
	_, err := codec.DecodeSealedRelayValue("profiles_encrypted", json.RawMessage(`"YWJj"`))
	if err == nil {
		t.Fatal("missing account key accepted")
	}
	if errors.Is(err, configsync.ErrInvalidSchemaValue) {
		t.Fatalf("missing key misclassified as schema error: %v", err)
	}
}

func countRecordCollections(records []configsync.PlainRecord) map[string]int {
	out := make(map[string]int)
	for _, record := range records {
		out[record.Collection]++
	}
	return out
}
