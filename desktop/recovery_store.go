package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/attson/atterm/internal/appdir"
)

const (
	recoveryTTLDuration = 14 * 24 * time.Hour
	recoveryMaxBytes    = 256 * 1024
)

var errSnapshotTooLarge = errors.New("recovery snapshot exceeds size cap")

// RecoveryStore owns the on-disk recovery.json document. All methods are
// safe to call from any goroutine; internal serialization is via the file
// system rename. Caller controls path + host id to keep tests fully
// isolated from real ~/.config.
type RecoveryStore struct {
	path    string
	hostID  string
	nowUnix func() int64
}

// NewRecoveryStore wires a store to ~/.config/atterm/recovery.json. Used by
// production code; tests build their own with tempRecoveryStore.
func NewRecoveryStore(hostID string) (*RecoveryStore, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return nil, err
	}
	return &RecoveryStore{
		path:    filepath.Join(dir, "recovery.json"),
		hostID:  hostID,
		nowUnix: func() int64 { return time.Now().Unix() },
	}, nil
}

// Save atomically writes snap. Caller is expected to set Version, HostID,
// SavedAtUnix; we don't override them so MarkCleanShutdown can re-Save the
// exact loaded snapshot with just CleanShutdown flipped.
func (rs *RecoveryStore) Save(snap RecoverySnapshot) error {
	blob, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal recovery snapshot: %w", err)
	}
	if len(blob) > recoveryMaxBytes {
		return errSnapshotTooLarge
	}
	tmp := rs.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("write recovery tmp: %w", err)
	}
	if f, err := os.Open(tmp); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, rs.path); err != nil {
		return fmt.Errorf("rename recovery: %w", err)
	}
	return nil
}

// Load reads the snapshot, validating version/host/TTL. On any rejection
// it deletes the file and returns the zero RecoverySnapshot. The caller
// gets the original CleanShutdown bit (so the dialog can branch on it),
// but the on-disk file is immediately overwritten with CleanShutdown=false
// to handle a second crash mid-launch.
func (rs *RecoveryStore) Load() (RecoverySnapshot, error) {
	blob, err := os.ReadFile(rs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return RecoverySnapshot{}, nil
		}
		return RecoverySnapshot{}, err
	}
	var snap RecoverySnapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		logWarn("recovery", "discard malformed snapshot: %v", err)
		_ = os.Remove(rs.path)
		return RecoverySnapshot{}, nil
	}
	now := rs.nowUnix()
	if snap.Version != recoverySnapshotVersion ||
		snap.HostID != rs.hostID ||
		(snap.SavedAtUnix != 0 && now-snap.SavedAtUnix > int64(recoveryTTLDuration/time.Second)) {
		_ = os.Remove(rs.path)
		return RecoverySnapshot{}, nil
	}
	// Two-phase clean flag: caller sees the loaded value, on-disk file goes false.
	if snap.CleanShutdown {
		dirty := snap
		dirty.CleanShutdown = false
		if err := rs.Save(dirty); err != nil {
			logWarn("recovery", "rewrite clean_shutdown=false: %v", err)
		}
	}
	return snap, nil
}

// Discard removes the file. Missing file is not an error.
func (rs *RecoveryStore) Discard() error {
	if err := os.Remove(rs.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MarkCleanShutdown rewrites the file with CleanShutdown=true so the next
// launch can render "last clean exit" copy in the dialog. Idempotent; no-op
// if there's no file to mark.
func (rs *RecoveryStore) MarkCleanShutdown(snap RecoverySnapshot) error {
	if len(snap.Tabs) == 0 {
		return nil
	}
	snap.CleanShutdown = true
	snap.SavedAtUnix = rs.nowUnix()
	return rs.Save(snap)
}
