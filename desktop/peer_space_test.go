package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/safekeyring"
)

func newTestPeerApp(t *testing.T) (*App, time.Time) {
	t.Helper()
	dir := t.TempDir()
	safekeyring.SetFileDirForTest(filepath.Join(dir, "keyring"))
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	now := time.Unix(1_800_000_000, 0)
	storePath := filepath.Join(dir, "peer-space.json")
	manager := &peerSpaceManager{
		now:               func() time.Time { return now },
		bootstrapLockPath: storePath + ".bootstrap.lock",
		configRoot:        filepath.Join(dir, "peer-spaces"),
	}
	manager.store = peerstore.New(storePath, func() ([]byte, error) {
		key, err := peerStoreKeySlot().Load()
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, errors.New("missing test peer store key")
		}
		return key, nil
	})
	return &App{peerSpace: manager}, now
}

func TestConcurrentPeerSpaceBootstrapKeepsOneIdentity(t *testing.T) {
	app, now := newTestPeerApp(t)
	otherManager := &peerSpaceManager{
		now:               func() time.Time { return now },
		bootstrapLockPath: app.peerSpace.bootstrapLockPath,
		configRoot:        app.peerSpace.configRoot,
	}
	storePath := strings.TrimSuffix(app.peerSpace.bootstrapLockPath, ".bootstrap.lock")
	otherManager.store = peerstore.New(storePath, func() ([]byte, error) {
		key, err := peerStoreKeySlot().Load()
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, errors.New("missing test peer store key")
		}
		return key, nil
	})
	other := &App{peerSpace: otherManager}

	statuses := make([]PeerSpaceStatus, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for index, candidate := range []*App{app, other} {
		wg.Add(1)
		go func(index int, candidate *App) {
			defer wg.Done()
			statuses[index], errs[index] = candidate.CreatePeerSpace()
		}(index, candidate)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if statuses[0].PeerID != statuses[1].PeerID || statuses[0].SpaceID != statuses[1].SpaceID {
		t.Fatalf("concurrent bootstrap diverged: %+v vs %+v", statuses[0], statuses[1])
	}
}

func TestCreatePeerSpaceIsIdempotent(t *testing.T) {
	app, _ := newTestPeerApp(t)
	first, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Configured || first.PeerID == "" || first.SpaceID == "" || first.GenesisHash == "" {
		t.Fatalf("incomplete status: %+v", first)
	}
	firstWrapping, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	if second.PeerID != first.PeerID || second.SpaceID != first.SpaceID || second.GenesisHash != first.GenesisHash {
		t.Fatalf("CreatePeerSpace rotated identity: first=%+v second=%+v", first, second)
	}
	secondWrapping, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstWrapping, secondWrapping) {
		t.Fatal("CreatePeerSpace rotated wrapping identity")
	}
}

func TestCreatePeerSpacePersistsAndRecoversInitialEpochKeys(t *testing.T) {
	app, _ := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.EpochRotations) != 2 {
		t.Fatalf("epoch rotations=%d want=2", len(state.EpochRotations))
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	rotations, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := app.peerSpace.loadWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		stored, err := loadPeerEpochKey(status.SpaceID, class)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := configsync.OpenRotationEpochKey(rotations[class], genesis, status.PeerID, wrapping)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Epoch != 1 || !bytes.Equal(stored.Bytes(), opened.Bytes()) {
			t.Fatalf("stored %s epoch key differs from rotation", class)
		}
		if err := peerEpochKeySlot(status.SpaceID, class).Clear(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.GetPeerSpaceStatus(); err != nil {
		t.Fatalf("recover epoch keys: %v", err)
	}
	recoveredState, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recoveredState.EpochRotations, state.EpochRotations) {
		t.Fatal("epoch key recovery replaced signed rotations")
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if _, err := loadPeerEpochKey(status.SpaceID, class); err != nil {
			t.Fatalf("recovered %s key: %v", class, err)
		}
	}
}

func TestPeerEpochKeysRotateOnScheduleWithoutLosingOldConfig(t *testing.T) {
	app, now := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	before, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	app.peerSpace.now = func() time.Time { return now.Add(peerEpochKeyRotationInterval + time.Minute) }
	if rotated, err := app.peerSpace.rotateEpochKeysIfNeeded(); err != nil || !rotated {
		t.Fatalf("scheduled rotation=%t err=%v", rotated, err)
	}
	updated, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	after, err := currentEpochRotations(updated.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if after[class].Document.Epoch != before[class].Document.Epoch+1 {
			t.Fatalf("%s epoch=%d want=%d", class, after[class].Document.Epoch, before[class].Document.Epoch+1)
		}
	}
	runtime, err := app.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	record, ok := runtime.replica.Get(configsync.CollectionPreferences, "terminal_theme")
	if !ok || record.KeyEpoch != 1 {
		t.Fatalf("pre-rotation record=%+v present=%t", record, ok)
	}
	plain, err := runtime.openRecord(record)
	if err != nil || string(plain.Value) != `"nord"` {
		t.Fatalf("open pre-rotation value=%s err=%v", plain.Value, err)
	}
	if rotated, err := app.peerSpace.rotateEpochKeysIfNeeded(); err != nil || rotated {
		t.Fatalf("repeated scheduled rotation=%t err=%v", rotated, err)
	}
}

func TestCreatePeerSpaceInitializesDurableConfigReplica(t *testing.T) {
	app, _ := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	if runtime == nil || runtime.replica == nil || runtime.identity == nil {
		t.Fatal("Peer Space config replica runtime was not initialized")
	}
	if runtime.spaceID != status.SpaceID || runtime.identity.PeerID() != status.PeerID {
		t.Fatalf("config runtime identity mismatch: status=%+v runtime=%+v", status, runtime)
	}
	syncKey, ok := runtime.keys[configsync.KeyClassSync]
	if !ok || syncKey.Epoch != 1 {
		t.Fatalf("sync epoch key=%+v present=%t", syncKey, ok)
	}
	if _, ok := runtime.keys[configsync.KeyClassVault]; !ok {
		t.Fatal("Space creator config runtime is missing its vault epoch key")
	}

	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_theme",
		Kind:          configsync.KindSet,
		Payload:       []byte(`"dark"`),
	}); err != nil {
		t.Fatal(err)
	}
	replicaPath := filepath.Join(app.peerSpace.configRoot, status.SpaceID, "config-replica.json")
	if info, err := os.Stat(replicaPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config replica file: info=%v err=%v", info, err)
	}

	app.peerSpace.configReplica = nil
	reopened, err := app.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.replica.Vector()[status.PeerID]; got != 1 {
		t.Fatalf("reopened config vector counter=%d want=1", got)
	}
}

func TestCreatePeerSpaceBootstrapsPortableConfigOnce(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{
		TerminalTheme:  "nord",
		QuickTemplates: []QuickTemplate{{ID: "template-1", Label: "Build", Text: "go test ./..."}},
		Profiles: []SessionProfile{
			{ID: "profile-local", Name: "Local env", Env: map[string]string{"LOCAL_ONLY": "secret"}},
			{ID: "profile-shared", Name: "Shared env", SyncEnv: true, Env: map[string]string{"SHARED": "yes"}},
		},
		DefaultProfileID: "profile-shared",
		SSHHosts:         []SSHHost{{ID: "host-1", Alias: "Production", Host: "example.com", User: "alice", AuthKind: "key", KeyID: "key-1"}},
		SSHKeys:          []SSHKey{{ID: "key-1", Name: "Primary", KeyType: "ED25519"}},
	}}
	if err := sshCredentialSlot("host-1").Save(sshCredential{Password: "must-not-sync"}); err != nil {
		t.Fatal(err)
	}
	if err := sshKeySecretSlot("key-1").Save(sshKeySecret{PrivateKey: "must-not-sync"}); err != nil {
		t.Fatal(err)
	}

	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	if got := runtime.replica.Vector()[status.PeerID]; got != 8 {
		t.Fatalf("bootstrap operation count=%d want=8", got)
	}
	if len(runtime.replica.Records(configsync.CollectionSSHCredential)) != 0 || len(runtime.replica.Records(configsync.CollectionSSHKeySecret)) != 0 {
		t.Fatal("SSH secrets entered the Peer bootstrap replica")
	}
	if got := len(runtime.replica.Records(configsync.CollectionProfileEnv)); got != 1 {
		t.Fatalf("profile env record count=%d want=1", got)
	}

	profileRecord, ok := runtime.replica.Get(configsync.CollectionProfiles, "profile-local")
	if !ok {
		t.Fatal("local profile metadata missing")
	}
	plainProfile, err := configsync.OpenPlainRecord(profileRecord, runtime.keys[configsync.KeyClassSync])
	if err != nil {
		t.Fatal(err)
	}
	var profile SessionProfile
	if err := json.Unmarshal(plainProfile.Value, &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Env) != 0 {
		t.Fatalf("non-opted profile env leaked into metadata: %+v", profile.Env)
	}

	envRecord, ok := runtime.replica.Get(configsync.CollectionProfileEnv, "profile-shared")
	if !ok {
		t.Fatal("opted-in profile env missing")
	}
	plainEnv, err := configsync.OpenPlainRecord(envRecord, runtime.keys[configsync.KeyClassVault])
	if err != nil {
		t.Fatal(err)
	}
	var environment profileEnvRecord
	if err := json.Unmarshal(plainEnv.Value, &environment); err != nil {
		t.Fatal(err)
	}
	if environment.Env["SHARED"] != "yes" {
		t.Fatalf("opted-in profile env=%+v", environment.Env)
	}
	codec := newDesktopRelaySealedCodec(func() []byte { return bytes.Repeat([]byte{7}, 32) })
	materialized, materializeErrors := runtime.materializeRelayValues(codec)
	if len(materializeErrors) != 0 {
		t.Fatalf("materialize Relay values: %v", materializeErrors)
	}
	materializedByKey := make(map[string]configsync.RelayValue, len(materialized))
	for _, value := range materialized {
		materializedByKey[value.Key] = value
	}
	for _, key := range []string{"terminal_theme", "quick_templates", "profiles_encrypted", "ssh_hosts_encrypted"} {
		if _, ok := materializedByKey[key]; !ok {
			t.Fatalf("materialized Relay value %s missing: %+v", key, materializedByKey)
		}
	}

	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	if got := runtime.replica.Vector()[status.PeerID]; got != 8 {
		t.Fatalf("idempotent bootstrap advanced vector to %d", got)
	}
}

func TestPeerConfigReplicaAppendsOnlyChangedLocalRecords(t *testing.T) {
	app, _ := newTestPeerApp(t)
	cfg := appConfig{
		TerminalTheme:    "nord",
		TerminalFontSize: 17,
		Profiles: []SessionProfile{{
			ID: "profile-1", Name: "Shared", SyncEnv: true,
			Env: map[string]string{"TOKEN": "shared"},
		}},
		DefaultProfileID: "profile-1",
		SSHHosts:         []SSHHost{{ID: "host-1", Alias: "Before", Host: "example.com", User: "alice"}},
	}
	app.cfgStore = &configStore{cfg: cfg}
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	before := runtime.replica.Vector()[status.PeerID]

	if operations, err := runtime.appendLocalRelayKey(cfg, "terminal_theme"); err != nil || operations != 0 {
		t.Fatalf("unchanged terminal theme operations=%d err=%v", operations, err)
	}
	changed := cfg
	changed.TerminalTheme = "dark"
	if operations, err := runtime.appendLocalRelayKey(changed, "terminal_theme"); err != nil || operations != 1 {
		t.Fatalf("changed terminal theme operations=%d err=%v", operations, err)
	}
	if font, ok := runtime.replica.Get(configsync.CollectionPreferences, "terminal_font_size"); !ok || font.Deleted {
		t.Fatal("updating one scalar deleted another preference")
	}

	changed.Profiles = append([]SessionProfile(nil), cfg.Profiles...)
	changed.Profiles[0].SyncEnv = false
	if operations, err := runtime.appendLocalRelayKey(changed, "profiles_encrypted"); err != nil || operations != 2 {
		t.Fatalf("disable profile env operations=%d err=%v", operations, err)
	}
	if env, ok := runtime.replica.Get(configsync.CollectionProfileEnv, "profile-1"); !ok || !env.Deleted {
		t.Fatalf("disabled profile env was not tombstoned: ok=%t record=%+v", ok, env)
	}
	if operations, err := runtime.appendLocalRelayKey(changed, "profiles_encrypted"); err != nil || operations != 0 {
		t.Fatalf("repeated profile update operations=%d err=%v", operations, err)
	}

	secretValue, err := configsync.CanonicalEntityJSON("host-1", sshCredentialRecord{ID: "host-1", Password: "relay-only"})
	if err != nil {
		t.Fatal(err)
	}
	secretMutation, err := configsync.SetRecordMutation(configsync.PlainRecord{
		Collection: configsync.CollectionSSHCredential, RecordID: "host-1",
		KeyClass: configsync.KeyClassVault, Value: secretValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, runtime.keys[configsync.KeyClassVault], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    secretMutation.Collection,
		RecordID:      secretMutation.RecordID,
		Kind:          secretMutation.Kind,
		Payload:       secretMutation.Payload,
	}); err != nil {
		t.Fatal(err)
	}
	changed.SSHHosts = append([]SSHHost(nil), cfg.SSHHosts...)
	changed.SSHHosts[0].Alias = "After"
	if operations, err := runtime.appendLocalRelayKey(changed, "ssh_hosts_encrypted"); err != nil || operations != 1 {
		t.Fatalf("changed SSH metadata operations=%d err=%v", operations, err)
	}
	if secret, ok := runtime.replica.Get(configsync.CollectionSSHCredential, "host-1"); !ok || secret.Deleted {
		t.Fatal("SSH metadata update deleted Relay-only credential")
	}
	if got := runtime.replica.Vector()[status.PeerID]; got != before+5 {
		t.Fatalf("local diff advanced vector to %d want=%d", got, before+5)
	}
}

func TestPeerConfigReplicaCompactsBoundedOperationTail(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	mutations := make([]configsync.RecordMutation, peerConfigCompactTailOperations)
	for index := range mutations {
		mutations[index] = configsync.RecordMutation{
			Collection: configsync.CollectionQuickTemplate,
			RecordID:   fmt.Sprintf("bounded-%04d", index),
			Kind:       configsync.KindSet,
			KeyClass:   configsync.KeyClassSync,
			Payload:    json.RawMessage(`{"command":"true","name":"Bounded"}`),
		}
	}
	if operations, err := runtime.appendPeerConfigMutations("bounded tail test", mutations); err != nil || operations != len(mutations) {
		t.Fatalf("bounded append operations=%d err=%v", operations, err)
	}
	state := runtime.replica.StateForPeer(configsync.VersionVector{})
	if state.Snapshot == "" || len(state.Ops) != 0 {
		t.Fatalf("bounded Peer state snapshot=%t ops=%d", state.Snapshot != "", len(state.Ops))
	}
	if got := len(runtime.replica.Records(configsync.CollectionQuickTemplate)); got != len(mutations) {
		t.Fatalf("compacted records=%d want=%d", got, len(mutations))
	}
}

func TestPeerConfigReplicaPrunesTombstoneAtActiveMemberAckFloor(t *testing.T) {
	app, now := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	remotes := make([]*peercrypto.Identity, 2)
	for index := range remotes {
		remotes[index], err = peercrypto.GenerateIdentity()
		if err != nil {
			t.Fatal(err)
		}
		wrapping, wrappingErr := peercrypto.GenerateWrappingIdentity()
		if wrappingErr != nil {
			t.Fatal(wrappingErr)
		}
		membership := issueTestPeerMembership(t, runtime.identity, genesis, creator, remotes[index], wrapping, now)
		if _, err := app.peerSpace.store.ApplyMemberships([]string{membership.Token}, now); err != nil {
			t.Fatal(err)
		}
	}
	key := runtime.keys[configsync.KeyClassSync]
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, key, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionQuickTemplate,
		RecordID: "removed-template", Kind: configsync.KindSet, Payload: json.RawMessage(`{"command":"true","name":"Removed"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, key, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionQuickTemplate,
		RecordID: "removed-template", Kind: configsync.KindDelete,
	}); err != nil {
		t.Fatal(err)
	}
	vector := runtime.replica.Vector()
	if pruned, err := runtime.pruneStableTombstones(); err != nil || pruned != 0 {
		t.Fatalf("unacknowledged prune=%d err=%v", pruned, err)
	}
	if err := app.peerSpace.store.RecordConfigExchange(remotes[0].PeerID(), vector, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := app.peerSpace.store.RecordConfigExchange(remotes[1].PeerID(), configsync.VersionVector{
		runtime.identity.PeerID(): vector[runtime.identity.PeerID()] - 1,
	}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if pruned, err := runtime.pruneStableTombstones(); err != nil || pruned != 0 {
		t.Fatalf("partially acknowledged prune=%d err=%v", pruned, err)
	}
	app.peerSpace.now = func() time.Time { return now.Add(2 * time.Minute) }
	channel := &peerConfigChannel{app: app, remotePeerID: remotes[1].PeerID()}
	if err := channel.recordExchange(vector); err != nil {
		t.Fatal(err)
	}
	if record, ok := runtime.replica.Get(configsync.CollectionQuickTemplate, "removed-template"); ok {
		t.Fatalf("stable tombstone still materialized: %+v", record)
	}
}

func TestPeerConfigReplicaRelayImportFiltersSecretsAndCapabilities(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	accountKey := bytes.Repeat([]byte{9}, 32)
	codec := newDesktopRelaySealedCodec(func() []byte { return accountKey })

	sshValue, err := sealSSHHosts(
		accountKey,
		[]SSHHost{{ID: "host-1", Alias: "Production", Host: "example.com", User: "alice", KeyID: "key-1"}},
		map[string]sshCredential{"host-1": {Password: "relay-password"}},
		[]SSHKey{{ID: "key-1", Name: "Primary", KeyType: "ED25519"}},
		map[string]sshKeySecret{"key-1": {PrivateKey: "relay-private-key"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	refs, operations, err := runtime.appendRelayValue(configsync.RelayValue{
		Key: "ssh_hosts_encrypted", Value: sshValue, UpdatedAt: 10,
	}, nil, codec)
	if err != nil {
		t.Fatal(err)
	}
	if operations != 2 || len(refs) != 2 {
		t.Fatalf("SSH Relay import operations=%d refs=%+v", operations, refs)
	}
	if len(runtime.replica.Records(configsync.CollectionSSHCredential)) != 0 || len(runtime.replica.Records(configsync.CollectionSSHKeySecret)) != 0 {
		t.Fatal("Relay SSH secrets entered the Peer replica")
	}

	delete(runtime.keys, configsync.KeyClassVault)
	profileValue, err := sealProfiles(accountKey, []SessionProfile{{
		ID: "profile-1", Name: "Shared", SyncEnv: true, Env: map[string]string{"TOKEN": "relay-secret"},
	}}, "profile-1")
	if err != nil {
		t.Fatal(err)
	}
	refs, operations, err = runtime.appendRelayValue(configsync.RelayValue{
		Key: "profiles_encrypted", Value: profileValue, UpdatedAt: 11,
	}, nil, codec)
	if err != nil {
		t.Fatal(err)
	}
	if operations != 2 || len(refs) != 2 {
		t.Fatalf("non-vault Profile import operations=%d refs=%+v", operations, refs)
	}
	if len(runtime.replica.Records(configsync.CollectionProfileEnv)) != 0 {
		t.Fatal("device without vault capability imported profile env")
	}
}

func TestPeerConfigReplicaPersistsRelayImportAcknowledgement(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	store, err := app.peerSpace.relayCompatibilityStore("realm-import")
	if err != nil {
		t.Fatal(err)
	}
	values := []configsync.RelayValue{
		{Key: "terminal_theme", Value: json.RawMessage(`"dark"`), UpdatedAt: 100},
		{Key: "terminal_font_size", Value: json.RawMessage(`18`), UpdatedAt: 101},
	}
	operations, importErrors, err := runtime.importRelayValues(store, values, nil)
	if err != nil || len(importErrors) != 0 || operations != 2 {
		t.Fatalf("first Relay import operations=%d errors=%v err=%v", operations, importErrors, err)
	}
	operations, importErrors, err = runtime.importRelayValues(store, values, nil)
	if err != nil || len(importErrors) != 0 || operations != 0 {
		t.Fatalf("Relay echo import operations=%d errors=%v err=%v", operations, importErrors, err)
	}

	reopened, err := app.peerSpace.relayCompatibilityStore("realm-import")
	if err != nil {
		t.Fatal(err)
	}
	compatibility, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	state := compatibility.State()
	for _, value := range values {
		entry := state.Keys[value.Key]
		if entry.RelayUpdatedAt != value.UpdatedAt || entry.RelayValueHash == "" || len(entry.RelayRecords) != 1 {
			t.Fatalf("persisted Relay state for %s=%+v", value.Key, entry)
		}
	}
}

func TestPeerConfigReplicaPlansSafeRelayExportsUntilAcknowledged(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{
		TerminalTheme: "nord",
		SSHHosts:      []SSHHost{{ID: "host-1", Alias: "Production", Host: "example.com", User: "alice"}},
	}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	store, err := app.peerSpace.relayCompatibilityStore("realm-export")
	if err != nil {
		t.Fatal(err)
	}
	codec := newDesktopRelaySealedCodec(func() []byte { return bytes.Repeat([]byte{3}, 32) })

	exports, exportErrors, err := runtime.planRelayExports(store, codec)
	if err != nil || len(exportErrors) != 0 {
		t.Fatalf("plan Relay exports errors=%v err=%v", exportErrors, err)
	}
	if len(exports) != 1 || exports[0].Key != "terminal_theme" {
		t.Fatalf("planned Relay exports=%+v", exports)
	}
	retry, exportErrors, err := runtime.planRelayExports(store, codec)
	if err != nil || len(exportErrors) != 0 || len(retry) != 1 {
		t.Fatalf("pending Relay export was not retryable: exports=%+v errors=%v err=%v", retry, exportErrors, err)
	}

	operations, importErrors, err := runtime.importRelayValues(store, exports, codec)
	if err != nil || len(importErrors) != 0 || operations != 0 {
		t.Fatalf("acknowledge Relay export operations=%d errors=%v err=%v", operations, importErrors, err)
	}
	exports, exportErrors, err = runtime.planRelayExports(store, codec)
	if err != nil || len(exportErrors) != 0 || len(exports) != 0 {
		t.Fatalf("acknowledged Relay export replanned: exports=%+v errors=%v err=%v", exports, exportErrors, err)
	}
	compatibility, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	state := compatibility.State()
	if _, exists := state.Keys["ssh_hosts_encrypted"]; exists {
		t.Fatal("unsafe SSH bundle entered Relay export state")
	}
}

func TestPeerConfigReplicaProjectsCanonicalConfigWithoutSecrets(t *testing.T) {
	app, _ := newTestPeerApp(t)
	enabled := true
	cfg := appConfig{
		TerminalTheme:        "nord",
		NotificationsEnabled: &enabled,
		QuickTemplates:       []QuickTemplate{{ID: "template-1", Label: "Build", Text: "go test ./..."}},
		Profiles: []SessionProfile{
			{ID: "profile-local-env", Name: "Local env", Env: map[string]string{"LOCAL": "creator"}},
			{ID: "profile-shared-env", Name: "Shared env", SyncEnv: true, Env: map[string]string{"SHARED": "canonical"}},
		},
		DefaultProfileID: "profile-shared-env",
		SSHHosts:         []SSHHost{{ID: "host-1", Alias: "Canonical", Host: "example.com", User: "alice", KeyID: "key-1"}},
		SSHKeys:          []SSHKey{{ID: "key-1", Name: "Canonical key", KeyType: "ED25519"}},
	}
	app.cfgStore = &configStore{cfg: cfg}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	staleEnv, err := configsync.CanonicalEntityJSON("profile-local-env", profileEnvRecord{
		ID: "profile-local-env", Env: map[string]string{"LOCAL": "stale-canonical"},
	})
	if err != nil {
		t.Fatal(err)
	}
	staleMutation, err := configsync.SetRecordMutation(configsync.PlainRecord{
		Collection: configsync.CollectionProfileEnv, RecordID: "profile-local-env",
		KeyClass: configsync.KeyClassVault, Value: staleEnv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, runtime.keys[configsync.KeyClassVault], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    staleMutation.Collection,
		RecordID:      staleMutation.RecordID,
		Kind:          staleMutation.Kind,
		Payload:       staleMutation.Payload,
	}); err != nil {
		t.Fatal(err)
	}
	withoutCollections := cfg
	withoutCollections.NotificationsEnabled = nil
	withoutCollections.QuickTemplates = nil
	if operations, err := runtime.appendLocalRelayKey(withoutCollections, "notifications_enabled"); err != nil || operations != 1 {
		t.Fatalf("clear notification operations=%d err=%v", operations, err)
	}
	if operations, err := runtime.appendLocalRelayKey(withoutCollections, "quick_templates"); err != nil || operations != 1 {
		t.Fatalf("clear templates operations=%d err=%v", operations, err)
	}
	if err := sshCredentialSlot("host-1").Save(sshCredential{Password: "local-secret"}); err != nil {
		t.Fatal(err)
	}

	disabled := false
	base := appConfig{
		TerminalTheme:        "light",
		NotificationsEnabled: &disabled,
		QuickTemplates:       []QuickTemplate{{ID: "old", Label: "Old", Text: "old"}},
		Profiles: []SessionProfile{
			{ID: "profile-local-env", Name: "Old local", Env: map[string]string{"LOCAL": "device"}},
			{ID: "profile-shared-env", Name: "Old shared", SyncEnv: true, Env: map[string]string{"SHARED": "stale"}},
			{ID: "local-only", Name: "Pending import", Env: map[string]string{"ONLY": "here"}},
		},
		DefaultProfileID: "local-only",
		SSHHosts:         []SSHHost{{ID: "old-host", Alias: "Old", Host: "old.example.com"}},
		SSHKeys:          []SSHKey{{ID: "old-key", Name: "Old key"}},
	}
	projected, projectionErrors := runtime.projectLocalConfig(base)
	if len(projectionErrors) != 0 {
		t.Fatalf("project canonical config: %v", projectionErrors)
	}
	if projected.TerminalTheme != "nord" || projected.NotificationsEnabled != nil || len(projected.QuickTemplates) != 0 {
		t.Fatalf("projected portable config=%+v", projected)
	}
	if len(projected.Profiles) != 2 || projected.DefaultProfileID != "profile-shared-env" {
		t.Fatalf("projected profiles=%+v default=%q", projected.Profiles, projected.DefaultProfileID)
	}
	if projected.Profiles[0].Env["LOCAL"] != "device" || projected.Profiles[1].Env["SHARED"] != "canonical" {
		t.Fatalf("projected profile env=%+v", projected.Profiles)
	}
	if len(projected.SSHHosts) != 1 || projected.SSHHosts[0].Alias != "Canonical" || len(projected.SSHKeys) != 1 || projected.SSHKeys[0].Name != "Canonical key" {
		t.Fatalf("projected SSH metadata hosts=%+v keys=%+v", projected.SSHHosts, projected.SSHKeys)
	}
	credential, err := sshCredentialSlot("host-1").Load()
	if err != nil || credential.Password != "local-secret" {
		t.Fatalf("projection changed SSH credential=%+v err=%v", credential, err)
	}
	if base.TerminalTheme != "light" || base.Profiles[0].Env["LOCAL"] != "device" {
		t.Fatal("projection mutated its base snapshot")
	}

	delete(runtime.keys, configsync.KeyClassVault)
	withoutVault, projectionErrors := runtime.projectLocalConfig(base)
	if len(projectionErrors) != 0 {
		t.Fatalf("project without vault capability: %v", projectionErrors)
	}
	if withoutVault.Profiles[1].Env["SHARED"] != "stale" {
		t.Fatalf("projection without vault capability replaced local env: %+v", withoutVault.Profiles[1].Env)
	}
}

func TestPendingPeerConfigIsWriteOnceCapabilityGatedAndMergeOnly(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{
		QuickTemplates: []QuickTemplate{{ID: "peer-template", Label: "Peer", Text: "peer"}},
	}}
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	pendingConfig := appConfig{
		TerminalTheme:  "local-theme",
		QuickTemplates: []QuickTemplate{{ID: "local-template", Label: "Local", Text: "local"}},
		Profiles: []SessionProfile{{
			ID: "local-profile", Name: "Local profile", SyncEnv: true,
			Env: map[string]string{"TOKEN": "pending-local-secret"},
		}},
		DefaultProfileID: "local-profile",
		SSHHosts:         []SSHHost{{ID: "local-host", Alias: "Local host", Host: "local.example.com"}},
	}
	count, captured, err := app.peerSpace.capturePendingPeerConfig(pendingConfig)
	if err != nil || !captured || count != 6 {
		t.Fatalf("capture pending config count=%d captured=%t err=%v", count, captured, err)
	}
	pending, ok, err := app.peerSpace.loadPendingPeerConfig()
	if err != nil || !ok || len(pending.Records) != count {
		t.Fatalf("load pending config records=%d ok=%t err=%v", len(pending.Records), ok, err)
	}
	storePath := strings.TrimSuffix(app.peerSpace.bootstrapLockPath, ".bootstrap.lock")
	disk, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("pending-local-secret")) || bytes.Contains(disk, []byte("local-theme")) {
		t.Fatal("pending local config was written outside the encrypted Peer store")
	}
	if repeatedCount, repeatedCapture, err := app.peerSpace.capturePendingPeerConfig(pendingConfig); err != nil || repeatedCapture || repeatedCount != count {
		t.Fatalf("idempotent pending capture count=%d captured=%t err=%v", repeatedCount, repeatedCapture, err)
	}
	replacement := pendingConfig
	replacement.TerminalTheme = "replacement"
	if _, _, err := app.peerSpace.capturePendingPeerConfig(replacement); !errors.Is(err, peerstore.ErrPendingExists) {
		t.Fatalf("pending config replacement error=%v", err)
	}

	runtime := app.peerSpace.configReplica
	vaultKey := runtime.keys[configsync.KeyClassVault]
	delete(runtime.keys, configsync.KeyClassVault)
	before := runtime.replica.Vector()[status.PeerID]
	if _, err := runtime.appendPendingPeerConfig(pending.plainRecords()); err == nil {
		t.Fatal("pending vault config imported without vault capability")
	}
	if got := runtime.replica.Vector()[status.PeerID]; got != before {
		t.Fatalf("failed pending import advanced vector to %d want=%d", got, before)
	}
	if _, ok, err := app.peerSpace.loadPendingPeerConfig(); err != nil || !ok {
		t.Fatalf("failed import removed pending config: ok=%t err=%v", ok, err)
	}

	runtime.keys[configsync.KeyClassVault] = vaultKey
	operations, err := app.peerSpace.acceptPendingPeerConfig()
	if err != nil || operations != count {
		t.Fatalf("accept pending config operations=%d err=%v", operations, err)
	}
	templates := runtime.replica.Records(configsync.CollectionQuickTemplate)
	if len(templates) != 2 {
		t.Fatalf("pending import replaced Peer templates: %+v", templates)
	}
	if len(runtime.replica.Records(configsync.CollectionSSHCredential)) != 0 || len(runtime.replica.Records(configsync.CollectionSSHKeySecret)) != 0 {
		t.Fatal("pending import included SSH secrets")
	}
	if _, ok, err := app.peerSpace.loadPendingPeerConfig(); err != nil || ok {
		t.Fatalf("accepted pending config remains: ok=%t err=%v", ok, err)
	}
	if operations, err := app.peerSpace.acceptPendingPeerConfig(); err != nil || operations != 0 {
		t.Fatalf("repeated pending accept operations=%d err=%v", operations, err)
	}
}

func TestApplyPeerConfigProjectionPreservesThenImportsLocalConfig(t *testing.T) {
	isolateConfigDir(t)
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{
		TerminalTheme:  "nord",
		QuickTemplates: []QuickTemplate{{ID: "peer-template", Label: "Peer", Text: "peer"}},
	}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	commits := 0
	app.cfgStore = &configStore{cfg: appConfig{
		TerminalTheme:  "local-theme",
		QuickTemplates: []QuickTemplate{{ID: "local-template", Label: "Local", Text: "local"}},
	}}
	app.cfgStore.setOnCommit(func(appConfig) { commits++ })

	pendingRecords, err := app.applyPeerConfigProjection(true)
	if err != nil || pendingRecords != 2 {
		t.Fatalf("initial projection pending=%d err=%v", pendingRecords, err)
	}
	projected := app.cfgStore.Get()
	if projected.TerminalTheme != "nord" || len(projected.QuickTemplates) != 1 || projected.QuickTemplates[0].ID != "peer-template" {
		t.Fatalf("initial projected config=%+v", projected)
	}
	if commits != 1 {
		t.Fatalf("initial projection commits=%d want=1", commits)
	}
	if _, ok, err := app.peerSpace.loadPendingPeerConfig(); err != nil || !ok {
		t.Fatalf("pre-join config not pending: ok=%t err=%v", ok, err)
	}

	operations, err := app.peerSpace.acceptPendingPeerConfig()
	if err != nil || operations != 2 {
		t.Fatalf("accept pre-join config operations=%d err=%v", operations, err)
	}
	if pendingRecords, err := app.applyPeerConfigProjection(false); err != nil || pendingRecords != 0 {
		t.Fatalf("post-import projection pending=%d err=%v", pendingRecords, err)
	}
	merged := app.cfgStore.Get()
	if merged.TerminalTheme != "local-theme" || len(merged.QuickTemplates) != 2 {
		t.Fatalf("merged projected config=%+v", merged)
	}
	ids := map[string]bool{}
	for _, template := range merged.QuickTemplates {
		ids[template.ID] = true
	}
	if !ids["peer-template"] || !ids["local-template"] {
		t.Fatalf("merged templates=%+v", merged.QuickTemplates)
	}
	if commits != 2 {
		t.Fatalf("total projection commits=%d want=2", commits)
	}
}

func TestApplyPeerConfigProjectionRejectsPartialCommit(t *testing.T) {
	isolateConfigDir(t)
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	unknownEpoch, err := configsync.GenerateEpochKey(configsync.KeyClassSync, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, unknownEpoch, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_theme",
		Kind:          configsync.KindSet,
		Payload:       json.RawMessage(`"dark"`),
	}); err != nil {
		t.Fatal(err)
	}
	commits := 0
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	app.cfgStore.setOnCommit(func(appConfig) { commits++ })
	if _, err := app.applyPeerConfigProjection(false); !errors.Is(err, configsync.ErrInvalidEpochKey) {
		t.Fatalf("projection error=%v", err)
	}
	if commits != 0 || app.cfgStore.Get().TerminalTheme != "local-theme" {
		t.Fatalf("failed projection committed config=%+v commits=%d", app.cfgStore.Get(), commits)
	}
}

func TestCorruptPeerConfigReplicaDoesNotSetStartupFatal(t *testing.T) {
	app, _ := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	syncKey := runtime.keys[configsync.KeyClassSync]
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_theme",
		Kind:          configsync.KindSet,
		Payload:       []byte(`"dark"`),
	}); err != nil {
		t.Fatal(err)
	}
	replicaPath := filepath.Join(app.peerSpace.configRoot, status.SpaceID, "config-replica.json")
	if err := os.WriteFile(replicaPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.peerSpace.configReplica = nil

	app.restorePeerConfigReplica()
	if startup := app.GetStartupError(); startup.Fatal {
		t.Fatalf("Peer replica failure became startup-fatal: %+v", startup)
	}
}

func TestPeerRelayCompatibilityStateSurvivesRestart(t *testing.T) {
	app, _ := newTestPeerApp(t)
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	store, err := app.peerSpace.relayCompatibilityStore("realm-test")
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Update(func(compatibility *configsync.RelayCompatibility) error {
		compatibility.SetMigrationMarkers(true, false)
		_, errs := compatibility.PlanExports([]configsync.RelayValue{{
			Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100,
		}})
		if len(errs) != 0 {
			return errs[0]
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app.peerSpace.configReplica = nil
	reopenedStore, err := app.peerSpace.relayCompatibilityStore("realm-test")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := reopenedStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	restored := reopened.State()
	if !restored.LocalSeeded || restored.Keys["terminal_theme"].PendingExportHash != state.Keys["terminal_theme"].PendingExportHash {
		t.Fatalf("restored Relay compatibility state=%+v", restored)
	}
}

func TestCreateAndRevokePeerInvitationBatch(t *testing.T) {
	app, now := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{
		Count: 3, ValidForHours: 24, Permission: "control",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invitations) != 3 {
		t.Fatalf("invitations = %d", len(invitations))
	}
	if invitations[0].BatchID == "" || invitations[1].BatchID != invitations[0].BatchID {
		t.Fatal("invitation batch ids differ")
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := peerproto.VerifyInvitation(invitations[0].Token, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if doc.RedemptionPeerID != status.PeerID || doc.Permission != peerproto.PermissionControl {
		t.Fatalf("unexpected ticket: %+v", doc)
	}
	if err := app.RevokePeerInvitationBatch(invitations[0].BatchID); err != nil {
		t.Fatal(err)
	}
	state, err = app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 1 {
		t.Fatalf("signed revocations=%d want=1", len(state.Revocations))
	}
	revocation, err := peerproto.VerifyRevocation(state.Revocations[0], genesis)
	if err != nil {
		t.Fatal(err)
	}
	if revocation.Document.Kind != peerproto.RevocationInvitationBatch || revocation.Document.TargetID != invitations[0].BatchID {
		t.Fatalf("unexpected batch revocation: %+v", revocation.Document)
	}
	if err := app.RevokePeerInvitationBatch(invitations[0].BatchID); err != nil {
		t.Fatal(err)
	}
	state, err = app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 1 {
		t.Fatalf("idempotent batch revoke stored %d tokens", len(state.Revocations))
	}
	status, err = app.GetPeerSpaceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.RevokedInvites != 3 || status.OpenInvitations != 0 {
		t.Fatalf("status after revoke: %+v", status)
	}
	listed, err := app.ListPeerInvitations()
	if err != nil {
		t.Fatal(err)
	}
	for _, invite := range listed {
		if invite.Token != "" {
			t.Fatal("revoked invitation still exposes its pairing secret")
		}
	}
	if err := app.RevokePeerInvitationBatch("db8f16c4-8bf5-47b6-8672-b5b75a70d82d"); !errors.Is(err, peerstore.ErrInviteInvalid) {
		t.Fatalf("unknown batch error=%v", err)
	}
}

func TestPeerInvitationsRequireSpace(t *testing.T) {
	app, _ := newTestPeerApp(t)
	if _, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1}); !errors.Is(err, peerstore.ErrNotInitialized) {
		t.Fatalf("CreatePeerInvitations without space error = %v", err)
	}
}

func TestRedeemPeerJoinRequestIsIdempotentAndRejectsReplay(t *testing.T) {
	app, now := newTestPeerApp(t)
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	invitations, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1, Permission: "control"})
	if err != nil {
		t.Fatal(err)
	}
	joiningIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joiningWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	request, err := peerproto.NewJoinRequest(joiningIdentity, joiningWrapping.PublicBytes(), invitations[0].Token, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.peerSpace.redeemJoinRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.peerSpace.redeemJoinRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	firstStable, secondStable := first, second
	firstStable.EpochEnvelopes = nil
	secondStable.EpochEnvelopes = nil
	if first.GenesisToken == "" || first.MembershipToken == "" || !reflect.DeepEqual(secondStable, firstStable) {
		t.Fatalf("idempotent join results differ: first=%+v second=%+v", first, second)
	}
	genesis, err := peerproto.VerifyGenesis(first.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(first.MembershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Document.SubjectPeerID != joiningIdentity.PeerID() || membership.Document.Permission != peerproto.PermissionControl {
		t.Fatalf("unexpected joined membership: %+v", membership.Document)
	}
	if !bytes.Equal(membership.WrappingPublicKey, joiningWrapping.PublicBytes()) {
		t.Fatal("joined membership did not retain wrapping public key")
	}
	if len(first.EpochRotations) != 2 || len(first.EpochEnvelopes) != 1 {
		t.Fatalf("join bootstrap rotations=%d envelopes=%d", len(first.EpochRotations), len(first.EpochEnvelopes))
	}
	rotations, err := currentEpochRotations(first.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	key, err := configsync.OpenEpochKey(first.EpochEnvelopes[0], genesis.Document.SpaceID, joiningIdentity.PeerID(), joiningWrapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := configsync.ValidateEpochKeyForRotation(key, rotations[configsync.KeyClassSync]); err != nil {
		t.Fatalf("bootstrap envelope key does not match signed rotation: %v", err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, token := range state.Memberships {
		found = found || token == first.MembershipToken
	}
	if !found {
		t.Fatal("redeemed membership was not added to the directory")
	}

	replayIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replayWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := peerproto.NewJoinRequest(replayIdentity, replayWrapping.PublicBytes(), invitations[0].Token, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.peerSpace.redeemJoinRequest(replay); !errors.Is(err, peerstore.ErrInviteConsumed) {
		t.Fatalf("cross-device replay error = %v", err)
	}
}
