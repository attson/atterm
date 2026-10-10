package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerbackup"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const peerTrustBackupPayloadVersion = 1

type peerTrustBackupPayload struct {
	Version            int             `json:"version"`
	ExportedAt         int64           `json:"exported_at"`
	SigningPrivateKey  []byte          `json:"signing_private_key"`
	WrappingPrivateKey []byte          `json:"wrapping_private_key"`
	State              peerstore.State `json:"state"`
}

// ExportPeerTrustBackup writes a passphrase-encrypted recovery package. The
// native file never contains plaintext private or epoch keys.
func (a *App) ExportPeerTrustBackup(passphrase string) (string, error) {
	data, err := a.buildPeerTrustBackup(passphrase)
	if err != nil {
		return "", err
	}
	defaultName := "atterm-peer-trust-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ".json"
	save := a.saveDialog
	if save == nil {
		save = wailsruntime.SaveFileDialog
	}
	path, err := save(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export Peer Space recovery package",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "AT Term Peer recovery (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	write := a.writeFile
	if write == nil {
		write = os.WriteFile
	}
	if err := write(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) buildPeerTrustBackup(passphrase string) ([]byte, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, err
	}
	wrapping, err := manager.loadWrappingIdentity()
	if err != nil {
		return nil, err
	}
	if _, _, err := validatePeerTrustBackupState(state, identity, wrapping, manager.now()); err != nil {
		return nil, err
	}

	// Invitations carry replayable pairing secrets, while acknowledgement and
	// pending-import cursors are live replica state. Neither belongs in an
	// offline identity recovery point.
	state.Invitations = nil
	state.PendingConfigImport = nil
	state.ConfigSyncPeers = nil
	state.RevokedMembers = nil
	state.RevokedGrantSerials = nil
	payload := peerTrustBackupPayload{
		Version: peerTrustBackupPayloadVersion, ExportedAt: manager.now().Unix(),
		SigningPrivateKey: identity.PrivateBytes(), WrappingPrivateKey: wrapping.PrivateBytes(), State: state,
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal Peer trust backup: %w", err)
	}
	defer clear(plaintext)
	defer clear(payload.SigningPrivateKey)
	defer clear(payload.WrappingPrivateKey)
	return peerbackup.Seal(passphrase, plaintext)
}

// ImportPeerTrustBackup restores one encrypted recovery package into an empty
// installation. Existing Peer identity is never overwritten.
func (a *App) ImportPeerTrustBackup(encoded, passphrase string) (PeerSpaceStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	return manager.importPeerTrustBackup([]byte(encoded), passphrase)
}

func (m *peerSpaceManager) importPeerTrustBackup(encoded []byte, passphrase string) (PeerSpaceStatus, error) {
	release, err := m.acquireBootstrapLock()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	defer release()
	if status, statusErr := m.status(); statusErr == nil && status.Configured {
		return PeerSpaceStatus{}, errors.New("Peer Space is already configured")
	} else if statusErr != nil && !errors.Is(statusErr, peerstore.ErrNotInitialized) {
		return PeerSpaceStatus{}, fmt.Errorf("check existing Peer Space: %w", statusErr)
	}
	if occupied, err := peerTrustSlotsOccupied(); err != nil {
		return PeerSpaceStatus{}, err
	} else if occupied {
		return PeerSpaceStatus{}, errors.New("Peer identity secure storage is not empty")
	}

	plaintext, err := peerbackup.Open(passphrase, encoded)
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	defer clear(plaintext)
	var payload peerTrustBackupPayload
	if err := strictPeerTrustJSON(plaintext, &payload); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("invalid Peer trust backup payload: %w", err)
	}
	defer clear(payload.SigningPrivateKey)
	defer clear(payload.WrappingPrivateKey)
	if payload.Version != peerTrustBackupPayloadVersion || payload.ExportedAt <= 0 {
		return PeerSpaceStatus{}, errors.New("invalid Peer trust backup payload")
	}
	if len(payload.State.Invitations) != 0 || len(payload.State.PendingConfigImport) != 0 ||
		len(payload.State.ConfigSyncPeers) != 0 || len(payload.State.RevokedMembers) != 0 ||
		len(payload.State.RevokedGrantSerials) != 0 {
		return PeerSpaceStatus{}, errors.New("Peer trust backup contains live or derived replica state")
	}
	identity, err := peercrypto.ParseIdentity(payload.SigningPrivateKey)
	if err != nil {
		return PeerSpaceStatus{}, errors.New("invalid Peer trust backup identity")
	}
	wrapping, err := peercrypto.ParseWrappingIdentity(payload.WrappingPrivateKey)
	if err != nil {
		return PeerSpaceStatus{}, errors.New("invalid Peer trust backup wrapping identity")
	}
	genesis, _, err := validatePeerTrustBackupState(payload.State, identity, wrapping, m.now())
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	if m.configRoot != "" {
		spaceRoot := filepath.Join(m.configRoot, genesis.Document.SpaceID)
		if _, err := os.Stat(spaceRoot); err == nil {
			return PeerSpaceStatus{}, errors.New("Peer Space config replica already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return PeerSpaceStatus{}, fmt.Errorf("check Peer Space config replica: %w", err)
		}
	}

	storeKey := make([]byte, 32)
	if _, err := rand.Read(storeKey); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("generate Peer store key: %w", err)
	}
	defer clear(storeKey)
	if err := peerIdentitySlot().Save(payload.SigningPrivateKey); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("restore Peer identity: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			clearImportedPeerTrust(m, genesis.Document.SpaceID)
		}
	}()
	if err := peerWrappingIdentitySlot().Save(payload.WrappingPrivateKey); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("restore Peer wrapping identity: %w", err)
	}
	if err := peerStoreKeySlot().Save(storeKey); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("restore Peer store key: %w", err)
	}
	if err := m.store.Initialize(payload.State); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("restore Peer Space state: %w", err)
	}
	if err := m.ensureInitialEpochState(); err != nil {
		return PeerSpaceStatus{}, fmt.Errorf("restore Peer Space epoch keys: %w", err)
	}
	status, err := m.status()
	if err != nil {
		return PeerSpaceStatus{}, err
	}
	cleanup = false
	return status, nil
}

func validatePeerTrustBackupState(state peerstore.State, identity *peercrypto.Identity, wrapping *peercrypto.WrappingIdentity, now time.Time) (peerproto.VerifiedGenesis, peerproto.VerifiedGrant, error) {
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, errors.New("invalid Peer trust backup genesis")
	}
	membership, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, errors.New("Peer trust backup membership is not active")
	}
	if membership.Document.SubjectPeerID != identity.PeerID() ||
		!bytes.Equal(membership.PublicKey, identity.PublicBytes()) ||
		!bytes.Equal(membership.WrappingPublicKey, wrapping.PublicBytes()) {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, errors.New("Peer trust backup identity does not own membership")
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, fmt.Errorf("validate Peer trust backup governance: %w", err)
	}
	localActive := false
	for _, candidate := range active {
		if candidate.Document.SubjectPeerID == identity.PeerID() {
			localActive = true
			break
		}
	}
	if !localActive {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, errors.New("Peer trust backup membership is revoked")
	}
	rotations, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil || len(rotations) != 2 {
		return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, errors.New("Peer trust backup epoch history is invalid")
	}
	classes := []configsync.KeyClass{configsync.KeyClassSync}
	if membership.Document.CanSyncSecrets {
		classes = append(classes, configsync.KeyClassVault)
	}
	for _, class := range classes {
		if _, err := openBackupEpochKey(state, genesis, rotations[class], identity.PeerID(), wrapping); err != nil {
			return peerproto.VerifiedGenesis{}, peerproto.VerifiedGrant{}, fmt.Errorf("Peer trust backup %s epoch key is unavailable", class)
		}
	}
	return genesis, membership, nil
}

func openBackupEpochKey(state peerstore.State, genesis peerproto.VerifiedGenesis, rotation configsync.VerifiedEpochRotation, peerID string, wrapping *peercrypto.WrappingIdentity) (configsync.EpochKey, error) {
	if key, err := configsync.OpenRotationEpochKey(rotation, genesis, peerID, wrapping); err == nil {
		return key, nil
	}
	for _, envelope := range state.EpochEnvelopes {
		key, err := configsync.OpenEpochKey(envelope, genesis.Document.SpaceID, peerID, wrapping)
		if err == nil && configsync.ValidateEpochKeyForRotation(key, rotation) == nil {
			return key, nil
		}
	}
	return configsync.EpochKey{}, configsync.ErrInvalidEpochKey
}

func peerTrustSlotsOccupied() (bool, error) {
	for _, load := range []func() ([]byte, error){peerIdentitySlot().Load, peerWrappingIdentitySlot().Load, peerStoreKeySlot().Load} {
		value, err := load()
		if err != nil {
			return false, err
		}
		if len(value) != 0 {
			return true, nil
		}
	}
	return false, nil
}

func clearImportedPeerTrust(m *peerSpaceManager, spaceID string) {
	_ = peerEpochKeySlot(spaceID, configsync.KeyClassSync).Clear()
	_ = peerEpochKeySlot(spaceID, configsync.KeyClassVault).Clear()
	_ = peerStoreKeySlot().Clear()
	_ = peerWrappingIdentitySlot().Clear()
	_ = peerIdentitySlot().Clear()
	if path := strings.TrimSuffix(m.bootstrapLockPath, ".bootstrap.lock"); path != "" && path != m.bootstrapLockPath {
		_ = os.Remove(path)
	}
}

func strictPeerTrustJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
