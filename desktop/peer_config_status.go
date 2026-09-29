package main

import (
	"errors"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
)

// PeerConfigSyncStatus is a token-free projection of local durable config
// state. PendingOperations counts operations not acknowledged by any active
// remote member; it does not imply storage on a central service.
type PeerConfigSyncStatus struct {
	Configured              bool   `json:"configured"`
	LocalOperations         uint64 `json:"local_operations"`
	PendingOperations       uint64 `json:"pending_operations"`
	ReplicaDevices          int    `json:"replica_devices"`
	ActiveRemoteMembers     int    `json:"active_remote_members"`
	AcknowledgingPeers      int    `json:"acknowledging_peers"`
	LastExchangeAt          int64  `json:"last_exchange_at,omitempty"`
	PendingImportRecords    int    `json:"pending_import_records"`
	PendingImportCapturedAt int64  `json:"pending_import_captured_at,omitempty"`
}

// GetPeerConfigSyncStatus reports local durable progress without exposing
// operations, version-vector actor ids, memberships, or epoch material.
func (a *App) GetPeerConfigSyncStatus() (PeerConfigSyncStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	return manager.configSyncStatus()
}

func (m *peerSpaceManager) configSyncStatus() (PeerConfigSyncStatus, error) {
	state, err := m.store.Load()
	if errors.Is(err, peerstore.ErrNotInitialized) {
		return PeerConfigSyncStatus{Configured: false}, nil
	}
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	runtime, err := m.ensureConfigReplica()
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	if err := runtime.replica.Reload(); err != nil {
		return PeerConfigSyncStatus{}, err
	}
	localVector := runtime.replica.Vector()
	status := PeerConfigSyncStatus{
		Configured:     true,
		ReplicaDevices: len(localVector),
	}
	for _, counter := range localVector {
		status.LocalOperations += counter
	}

	active, err := activePeerMemberships(state, genesis, m.now())
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	acknowledged := make(configsync.VersionVector)
	localPeerID := runtime.identity.PeerID()
	for _, membership := range active {
		peerID := membership.Document.SubjectPeerID
		if peerID == localPeerID {
			continue
		}
		status.ActiveRemoteMembers++
		peerState, ok := state.ConfigSyncPeers[peerID]
		if !ok || peerState.LastExchangeAt == 0 {
			continue
		}
		status.AcknowledgingPeers++
		if peerState.LastExchangeAt > status.LastExchangeAt {
			status.LastExchangeAt = peerState.LastExchangeAt
		}
		acknowledged.Merge(configsync.VersionVector(peerState.Acknowledged))
	}
	for actor, counter := range localVector {
		if confirmed := acknowledged[actor]; counter > confirmed {
			status.PendingOperations += counter - confirmed
		}
	}
	if pending, ok, err := m.loadPendingPeerConfig(); err != nil {
		return PeerConfigSyncStatus{}, err
	} else if ok {
		status.PendingImportRecords = len(pending.Records)
		status.PendingImportCapturedAt = pending.CapturedAt
	}
	return status, nil
}

// AcceptPendingPeerConfig merges the preserved pre-join values and projects
// the resulting winners before clearing the encrypted pending payload. A
// failed projection therefore remains retryable.
func (a *App) AcceptPendingPeerConfig() (PeerConfigSyncStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	pending, ok, err := manager.loadPendingPeerConfig()
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	if ok {
		runtime, err := manager.ensureConfigReplica()
		if err != nil {
			return PeerConfigSyncStatus{}, err
		}
		if _, err := runtime.appendPendingPeerConfig(pending.plainRecords()); err != nil {
			return PeerConfigSyncStatus{}, err
		}
		if _, err := a.applyPeerConfigProjection(false); err != nil {
			return PeerConfigSyncStatus{}, err
		}
		if err := manager.store.ClearPendingConfigImport(manager.now()); err != nil {
			return PeerConfigSyncStatus{}, err
		}
	}
	return manager.configSyncStatus()
}

// DiscardPendingPeerConfig keeps the adopted Space state and removes only the
// encrypted pre-join customization snapshot.
func (a *App) DiscardPendingPeerConfig() (PeerConfigSyncStatus, error) {
	manager, err := a.peerManager()
	if err != nil {
		return PeerConfigSyncStatus{}, err
	}
	if err := manager.discardPendingPeerConfig(); err != nil {
		return PeerConfigSyncStatus{}, err
	}
	return manager.configSyncStatus()
}
