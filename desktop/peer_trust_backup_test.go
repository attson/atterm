package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerbackup"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/google/uuid"
)

const testPeerBackupPassphrase = "correct horse battery staple"

func TestPeerTrustBackupRestoresIdentityWithFreshStoreAndDerivedEpochKeys(t *testing.T) {
	source, now := newTestPeerApp(t)
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1, ValidForHours: 24, Permission: "control"}); err != nil {
		t.Fatal(err)
	}
	sourceIdentity, err := peerIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceWrapping, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceStoreKey, err := peerStoreKeySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceStatus, err := source.GetPeerSpaceStatus()
	if err != nil {
		t.Fatal(err)
	}
	sourceSyncKey, err := loadPeerEpochKey(sourceStatus.SpaceID, configsync.KeyClassSync)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := source.buildPeerTrustBackup(testPeerBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{sourceIdentity, sourceWrapping, sourceStoreKey, sourceSyncKey.Bytes()} {
		if bytes.Contains(backup, secret) || strings.Contains(string(backup), base64.StdEncoding.EncodeToString(secret)) {
			t.Fatal("Peer recovery package exposes secure-storage material")
		}
	}
	if strings.Contains(string(backup), "signing_private_key") || strings.Contains(string(backup), "epoch_rotations") {
		t.Fatal("Peer recovery package exposes plaintext payload fields")
	}

	clearTestPeerTrustSlots(t, sourceStatus.SpaceID)
	destination := newPeerTrustImportApp(t, now)
	restored, err := destination.ImportPeerTrustBackup(string(backup), testPeerBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PeerID != sourceStatus.PeerID || restored.SpaceID != sourceStatus.SpaceID || restored.GenesisHash != sourceStatus.GenesisHash {
		t.Fatalf("restored status=%+v source=%+v", restored, sourceStatus)
	}
	restoredIdentity, _ := peerIdentitySlot().Load()
	restoredWrapping, _ := peerWrappingIdentitySlot().Load()
	restoredStoreKey, _ := peerStoreKeySlot().Load()
	if !bytes.Equal(restoredIdentity, sourceIdentity) || !bytes.Equal(restoredWrapping, sourceWrapping) {
		t.Fatal("restored Peer identities changed")
	}
	if len(restoredStoreKey) != 32 || bytes.Equal(restoredStoreKey, sourceStoreKey) {
		t.Fatal("Peer store key was exported instead of regenerated")
	}
	restoredSyncKey, err := loadPeerEpochKey(restored.SpaceID, configsync.KeyClassSync)
	if err != nil || !bytes.Equal(restoredSyncKey.Bytes(), sourceSyncKey.Bytes()) {
		t.Fatalf("derived sync epoch key=%x err=%v", restoredSyncKey.Bytes(), err)
	}
	state, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Invitations) != 0 || len(state.ConfigSyncPeers) != 0 || len(state.PendingConfigImport) != 0 ||
		len(state.RevokedMembers) != 0 || len(state.RevokedGrantSerials) != 0 {
		t.Fatalf("live replica state leaked into recovery point: %+v", state)
	}
}

type peerRecoveryConfigLoopback struct {
	remoteMembership string
	localKeyring     string
	remoteKeyring    string
	peer             *peerConfigChannel
}

func (l *peerRecoveryConfigLoopback) RemoteMembershipToken() (string, bool) {
	return l.remoteMembership, l.remoteMembership != ""
}

func (l *peerRecoveryConfigLoopback) SendConfigMessage(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	if l.peer == nil {
		return errors.New("recovery config loopback is not connected")
	}
	safekeyring.SetFileDirForTest(l.remoteKeyring)
	defer safekeyring.SetFileDirForTest(l.localKeyring)
	return l.peer.Handle(ctx, kind, append([]byte(nil), payload...))
}

func TestPeerTrustBackupRestoredMemberAuthenticatesAndSyncsConfig(t *testing.T) {
	fixture := newPeerJoinFixture(t)
	joined, err := fixture.destination.JoinPeerSpace(JoinPeerSpaceReq{
		ConnectionBundle: fixture.bundle, ExpectedFingerprint: fixture.fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := fixture.destination.buildPeerTrustBackup(testPeerBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	sourceKeyring := peerTestKeyringPath(fixture.source)
	safekeyring.SetFileDirForTest(sourceKeyring)
	sourceState, err := fixture.source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceRuntime, err := fixture.source.peerSpace.ensureConfigReplica()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sourceRuntime.replica.AppendEncrypted(sourceRuntime.identity, sourceRuntime.keys[configsync.KeyClassSync], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_font_size",
		Kind:          configsync.KindSet,
		Payload:       json.RawMessage(`21`),
	}); err != nil {
		t.Fatal(err)
	}
	sourceIdentity, err := fixture.source.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}

	recoveryRoot := t.TempDir()
	recoveryKeyring := filepath.Join(recoveryRoot, "keyring")
	safekeyring.SetFileDirForTest(recoveryKeyring)
	recovered := newPeerTrustImportAppAt(t, fixture.now, filepath.Join(recoveryRoot, "install"))
	recovered.cfgStore = &configStore{cfg: appConfig{TerminalFontSize: 12}}
	recoveredStatus, err := recovered.ImportPeerTrustBackup(string(backup), testPeerBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredStatus.PeerID != joined.PeerID || recoveredStatus.SpaceID != joined.SpaceID {
		t.Fatalf("recovered status=%+v joined=%+v", recoveredStatus, joined)
	}
	recoveredIdentity, err := recovered.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	recoveredState, err := recovered.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}

	clientRemote, hostRemote := authenticateRecoveredPeer(t, fixture.now, recoveredIdentity, sourceIdentity, recoveredState, sourceState)
	if clientRemote != sourceState.LocalMembership || hostRemote != recoveredState.LocalMembership {
		t.Fatal("authenticated handshake returned an unexpected membership")
	}

	recoveredTransport := &peerRecoveryConfigLoopback{
		remoteMembership: clientRemote, localKeyring: recoveryKeyring, remoteKeyring: sourceKeyring,
	}
	sourceTransport := &peerRecoveryConfigLoopback{
		remoteMembership: hostRemote, localKeyring: sourceKeyring, remoteKeyring: recoveryKeyring,
	}
	safekeyring.SetFileDirForTest(recoveryKeyring)
	recoveredChannel, err := newPeerConfigChannel(recovered, recoveredTransport)
	if err != nil {
		t.Fatal(err)
	}
	safekeyring.SetFileDirForTest(sourceKeyring)
	sourceChannel, err := newPeerConfigChannel(fixture.source, sourceTransport)
	if err != nil {
		t.Fatal(err)
	}
	recoveredTransport.peer = sourceChannel
	sourceTransport.peer = recoveredChannel

	safekeyring.SetFileDirForTest(recoveryKeyring)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := recoveredChannel.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := recovered.cfgStore.Get().TerminalFontSize; got != 21 {
		t.Fatalf("recovered config font size=%d want=21", got)
	}

	for _, side := range []struct {
		name       string
		app        *App
		keyring    string
		remotePeer string
	}{
		{name: "source", app: fixture.source, keyring: sourceKeyring, remotePeer: recoveredStatus.PeerID},
		{name: "recovered", app: recovered, keyring: recoveryKeyring, remotePeer: sourceIdentity.PeerID()},
	} {
		safekeyring.SetFileDirForTest(side.keyring)
		state, err := side.app.peerSpace.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if exchange, ok := state.ConfigSyncPeers[side.remotePeer]; !ok || exchange.LastExchangeAt != fixture.now.Unix() {
			t.Fatalf("%s durable exchange=%+v present=%t", side.name, exchange, ok)
		}
	}
}

func authenticateRecoveredPeer(
	t *testing.T,
	now time.Time,
	clientIdentity, hostIdentity *peercrypto.Identity,
	clientState, hostState peerstore.State,
) (clientRemoteMembership, hostRemoteMembership string) {
	t.Helper()
	clientAuth, err := peertransport.NewPeerMembershipAuthenticator(
		clientIdentity, peertransport.RoleClient, clientState.GenesisToken,
		clientState.LocalMembership, hostState.LocalMembership, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	hostAuth, err := peertransport.NewPeerMembershipAuthenticator(
		hostIdentity, peertransport.RoleHost, hostState.GenesisToken,
		clientState.LocalMembership, hostState.LocalMembership, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	authorization := peertransport.Authorization{
		AttemptID: uuid.New(), Ticket: bytes.Repeat([]byte{0x42}, 32), SessionID: peertransport.ConfigSyncSessionID(),
		UserID: clientIdentity.PeerID(), HostID: hostIdentity.PeerID(), ClientInstanceID: "recovered-install",
		Permission: peertransport.PermissionControl, ExpiresAtUnixMillis: uint64(time.Now().Add(time.Minute).UnixMilli()),
	}
	client, err := peertransport.NewClientHandshake(clientAuth, authorization)
	if err != nil {
		t.Fatal(err)
	}
	host, err := peertransport.NewHostHandshake(hostAuth, authorization)
	if err != nil {
		t.Fatal(err)
	}
	clientHello, err := client.ClientHello()
	if err != nil {
		t.Fatal(err)
	}
	hostHello, err := host.Handle(clientHello)
	if err != nil {
		t.Fatal(err)
	}
	clientFinish, err := client.Handle(hostHello.Response)
	if err != nil {
		t.Fatal(err)
	}
	hostResult, err := host.Handle(clientFinish.Response)
	if err != nil {
		t.Fatal(err)
	}
	clientResult, err := client.Handle(hostResult.Response)
	if err != nil {
		t.Fatal(err)
	}
	if !clientResult.Authenticated || !hostResult.Authenticated || clientResult.TrafficKeys != hostResult.TrafficKeys {
		t.Fatal("restored Peer membership handshake did not authenticate both sides")
	}
	return clientAuth.RemoteMembershipToken(), hostAuth.RemoteMembershipToken()
}

func TestPeerTrustBackupImportFailsClosed(t *testing.T) {
	source, now := newTestPeerApp(t)
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	status, err := source.GetPeerSpaceStatus()
	if err != nil {
		t.Fatal(err)
	}
	backup, err := source.buildPeerTrustBackup(testPeerBackupPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ImportPeerTrustBackup(string(backup), testPeerBackupPassphrase); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("overwrite existing Peer Space error=%v", err)
	}

	clearTestPeerTrustSlots(t, status.SpaceID)
	destination := newPeerTrustImportApp(t, now)
	plaintext, err := peerbackup.Open(testPeerBackupPassphrase, backup)
	if err != nil {
		t.Fatal(err)
	}
	var payload peerTrustBackupPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatal(err)
	}
	clear(plaintext)
	payload.State.Invitations = []peerstore.Invitation{{InviteID: "must-not-restore"}}
	modifiedPlaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	modified, err := peerbackup.Seal(testPeerBackupPassphrase, modifiedPlaintext)
	clear(modifiedPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destination.ImportPeerTrustBackup(string(modified), testPeerBackupPassphrase); err == nil || !strings.Contains(err.Error(), "live or derived") {
		t.Fatalf("live-state recovery error=%v", err)
	}
	if occupied, err := peerTrustSlotsOccupied(); err != nil || occupied {
		t.Fatalf("rejected live-state import occupied=%t err=%v", occupied, err)
	}
	if _, err := destination.ImportPeerTrustBackup(string(backup), "wrong passphrase value"); !errors.Is(err, peerbackup.ErrBadPassphrase) {
		t.Fatalf("wrong passphrase error=%v", err)
	}
	if occupied, err := peerTrustSlotsOccupied(); err != nil || occupied {
		t.Fatalf("failed import secure-storage state occupied=%t err=%v", occupied, err)
	}
	storePath := strings.TrimSuffix(destination.peerSpace.bootstrapLockPath, ".bootstrap.lock")
	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed import created Peer store: %v", err)
	}
}

func newPeerTrustImportApp(t *testing.T, now time.Time) *App {
	t.Helper()
	return newPeerTrustImportAppAt(t, now, t.TempDir())
}

func newPeerTrustImportAppAt(t *testing.T, now time.Time, dir string) *App {
	t.Helper()
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
			return nil, errors.New("missing test Peer store key")
		}
		return key, nil
	})
	return &App{peerSpace: manager}
}

func peerTestKeyringPath(app *App) string {
	storePath := strings.TrimSuffix(app.peerSpace.bootstrapLockPath, ".bootstrap.lock")
	return filepath.Join(filepath.Dir(storePath), "keyring")
}

func clearTestPeerTrustSlots(t *testing.T, spaceID string) {
	t.Helper()
	for _, clearSlot := range []func() error{
		peerEpochKeySlot(spaceID, configsync.KeyClassSync).Clear,
		peerEpochKeySlot(spaceID, configsync.KeyClassVault).Clear,
		peerStoreKeySlot().Clear,
		peerWrappingIdentitySlot().Clear,
		peerIdentitySlot().Clear,
	} {
		if err := clearSlot(); err != nil {
			t.Fatal(err)
		}
	}
}
