package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
)

func TestPeerConfigSyncAdoptsAuthorizedSnapshotAndTailOnce(t *testing.T) {
	isolateConfigDir(t)
	source, _ := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	status, err := source.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity)
	if err != nil {
		t.Fatal(err)
	}
	syncKey := source.peerSpace.configReplica.keys[configsync.KeyClassSync]
	if _, _, err := source.peerSpace.configReplica.replica.AppendEncrypted(source.peerSpace.configReplica.identity, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences, RecordID: "terminal_font_size",
		Kind: configsync.KindSet, Payload: json.RawMessage(`17`),
	}); err != nil {
		t.Fatal(err)
	}

	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme", TerminalFontSize: 12}}
	commits := 0
	destination.cfgStore.setOnCommit(func(appConfig) { commits++ })
	state, _ := source.peerSpace.store.Load()
	receiver, err := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	batch := peerConfigSyncBatch(t, source, configsync.VersionVector{}, nil, state.EpochRotations)
	ack, err := receiver.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Done || ack.Durable.SpaceID != status.SpaceID || ack.Durable.Vector[status.PeerID] == 0 || ack.PendingLocalRecords == 0 {
		t.Fatalf("unexpected sync ack: %+v", ack)
	}
	projected := destination.cfgStore.Get()
	if projected.TerminalTheme != "nord" || projected.TerminalFontSize != 17 || commits != 1 {
		t.Fatalf("projected=%+v commits=%d", projected, commits)
	}
	if _, ok, err := destination.peerSpace.loadPendingPeerConfig(); err != nil || !ok {
		t.Fatalf("pre-join config pending=%t err=%v", ok, err)
	}
	replayed, err := receiver.Add(batch)
	if err != nil || !reflect.DeepEqual(replayed, ack) || commits != 1 {
		t.Fatalf("replay ack=%+v err=%v commits=%d", replayed, err, commits)
	}
	if current := destination.peerSpace.configReplica.replica.StateForPeer(configsync.VersionVector{}); current.Snapshot != snapshot {
		t.Fatal("destination did not retain the authorized snapshot")
	}
}

func TestPeerConfigSyncRejectsUnknownSnapshotCreatorBeforeCapture(t *testing.T) {
	isolateConfigDir(t)
	source, now := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{}}
	status, err := source.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	state, _ := source.peerSpace.store.Load()
	genesis, _ := peerproto.VerifyGenesis(state.GenesisToken)
	outsider, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replica, err := configsync.NewReplica(status.SpaceID, configsync.SchemaVersion, configsync.NewClockWithSource(func() time.Time { return now }, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replica.Append(outsider, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionPreferences,
		RecordID: "terminal_theme", Kind: configsync.KindSet,
		KeyClass: configsync.KeyClassSync, KeyEpoch: 1, Payload: []byte(`"hostile"`),
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := replica.SignSnapshot(outsider)
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := configsync.BuildAntiEntropyInventory(genesis, configsync.VersionVector{}, nil, nil, nil)
	plan, err := configsync.NewAntiEntropyPlan(genesis, configsync.SyncState{
		Snapshot: snapshot,
		Ack:      configsync.DurableAck{SpaceID: status.SpaceID, Vector: replica.Vector()},
		Ops:      []string{},
	}, nil, nil, nil, remote, configsync.MaxAntiEntropyBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	batch, _ := plan.Next(nil)
	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	receiver, err := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Add(batch); !errors.Is(err, errPeerConfigSyncDenied) {
		t.Fatalf("unknown snapshot creator error=%v", err)
	}
	if destination.cfgStore.Get().TerminalTheme != "local-theme" {
		t.Fatal("rejected snapshot changed local config")
	}
	if _, ok, err := destination.peerSpace.loadPendingPeerConfig(); err != nil || ok {
		t.Fatalf("rejected snapshot created pending config: ok=%t err=%v", ok, err)
	}
}

func TestPeerConfigSyncRejectsRevokedRemoteMembership(t *testing.T) {
	source, now := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{}}
	status, err := source.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	state, _ := source.peerSpace.store.Load()
	genesis, _ := peerproto.VerifyGenesis(state.GenesisToken)
	membership, _ := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	identity, _ := source.peerSpace.loadIdentity()
	revocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationMember, status.PeerID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.peerSpace.store.ApplyRevocations([]string{revocation.Token}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := source.newPeerConfigSyncReceiver(state.LocalMembership); !errors.Is(err, errPeerConfigSyncDenied) {
		t.Fatalf("revoked remote error=%v", err)
	}
}

func TestPeerConfigSyncFailedAdoptionDoesNotCaptureOrProject(t *testing.T) {
	isolateConfigDir(t)
	source, _ := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity); err != nil {
		t.Fatal(err)
	}
	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	runtime, err := destination.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	key := runtime.keys[configsync.KeyClassSync]
	localIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.AppendEncrypted(localIdentity, key, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionPreferences,
		RecordID: "terminal_theme", Kind: configsync.KindSet, Payload: json.RawMessage(`"destination"`),
	}); err != nil {
		t.Fatal(err)
	}
	state, _ := source.peerSpace.store.Load()
	receiver, _ := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	batch := peerConfigSyncBatch(t, source, configsync.VersionVector{}, nil, nil)
	if _, err := receiver.Add(batch); !errors.Is(err, configsync.ErrSnapshotNotCovered) {
		t.Fatalf("non-empty adoption error=%v", err)
	}
	if destination.cfgStore.Get().TerminalTheme != "local-theme" {
		t.Fatal("failed adoption projected local config")
	}
	if _, ok, err := destination.peerSpace.loadPendingPeerConfig(); err != nil || ok {
		t.Fatalf("failed adoption captured pending config: ok=%t err=%v", ok, err)
	}
}

func TestPeerConfigSyncRebasesCoveredExistingSnapshot(t *testing.T) {
	isolateConfigDir(t)
	source, _ := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	firstSnapshot, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity)
	if err != nil {
		t.Fatal(err)
	}

	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme", TerminalFontSize: 12}}
	destinationRuntime, err := destination.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destinationRuntime.replica.AdoptSnapshot(firstSnapshot); err != nil {
		t.Fatal(err)
	}

	sourceRuntime := source.peerSpace.configReplica
	if _, _, err := sourceRuntime.replica.AppendEncrypted(sourceRuntime.identity, sourceRuntime.keys[configsync.KeyClassSync], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_font_size",
		Kind:          configsync.KindSet,
		Payload:       json.RawMessage(`18`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sourceRuntime.replica.Compact(sourceRuntime.identity); err != nil {
		t.Fatal(err)
	}

	state, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	batch := peerConfigSyncBatch(t, source, destinationRuntime.replica.Vector(), nil, nil)
	ack, err := receiver.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Done || destination.cfgStore.Get().TerminalTheme != "nord" || destination.cfgStore.Get().TerminalFontSize != 18 {
		t.Fatalf("rebased config ack=%+v config=%+v", ack, destination.cfgStore.Get())
	}
}

func TestPeerConfigSyncOperationOnlyProjectsWithoutPreJoinCapture(t *testing.T) {
	isolateConfigDir(t)
	source, _ := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity)
	if err != nil {
		t.Fatal(err)
	}
	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	destinationRuntime, err := destination.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destinationRuntime.replica.AdoptSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	key := source.peerSpace.configReplica.keys[configsync.KeyClassSync]
	if _, _, err := source.peerSpace.configReplica.replica.AppendEncrypted(source.peerSpace.configReplica.identity, key, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionPreferences,
		RecordID: "terminal_theme", Kind: configsync.KindSet, Payload: json.RawMessage(`"solarized"`),
	}); err != nil {
		t.Fatal(err)
	}
	state, _ := source.peerSpace.store.Load()
	receiver, _ := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	batch := peerConfigSyncBatch(t, source, destinationRuntime.replica.Vector(), nil, nil)
	ack, err := receiver.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Done || ack.PendingLocalRecords != 0 || destination.cfgStore.Get().TerminalTheme != "solarized" {
		t.Fatalf("operation-only sync ack=%+v config=%+v", ack, destination.cfgStore.Get())
	}
	if _, ok, err := destination.peerSpace.loadPendingPeerConfig(); err != nil || ok {
		t.Fatalf("operation-only sync captured pending config: ok=%t err=%v", ok, err)
	}
}

func TestPeerConfigSyncRejectsOperationFromUnknownActor(t *testing.T) {
	isolateConfigDir(t)
	source, now := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	status, err := source.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity)
	if err != nil {
		t.Fatal(err)
	}
	destination := newPeerConfigSyncDestination(t, source)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	runtime, err := destination.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.replica.AdoptSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	outsider, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	token, err := configsync.SignEncryptedOp(
		outsider, status.SpaceID, 1,
		configsync.Timestamp{PhysicalMS: now.UnixMilli()}, runtime.replica.Vector(),
		source.peerSpace.configReplica.keys[configsync.KeyClassSync],
		configsync.Mutation{
			SchemaVersion: configsync.SchemaVersion, Collection: configsync.CollectionPreferences,
			RecordID: "terminal_theme", Kind: configsync.KindSet, Payload: json.RawMessage(`"hostile"`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := configsync.VerifyOp(token)
	if err != nil {
		t.Fatal(err)
	}
	ackVector := runtime.replica.Vector()
	ackVector[outsider.PeerID()] = 1
	state, _ := source.peerSpace.store.Load()
	genesis, _ := peerproto.VerifyGenesis(state.GenesisToken)
	remote, _ := configsync.BuildAntiEntropyInventory(genesis, runtime.replica.Vector(), nil, nil, nil)
	plan, err := configsync.NewAntiEntropyPlan(genesis, configsync.SyncState{
		Ops: []string{operation.Token}, Ack: configsync.DurableAck{SpaceID: status.SpaceID, Vector: ackVector},
	}, nil, nil, nil, remote, configsync.MaxAntiEntropyBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	receiver, _ := destination.newPeerConfigSyncReceiver(state.LocalMembership)
	if _, err := receiver.Add(batch); !errors.Is(err, errPeerConfigSyncDenied) {
		t.Fatalf("unknown operation actor error=%v", err)
	}
	if destination.cfgStore.Get().TerminalTheme != "local-theme" {
		t.Fatal("unauthorized operation projected local config")
	}
}

func TestPeerConfigSyncPersistsSiblingMembershipBeforeItsOperation(t *testing.T) {
	isolateConfigDir(t)
	source, now := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	sourceState, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(sourceState.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	creatorMembership, err := peerproto.VerifyGrant(sourceState.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	creatorIdentity, err := source.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := peerproto.NewInvitationBatch(creatorIdentity, genesis, creatorMembership, now, peerproto.InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	siblingWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joinToken, err := peerproto.NewJoinRequest(sibling, siblingWrapping.PublicBytes(), tickets[0], now)
	if err != nil {
		t.Fatal(err)
	}
	join, err := peerproto.VerifyJoinRequest(joinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	siblingToken, err := peerproto.IssueMembership(creatorIdentity, genesis, creatorMembership, join, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.peerSpace.store.ApplyMemberships([]string{siblingToken}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity); err != nil {
		t.Fatal(err)
	}
	syncKey := source.peerSpace.configReplica.keys[configsync.KeyClassSync]
	if _, _, err := source.peerSpace.configReplica.replica.AppendEncrypted(sibling, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences, RecordID: "terminal_theme",
		Kind: configsync.KindSet, Payload: json.RawMessage(`"sibling-theme"`),
	}); err != nil {
		t.Fatal(err)
	}
	sourceState, err = source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	destination := newIndependentPeerConfigSyncDestination(t, source, sourceState)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}
	receiver, err := destination.newPeerConfigSyncReceiver(sourceState.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	destinationState, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := configsync.BuildAntiEntropyInventory(genesis, configsync.VersionVector{}, destinationState.Memberships, nil, destinationState.EpochRotations)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := source.peerSpace.configReplica.replica.PlanAntiEntropy(
		genesis, sourceState.Memberships, nil, sourceState.EpochRotations,
		remote, configsync.MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := receiver.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Done || destination.cfgStore.Get().TerminalTheme != "sibling-theme" {
		t.Fatalf("ack=%+v config=%+v", ack, destination.cfgStore.Get())
	}
	persisted, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, token := range persisted.Memberships {
		found = found || token == siblingToken
	}
	if !found {
		t.Fatal("sibling membership was not persisted before operation authorization")
	}
	revocation, err := peerproto.NewRevocation(
		creatorIdentity, genesis, creatorMembership, peerproto.RevocationMember,
		sibling.PeerID(), now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destination.peerSpace.store.ApplyRevocations([]string{revocation.Token}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.peerSpace.configReplica.replica.AppendEncrypted(sibling, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences, RecordID: "terminal_font_size",
		Kind: configsync.KindSet, Payload: json.RawMessage(`19`),
	}); err != nil {
		t.Fatal(err)
	}
	destinationRuntime, err := destination.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err = destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	remote, err = configsync.BuildAntiEntropyInventory(
		genesis, destinationRuntime.replica.Vector(), persisted.Memberships,
		persisted.Revocations, persisted.EpochRotations,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = source.peerSpace.configReplica.replica.PlanAntiEntropy(
		genesis, sourceState.Memberships, nil, sourceState.EpochRotations,
		remote, configsync.MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err = destination.newPeerConfigSyncReceiver(sourceState.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Add(batch); !errors.Is(err, errPeerConfigSyncDenied) {
		t.Fatalf("revoked sibling operation error=%v", err)
	}
	if destination.cfgStore.Get().TerminalFontSize == 19 {
		t.Fatal("revoked sibling operation was projected")
	}
}

func TestPeerConfigSyncRejectsMembershipSerialForkAtomically(t *testing.T) {
	source, now := newTestPeerApp(t)
	source.cfgStore = &configStore{cfg: appConfig{}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	sourceState, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(sourceState.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := source.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	creatorMembership, err := peerproto.VerifyGrant(sourceState.LocalMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	siblingWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	membership := issueTestPeerMembership(t, creator, genesis, creatorMembership, sibling, siblingWrapping, now)
	forkDoc := membership.Document
	forkDoc.Permission = peerproto.PermissionView
	raw, err := json.Marshal(forkDoc)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := creator.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	fork := "apm1." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature)
	destination := newIndependentPeerConfigSyncDestination(t, source, sourceState)
	receiver, err := destination.newPeerConfigSyncReceiver(sourceState.LocalMembership)
	if err != nil {
		t.Fatal(err)
	}
	destinationState, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := configsync.BuildAntiEntropyInventory(genesis, configsync.VersionVector{}, destinationState.Memberships, nil, destinationState.EpochRotations)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := configsync.NewAntiEntropyPlan(
		genesis,
		configsync.SyncState{Ack: configsync.DurableAck{SpaceID: genesis.Document.SpaceID, Vector: configsync.VersionVector{}}},
		[]string{membership.Token, fork}, nil, nil, remote, configsync.MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Add(batch); !errors.Is(err, peerstore.ErrMembershipFork) {
		t.Fatalf("membership fork error=%v", err)
	}
	persisted, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Memberships) != 1 || persisted.Memberships[0] != sourceState.LocalMembership {
		t.Fatalf("fork batch partially persisted: %v", persisted.Memberships)
	}
}

func newPeerConfigSyncDestination(t *testing.T, source *App) *App {
	t.Helper()
	manager := &peerSpaceManager{
		store: source.peerSpace.store, now: source.peerSpace.now,
		bootstrapLockPath: filepath.Join(t.TempDir(), "peer-space.bootstrap.lock"),
		configRoot:        filepath.Join(t.TempDir(), "peer-spaces"),
	}
	return &App{peerSpace: manager}
}

func newIndependentPeerConfigSyncDestination(t *testing.T, source *App, sourceState peerstore.State) *App {
	t.Helper()
	key, err := peerStoreKeySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(t.TempDir(), "peer-space.json")
	store := peerstore.New(storePath, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	if err := store.Initialize(peerstore.State{
		GenesisToken: sourceState.GenesisToken, LocalMembership: sourceState.LocalMembership,
		EpochRotations: append([]string(nil), sourceState.EpochRotations...), CreatedAt: sourceState.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	manager := &peerSpaceManager{
		store: store, now: source.peerSpace.now,
		bootstrapLockPath: storePath + ".bootstrap.lock",
		configRoot:        filepath.Join(t.TempDir(), "peer-spaces"),
	}
	return &App{peerSpace: manager}
}

func peerConfigSyncBatch(t *testing.T, source *App, vector configsync.VersionVector, revocations, rotations []string) configsync.AntiEntropyBatch {
	t.Helper()
	state, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := configsync.BuildAntiEntropyInventory(genesis, vector, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := source.peerSpace.configReplica.replica.PlanAntiEntropy(genesis, state.Memberships, revocations, rotations, remote, configsync.MaxAntiEntropyBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.Done {
		t.Fatal("test anti-entropy state unexpectedly requires multiple batches")
	}
	return batch
}
