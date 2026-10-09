package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerstore"
)

const peerPendingConfigVersion = 1

type peerPendingConfigImport struct {
	Version    int                       `json:"version"`
	CapturedAt int64                     `json:"captured_at"`
	Records    []peerPendingConfigRecord `json:"records"`
}

type peerPendingConfigRecord struct {
	Collection string              `json:"collection"`
	RecordID   string              `json:"record_id"`
	KeyClass   configsync.KeyClass `json:"key_class"`
	Position   string              `json:"position,omitempty"`
	Value      json.RawMessage     `json:"value"`
}

// capturePendingPeerConfig preserves customized pre-join values before a
// Space snapshot is projected locally. An unresolved capture is write-once so
// a reconnect cannot silently replace the choices still awaiting the user.
func (m *peerSpaceManager) capturePendingPeerConfig(cfg appConfig) (int, bool, error) {
	records, err := peerBootstrapRecords(cfg)
	if err != nil {
		return 0, false, err
	}
	if len(records) == 0 {
		return 0, false, nil
	}
	if existing, ok, err := m.loadPendingPeerConfig(); err != nil {
		return 0, false, err
	} else if ok {
		mutations, err := configsync.PlanRecordReplacement(records, existing.plainRecords())
		if err != nil {
			return 0, false, err
		}
		if len(mutations) == 0 {
			return len(records), false, nil
		}
		return 0, false, fmt.Errorf("capture pending Peer config: %w", peerstore.ErrPendingExists)
	}
	now := m.now()
	payload, err := marshalPendingPeerConfig(records, now.Unix())
	if err != nil {
		return 0, false, err
	}
	if err := m.store.SavePendingConfigImport(payload, now); err != nil {
		return 0, false, err
	}
	return len(records), true, nil
}

func (m *peerSpaceManager) loadPendingPeerConfig() (peerPendingConfigImport, bool, error) {
	state, err := m.store.Load()
	if err != nil {
		return peerPendingConfigImport{}, false, err
	}
	if len(state.PendingConfigImport) == 0 {
		return peerPendingConfigImport{}, false, nil
	}
	pending, err := parsePendingPeerConfig(state.PendingConfigImport)
	if err != nil {
		return peerPendingConfigImport{}, false, err
	}
	return pending, true, nil
}

// acceptPendingPeerConfig appends only local records represented by the
// capture. Missing local records never become tombstones for Peer-only data.
func (m *peerSpaceManager) acceptPendingPeerConfig() (int, error) {
	pending, ok, err := m.loadPendingPeerConfig()
	if err != nil || !ok {
		return 0, err
	}
	runtime, err := m.ensureConfigReplica()
	if err != nil {
		return 0, err
	}
	records := pending.plainRecords()
	operations, err := runtime.appendPendingPeerConfig(records)
	if err != nil {
		return 0, err
	}
	if err := m.store.ClearPendingConfigImport(m.now()); err != nil {
		return operations, err
	}
	return operations, nil
}

func (r *peerConfigReplica) appendPendingPeerConfig(records []configsync.PlainRecord) (int, error) {
	previous := make([]configsync.PlainRecord, 0, len(records))
	for _, record := range records {
		if _, available := r.keys[record.KeyClass]; !available {
			return 0, fmt.Errorf("accept pending Peer config: key class %s unavailable", record.KeyClass)
		}
		current, exists := r.replica.Get(record.Collection, record.RecordID)
		if !exists || current.Deleted {
			continue
		}
		if current.KeyClass != record.KeyClass || len(r.resolveEpochKeys(current.KeyClass, current.KeyEpoch)) == 0 {
			return 0, fmt.Errorf("accept pending Peer config %s/%s: %w", record.Collection, record.RecordID, configsync.ErrInvalidEpochKey)
		}
		opened, err := r.openRecord(current)
		if err != nil {
			return 0, err
		}
		previous = append(previous, opened)
	}
	mutations, err := configsync.PlanRecordReplacement(records, previous)
	if err != nil {
		return 0, err
	}
	return r.appendPeerConfigMutations("pending local Peer config", mutations)
}

func (m *peerSpaceManager) discardPendingPeerConfig() error {
	return m.store.ClearPendingConfigImport(m.now())
}

func marshalPendingPeerConfig(records []configsync.PlainRecord, capturedAt int64) ([]byte, error) {
	if len(records) == 0 || capturedAt <= 0 {
		return nil, errors.New("pending Peer config is empty")
	}
	pending := peerPendingConfigImport{
		Version: peerPendingConfigVersion, CapturedAt: capturedAt,
		Records: make([]peerPendingConfigRecord, len(records)),
	}
	seen := make(map[string]struct{}, len(records))
	for index, record := range records {
		if _, err := configsync.SetRecordMutation(record); err != nil {
			return nil, err
		}
		id := record.Collection + "\x00" + record.RecordID
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate pending Peer config record %s/%s", record.Collection, record.RecordID)
		}
		seen[id] = struct{}{}
		pending.Records[index] = peerPendingConfigRecord{
			Collection: record.Collection, RecordID: record.RecordID,
			KeyClass: record.KeyClass, Position: record.Position,
			Value: append(json.RawMessage(nil), record.Value...),
		}
	}
	return json.Marshal(pending)
}

func parsePendingPeerConfig(raw []byte) (peerPendingConfigImport, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var pending peerPendingConfigImport
	if err := decoder.Decode(&pending); err != nil {
		return peerPendingConfigImport{}, fmt.Errorf("decode pending Peer config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return peerPendingConfigImport{}, errors.New("decode pending Peer config: trailing JSON")
	}
	if pending.Version != peerPendingConfigVersion || pending.CapturedAt <= 0 || len(pending.Records) == 0 {
		return peerPendingConfigImport{}, errors.New("invalid pending Peer config header")
	}
	if _, err := marshalPendingPeerConfig(pending.plainRecords(), pending.CapturedAt); err != nil {
		return peerPendingConfigImport{}, err
	}
	return pending, nil
}

func (p peerPendingConfigImport) plainRecords() []configsync.PlainRecord {
	records := make([]configsync.PlainRecord, len(p.Records))
	for index, record := range p.Records {
		records[index] = configsync.PlainRecord{
			Collection: record.Collection, RecordID: record.RecordID,
			KeyClass: record.KeyClass, Position: record.Position,
			Value: append(json.RawMessage(nil), record.Value...),
		}
	}
	return records
}
