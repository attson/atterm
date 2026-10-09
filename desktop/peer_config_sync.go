package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
)

var errPeerConfigSyncDenied = errors.New("Peer config sync denied")

// peerConfigSyncAck separates the in-memory transfer cursor from the durable
// configuration frontier. A reconnect may restart chunk transfer at zero;
// signed tokens already in the durable frontier remain idempotent.
type peerConfigSyncAck struct {
	Cursor              configsync.AntiEntropyCursor
	Durable             configsync.DurableAck
	Done                bool
	PendingLocalRecords int
}

// peerConfigSyncReceiver coordinates one authenticated anti-entropy stream.
// It deliberately has no terminal/session dependency, so config exchange can
// never create a PTY subscriber or disturb lazy terminal upload.
type peerConfigSyncReceiver struct {
	app                 *App
	manager             *peerSpaceManager
	genesis             peerproto.VerifiedGenesis
	remote              peerproto.VerifiedGrant
	assembler           *configsync.AntiEntropyAssembler
	pending             []configsync.AntiEntropyItem
	configDirty         bool
	rotations           []string
	pendingLocalRecords int
	completed           bool
	lastAck             peerConfigSyncAck
}

func (a *App) newPeerConfigSyncReceiver(remoteMembershipToken string) (*peerConfigSyncReceiver, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, fmt.Errorf("verify Peer config sync genesis: %w", err)
	}
	now := manager.now()
	remote, err := peerproto.VerifyGrant(remoteMembershipToken, genesis, now)
	if err != nil {
		return nil, fmt.Errorf("verify Peer config sync membership: %w", err)
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return nil, err
	}
	local, err := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	if err != nil {
		return nil, fmt.Errorf("verify local Peer config sync membership: %w", err)
	}
	if !membershipIsActive(local, active) || !membershipIsActive(remote, active) {
		return nil, errPeerConfigSyncDenied
	}
	assembler, err := configsync.NewAntiEntropyAssembler(genesis)
	if err != nil {
		return nil, err
	}
	return &peerConfigSyncReceiver{
		app: a, manager: manager, genesis: genesis, remote: remote, assembler: assembler,
	}, nil
}

// Add accepts one transport batch and returns a cursor only after every
// complete token emitted by that batch has reached its durable owner. Local
// settings are projected once, at the terminal batch, after epoch refresh.
func (r *peerConfigSyncReceiver) Add(batch configsync.AntiEntropyBatch) (peerConfigSyncAck, error) {
	if r == nil || r.assembler == nil {
		return peerConfigSyncAck{}, configsync.ErrInvalidAntiEntropy
	}
	if r.completed {
		items, err := r.assembler.Add(batch)
		if err != nil || len(items) != 0 {
			return peerConfigSyncAck{}, configsync.ErrAntiEntropyOrder
		}
		return r.lastAck, nil
	}
	items, err := r.assembler.Add(batch)
	if err != nil {
		return peerConfigSyncAck{}, err
	}
	r.pending = append(r.pending, items...)
	if err := r.persistPending(); err != nil {
		return peerConfigSyncAck{}, err
	}
	if batch.Done {
		if len(r.rotations) != 0 {
			if _, err := r.manager.applyPeerEpochRotations(r.rotations); err != nil {
				return peerConfigSyncAck{}, fmt.Errorf("apply Peer config epoch rotations: %w", err)
			}
			r.rotations = nil
		}
		if r.configDirty {
			if _, err := r.app.applyPeerConfigProjection(false); err != nil {
				return peerConfigSyncAck{}, fmt.Errorf("project Peer config sync: %w", err)
			}
			r.configDirty = false
		}
	}
	runtime, err := r.manager.ensureConfigReplica()
	if err != nil {
		return peerConfigSyncAck{}, err
	}
	ack := peerConfigSyncAck{
		Cursor:  acceptedAntiEntropyCursor(batch),
		Durable: configsync.DurableAck{SpaceID: r.genesis.Document.SpaceID, Vector: runtime.replica.Vector()},
		Done:    batch.Done, PendingLocalRecords: r.pendingLocalRecords,
	}
	if batch.Done {
		r.completed = true
		r.lastAck = ack
	}
	return ack, nil
}

func (r *peerConfigSyncReceiver) persistPending() error {
	for len(r.pending) != 0 {
		item := r.pending[0]
		switch item.Kind {
		case configsync.AntiEntropyMembership:
			count := 1
			tokens := []string{item.Token}
			for count < len(r.pending) && r.pending[count].Kind == configsync.AntiEntropyMembership {
				tokens = append(tokens, r.pending[count].Token)
				count++
			}
			if _, err := r.manager.store.ApplyMemberships(tokens, r.manager.now()); err != nil {
				return fmt.Errorf("apply Peer config membership: %w", err)
			}
			r.pending = r.pending[count:]
			continue
		case configsync.AntiEntropySnapshot:
			if err := r.persistSnapshot(item.Token); err != nil {
				return err
			}
			r.configDirty = true
		case configsync.AntiEntropyOperation:
			if err := r.authorizeOperation(item.Token); err != nil {
				return err
			}
			runtime, err := r.manager.ensureConfigReplica()
			if err != nil {
				return err
			}
			if _, _, err := runtime.replica.Apply(item.Token); err != nil {
				return fmt.Errorf("apply Peer config operation: %w", err)
			}
			r.configDirty = true
		case configsync.AntiEntropyRevocation:
			if _, err := r.manager.store.ApplyRevocations([]string{item.Token}, r.manager.now()); err != nil {
				return fmt.Errorf("apply Peer config revocation: %w", err)
			}
		case configsync.AntiEntropyRotation:
			r.rotations = append(r.rotations, item.Token)
		default:
			return configsync.ErrInvalidAntiEntropy
		}
		r.pending = r.pending[1:]
	}
	return nil
}

func (r *peerConfigSyncReceiver) authorizeOperation(token string) error {
	operation, err := configsync.VerifyOp(token)
	if err != nil {
		return err
	}
	state, err := r.manager.store.Load()
	if err != nil {
		return err
	}
	active, err := activePeerMemberships(state, r.genesis, r.manager.now())
	if err != nil {
		return err
	}
	for _, membership := range active {
		if membership.Document.SubjectPeerID == operation.Document.ActorDeviceID && bytes.Equal(membership.PublicKey, operation.PublicKey) {
			return nil
		}
	}
	return errPeerConfigSyncDenied
}

func (r *peerConfigSyncReceiver) persistSnapshot(token string) error {
	verified, err := configsync.VerifySnapshot(token)
	if err != nil {
		return err
	}
	state, err := r.manager.store.Load()
	if err != nil {
		return err
	}
	active, err := activePeerMemberships(state, r.genesis, r.manager.now())
	if err != nil {
		return err
	}
	if !membershipIsActive(r.remote, active) || !snapshotCreatorIsActive(verified, active) {
		return errPeerConfigSyncDenied
	}
	runtime, err := r.manager.ensureConfigReplica()
	if err != nil {
		return err
	}
	current := runtime.replica.StateForPeer(configsync.VersionVector{})
	empty := current.Snapshot == "" && len(current.Ops) == 0 && len(current.Ack.Vector) == 0
	if empty {
		if r.app.cfgStore == nil {
			return errors.New("config store not ready")
		}
		count, _, err := r.manager.capturePendingPeerConfig(r.app.cfgStore.Get())
		if err != nil {
			return err
		}
		r.pendingLocalRecords = count
	}
	if _, err := runtime.replica.RebaseSnapshot(token); err != nil {
		return fmt.Errorf("rebase Peer config snapshot: %w", err)
	}
	return nil
}

func membershipIsActive(want peerproto.VerifiedGrant, active []peerproto.VerifiedGrant) bool {
	for _, membership := range active {
		if membership.Token == want.Token && membership.Document.SubjectPeerID == want.Document.SubjectPeerID {
			return true
		}
	}
	return false
}

func snapshotCreatorIsActive(snapshot configsync.VerifiedSnapshot, active []peerproto.VerifiedGrant) bool {
	for _, membership := range active {
		if membership.Document.SubjectPeerID == snapshot.Document.CreatorDeviceID && bytes.Equal(membership.PublicKey, snapshot.PublicKey) {
			return true
		}
	}
	return false
}

func acceptedAntiEntropyCursor(batch configsync.AntiEntropyBatch) configsync.AntiEntropyCursor {
	cursor := batch.Start
	for _, chunk := range batch.Chunks {
		cursor.Offset = chunk.Offset + len(chunk.Data)
		if cursor.Offset == chunk.Total {
			cursor.Item++
			cursor.Offset = 0
		}
	}
	return cursor
}
