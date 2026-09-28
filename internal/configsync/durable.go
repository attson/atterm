package configsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
)

const (
	durableVersion      = 1
	maxDurableStoreSize = 64 << 20
	durableLockTimeout  = 5 * time.Second
	durableStaleLockAge = 30 * time.Second
)

var ErrDurableStoreInvalid = errors.New("configsync: invalid durable store")

type durableState struct {
	Version       int      `json:"version"`
	SpaceID       string   `json:"space_id"`
	SchemaVersion uint32   `json:"schema_version"`
	Snapshot      string   `json:"snapshot,omitempty"`
	Ops           []string `json:"ops"`
}

// DurableAck is safe to send only after the operation and its vector have
// reached the atomically replaced local file.
type DurableAck struct {
	SpaceID string        `json:"space_id"`
	Vector  VersionVector `json:"vector"`
}

// SyncState is the durable state needed by a peer. Snapshot is populated when
// the peer's vector predates local compaction; Ops always contains the tail it
// still needs after adopting that snapshot.
type SyncState struct {
	Snapshot string     `json:"snapshot,omitempty"`
	Ops      []string   `json:"ops"`
	Ack      DurableAck `json:"ack"`
}

// EncryptedMutation pairs one plaintext mutation with its current epoch key.
// Batches may contain both sync and vault records without exposing either key
// to the compatibility adapter.
type EncryptedMutation struct {
	Key      EpochKey
	Mutation Mutation
}

// DurableReplica serializes each mutation across processes and acknowledges
// it only after fsync and atomic rename. Read methods use the last loaded view;
// Reload observes changes written by another process.
type DurableReplica struct {
	mu      sync.RWMutex
	path    string
	spaceID string
	schema  uint32
	clock   *Clock
	replica *Replica
	state   durableState
}

// OpenDurableReplica opens an existing replica file or prepares an empty one.
// The file is created by the first durable mutation.
func OpenDurableReplica(path, spaceID string, schemaVersion uint32, clock *Clock) (*DurableReplica, error) {
	if path == "" || !validSpaceID(spaceID) || schemaVersion == 0 {
		return nil, fmt.Errorf("%w: path, space, or schema", ErrDurableStoreInvalid)
	}
	if clock == nil {
		clock = NewClock(DefaultMaxFutureSkew)
	}
	d := &DurableReplica{path: path, spaceID: spaceID, schema: schemaVersion, clock: clock}
	state, err := d.readState()
	if err != nil {
		return nil, err
	}
	replica, err := d.buildReplica(state)
	if err != nil {
		return nil, err
	}
	d.state = state
	d.replica = replica
	return d, nil
}

// Reload replaces the in-memory view with the latest durable file contents.
func (d *DurableReplica) Reload() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, err := d.readState()
	if err != nil {
		return err
	}
	replica, err := d.buildReplica(state)
	if err != nil {
		return err
	}
	d.state, d.replica = state, replica
	return nil
}

// Apply durably stores one inbound operation before returning its ack vector.
func (d *DurableReplica) Apply(token string) (ApplyResult, DurableAck, error) {
	var result ApplyResult
	ack, err := d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		applied, err := replica.Apply(token)
		if err != nil {
			return false, err
		}
		result = applied
		if applied.Stored {
			state.Ops = append(state.Ops, token)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return ApplyResult{}, DurableAck{}, err
	}
	return result, ack, err
}

// Append allocates the next counter under the cross-process lock and durably
// stores the signed local operation before returning.
func (d *DurableReplica) Append(identity *peercrypto.Identity, mutation Mutation) (VerifiedOp, DurableAck, error) {
	var operation VerifiedOp
	ack, err := d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		created, err := replica.Append(identity, mutation)
		if err != nil {
			return false, err
		}
		operation = created
		state.Ops = append(state.Ops, created.Token)
		return true, nil
	})
	if err != nil {
		return VerifiedOp{}, DurableAck{}, err
	}
	return operation, ack, err
}

// AppendEncrypted allocates the counter, seals plaintext, signs, and persists
// the operation under one cross-process lock.
func (d *DurableReplica) AppendEncrypted(identity *peercrypto.Identity, key EpochKey, mutation Mutation) (VerifiedOp, DurableAck, error) {
	var operation VerifiedOp
	ack, err := d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		created, err := replica.AppendEncrypted(identity, key, mutation)
		if err != nil {
			return false, err
		}
		operation = created
		state.Ops = append(state.Ops, created.Token)
		return true, nil
	})
	if err != nil {
		return VerifiedOp{}, DurableAck{}, err
	}
	return operation, ack, nil
}

// AppendEncryptedBatch allocates contiguous counters and persists all signed
// operations with one atomic file replacement. Validation or encryption
// failure at any position leaves the durable replica unchanged.
func (d *DurableReplica) AppendEncryptedBatch(identity *peercrypto.Identity, mutations []EncryptedMutation) ([]VerifiedOp, DurableAck, error) {
	operations := make([]VerifiedOp, 0, len(mutations))
	ack, err := d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		for index, item := range mutations {
			created, err := replica.AppendEncrypted(identity, item.Key, item.Mutation)
			if err != nil {
				return false, fmt.Errorf("append encrypted mutation %d: %w", index, err)
			}
			operations = append(operations, created)
			state.Ops = append(state.Ops, created.Token)
		}
		return len(mutations) != 0, nil
	})
	if err != nil {
		return nil, DurableAck{}, err
	}
	return operations, ack, nil
}

// AdoptSnapshot durably initializes an empty replica from a trusted snapshot.
// Membership authorization of the snapshot creator must happen before this
// method is called.
func (d *DurableReplica) AdoptSnapshot(token string) (DurableAck, error) {
	return d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		if state.Snapshot != "" || len(state.Ops) != 0 {
			return false, ErrReplicaNotEmpty
		}
		if err := replica.InstallSnapshot(token); err != nil {
			return false, err
		}
		state.Snapshot = token
		return true, nil
	})
}

// Compact writes a new signed snapshot and removes all covered tail ops in the
// same atomic file replacement.
func (d *DurableReplica) Compact(identity *peercrypto.Identity) (string, DurableAck, error) {
	var snapshot string
	ack, err := d.mutate(func(replica *Replica, state *durableState) (bool, error) {
		created, err := replica.SignSnapshot(identity)
		if err != nil {
			return false, err
		}
		next := d.emptyState()
		next.Snapshot = created
		rebuilt, err := d.buildReplica(next)
		if err != nil {
			return false, fmt.Errorf("verify compacted replica: %w", err)
		}
		replica.replaceState(rebuilt)
		*state = next
		snapshot = created
		return true, nil
	})
	if err != nil {
		return "", DurableAck{}, err
	}
	return snapshot, ack, err
}

// Vector returns the current durable causal frontier.
func (d *DurableReplica) Vector() VersionVector {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.replica.Vector()
}

// Get returns one record from the last loaded durable view.
func (d *DurableReplica) Get(collection, recordID string) (Record, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.replica.Get(collection, recordID)
}

// Records returns one collection's last loaded durable materialized view.
func (d *DurableReplica) Records(collection string) []Record {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.replica.Records(collection)
}

// StateForPeer returns either missing tail operations or a snapshot plus tail
// when the peer is behind the local compaction boundary.
func (d *DurableReplica) StateForPeer(remote VersionVector) SyncState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	state := SyncState{Ack: DurableAck{SpaceID: d.spaceID, Vector: d.replica.Vector()}}
	if d.state.Snapshot != "" && !remote.Covers(d.replica.CompactedVector()) {
		state.Snapshot = d.state.Snapshot
		state.Ops = append([]string(nil), d.state.Ops...)
		return state
	}
	state.Ops = d.replica.MissingTokens(remote)
	return state
}

func (d *DurableReplica) mutate(apply func(*Replica, *durableState) (bool, error)) (DurableAck, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ack DurableAck
	err := d.withFileLock(func() error {
		state, err := d.readState()
		if err != nil {
			return err
		}
		replica, err := d.buildReplica(state)
		if err != nil {
			return err
		}
		dirty, err := apply(replica, &state)
		if err != nil {
			return err
		}
		if dirty {
			if err := d.writeState(state); err != nil {
				return err
			}
		}
		d.state, d.replica = state, replica
		ack = DurableAck{SpaceID: d.spaceID, Vector: replica.Vector()}
		return nil
	})
	return ack, err
}

func (d *DurableReplica) emptyState() durableState {
	return durableState{
		Version: durableVersion, SpaceID: d.spaceID,
		SchemaVersion: d.schema, Ops: []string{},
	}
}

func (d *DurableReplica) readState() (durableState, error) {
	blob, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return d.emptyState(), nil
	}
	if err != nil {
		return durableState{}, fmt.Errorf("read config replica: %w", err)
	}
	if len(blob) == 0 || len(blob) > maxDurableStoreSize {
		return durableState{}, fmt.Errorf("%w: file size", ErrDurableStoreInvalid)
	}
	var state durableState
	if err := strictDurableJSON(blob, &state); err != nil {
		return durableState{}, fmt.Errorf("%w: %v", ErrDurableStoreInvalid, err)
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(canonical, blob) {
		return durableState{}, fmt.Errorf("%w: non-canonical state", ErrDurableStoreInvalid)
	}
	if state.Version != durableVersion || state.SpaceID != d.spaceID || state.SchemaVersion != d.schema || state.Ops == nil {
		return durableState{}, fmt.Errorf("%w: header", ErrDurableStoreInvalid)
	}
	return state, nil
}

func (d *DurableReplica) buildReplica(state durableState) (*Replica, error) {
	replayClock := NewClockWithSource(time.Now, time.Duration(1<<63-1))
	replica, err := NewReplica(d.spaceID, d.schema, replayClock)
	if err != nil {
		return nil, err
	}
	var maxHLC Timestamp
	if state.Snapshot != "" {
		verified, err := VerifySnapshot(state.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("load config snapshot: %w", err)
		}
		maxHLC = verified.Document.CreatedHLC
		if err := replica.InstallSnapshot(state.Snapshot); err != nil {
			return nil, fmt.Errorf("install config snapshot: %w", err)
		}
	}
	for _, token := range state.Ops {
		verified, err := VerifyOp(token)
		if err != nil {
			return nil, fmt.Errorf("load config operation: %w", err)
		}
		if CompareTimestamp(verified.Document.HLC, maxHLC) > 0 {
			maxHLC = verified.Document.HLC
		}
		result, err := replica.Apply(token)
		if err != nil {
			return nil, fmt.Errorf("replay config operation: %w", err)
		}
		if !result.Stored {
			return nil, fmt.Errorf("%w: duplicate or compacted tail operation", ErrDurableStoreInvalid)
		}
	}
	if maxHLC.PhysicalMS > 0 {
		if err := d.clock.Restore(maxHLC); err != nil {
			return nil, err
		}
	}
	replica.clock = d.clock
	return replica, nil
}

func (d *DurableReplica) writeState(state durableState) error {
	if state.Ops == nil {
		state.Ops = []string{}
	}
	blob, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal config replica: %w", err)
	}
	if len(blob) > maxDurableStoreSize {
		return fmt.Errorf("configsync: durable store exceeds size limit")
	}
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config replica directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-replica-*.tmp")
	if err != nil {
		return fmt.Errorf("create config replica temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod config replica temp: %w", err)
	}
	if _, err := tmp.Write(blob); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config replica temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync config replica temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config replica temp: %w", err)
	}
	if err := os.Rename(tmpName, d.path); err != nil {
		return fmt.Errorf("replace config replica: %w", err)
	}
	if runtime.GOOS != "windows" {
		directory, err := os.Open(dir)
		if err != nil {
			return fmt.Errorf("open config replica directory: %w", err)
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return fmt.Errorf("sync config replica directory: %w", syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close config replica directory: %w", closeErr)
		}
	}
	return nil
}

func (d *DurableReplica) withFileLock(run func() error) error {
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config replica directory: %w", err)
	}
	lockPath := d.path + ".lock"
	deadline := time.Now().Add(durableLockTimeout)
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			defer os.Remove(lockPath)
			return run()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquire config replica lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > durableStaleLockAge {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("configsync: timed out waiting for durable store lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func strictDurableJSON(blob []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(blob))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
