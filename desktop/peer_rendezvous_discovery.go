package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
)

type peerRendezvousDiscoveryState struct {
	spaceID         string
	localPeerID     string
	activePeerIDs   []string
	activePeerIDSet map[string]struct{}
	syncKey         configsync.EpochKey
	storeState      peerstore.State
}

// rendezvousCoordinates derives the local registration address from current
// membership material. Nothing derived here is persisted or sent to Relay.
func (m *peerSpaceManager) rendezvousCoordinates(at time.Time) (peerdiscovery.Coordinates, error) {
	state, err := m.rendezvousDiscoveryState(at)
	if err != nil {
		return peerdiscovery.Coordinates{}, err
	}
	return peerdiscovery.DeriveCoordinates(state.syncKey, state.spaceID, state.localPeerID, at)
}

// resolveRendezvousPresence accepts only identifiers derived for a currently
// active remote member. Peer transport must still authenticate its exact grant.
func (m *peerSpaceManager) resolveRendezvousPresence(presenceID string, at time.Time) (string, bool, error) {
	state, err := m.rendezvousDiscoveryState(at)
	if err != nil {
		return "", false, err
	}
	peerID, ok, err := peerdiscovery.ResolvePresence(state.syncKey, state.spaceID, state.activePeerIDs, presenceID, at)
	if err != nil || !ok || peerID == state.localPeerID {
		return "", false, err
	}
	return peerID, true, nil
}

// planRendezvousSyncTargets joins ephemeral reachability to local durable sync
// progress. Unknown or revoked observations are ignored rather than promoted
// into membership state.
func (m *peerSpaceManager) planRendezvousSyncTargets(observations []peerdiscovery.Reachability, at time.Time) ([]peerdiscovery.SyncCandidate, error) {
	state, err := m.rendezvousDiscoveryState(at)
	if err != nil {
		return nil, err
	}
	runtime, err := m.ensureConfigReplica()
	if err != nil {
		return nil, err
	}
	if err := runtime.replica.Reload(); err != nil {
		return nil, fmt.Errorf("reload Peer config replica for Rendezvous planning: %w", err)
	}

	candidates := make([]peerdiscovery.SyncCandidate, 0, len(observations))
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if observation.PeerID == state.localPeerID {
			continue
		}
		if _, active := state.activePeerIDSet[observation.PeerID]; !active {
			continue
		}
		if _, duplicate := seen[observation.PeerID]; duplicate {
			continue
		}
		resolved, ok, err := peerdiscovery.ResolvePresence(
			state.syncKey, state.spaceID, []string{observation.PeerID}, observation.PresenceID, at,
		)
		if err != nil {
			return nil, err
		}
		if !ok || resolved != observation.PeerID || !observation.ExpiresAt.After(at) {
			continue
		}
		seen[observation.PeerID] = struct{}{}
		progress := state.storeState.ConfigSyncPeers[observation.PeerID]
		candidates = append(candidates, peerdiscovery.SyncCandidate{
			PeerID: observation.PeerID, PresenceID: observation.PresenceID,
			Acknowledged: configsync.VersionVector(progress.Acknowledged), LastExchangeAt: progress.LastExchangeAt,
		})
	}
	coordinates, err := peerdiscovery.DeriveCoordinates(state.syncKey, state.spaceID, state.localPeerID, at)
	if err != nil {
		return nil, err
	}
	return peerdiscovery.PlanSyncTargets(peerdiscovery.PlanRequest{
		LocalPeerID: state.localPeerID, ActiveMemberCount: len(state.activePeerIDs),
		LocalVector: runtime.replica.Vector(), Candidates: candidates, Slot: coordinates.Slot,
	})
}

func (m *peerSpaceManager) rendezvousDiscoveryState(at time.Time) (peerRendezvousDiscoveryState, error) {
	if m == nil || at.IsZero() {
		return peerRendezvousDiscoveryState{}, peerdiscovery.ErrInvalidDiscovery
	}
	stored, err := m.store.Load()
	if err != nil {
		return peerRendezvousDiscoveryState{}, err
	}
	genesis, err := peerproto.VerifyGenesis(stored.GenesisToken)
	if err != nil {
		return peerRendezvousDiscoveryState{}, fmt.Errorf("verify Peer Space for Rendezvous discovery: %w", err)
	}
	identity, err := m.loadIdentity()
	if err != nil {
		return peerRendezvousDiscoveryState{}, err
	}
	active, err := activePeerMemberships(stored, genesis, at)
	if err != nil {
		return peerRendezvousDiscoveryState{}, err
	}
	activePeerIDs := make([]string, 0, len(active))
	activePeerIDSet := make(map[string]struct{}, len(active))
	for _, membership := range active {
		peerID := membership.Document.SubjectPeerID
		activePeerIDs = append(activePeerIDs, peerID)
		activePeerIDSet[peerID] = struct{}{}
	}
	if _, active := activePeerIDSet[identity.PeerID()]; !active {
		return peerRendezvousDiscoveryState{}, errors.New("local Peer membership is not active")
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		return peerRendezvousDiscoveryState{}, fmt.Errorf("load Peer Space sync epoch for Rendezvous discovery: %w", err)
	}
	return peerRendezvousDiscoveryState{
		spaceID: genesis.Document.SpaceID, localPeerID: identity.PeerID(),
		activePeerIDs: activePeerIDs, activePeerIDSet: activePeerIDSet,
		syncKey: key, storeState: stored,
	}, nil
}
