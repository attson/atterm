package main

import (
	"bytes"
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
	"github.com/attson/atterm/internal/peerstore"
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
	dir := t.TempDir()
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
