package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	if first.GenesisToken == "" || first.MembershipToken == "" || second != first {
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
