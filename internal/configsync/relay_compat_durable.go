package configsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	maxRelayCompatibilitySize = 4 << 20
	relayCompatibilityTimeout = 5 * time.Second
	relayCompatibilityStale   = 30 * time.Second
)

// DurableRelayCompatibility persists one Relay realm's adapter state. Update
// reloads under a cross-process lock so multiple desktop instances sharing a
// Peer Space do not overwrite each other's echo and record-ref knowledge.
type DurableRelayCompatibility struct {
	mu      sync.Mutex
	path    string
	realmID string
}

// OpenDurableRelayCompatibility validates an existing realm state or prepares
// an empty store whose first successful Update creates the file.
func OpenDurableRelayCompatibility(path, realmID string) (*DurableRelayCompatibility, error) {
	if path == "" || realmID == "" {
		return nil, fmt.Errorf("%w: path or realm", ErrInvalidRelayState)
	}
	store := &DurableRelayCompatibility{path: path, realmID: realmID}
	if _, err := store.Load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Load returns an independent compatibility engine backed by the latest
// atomically replaced file. A missing file is a fresh realm state.
func (s *DurableRelayCompatibility) Load() (*RelayCompatibility, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Update serializes a read-modify-write transaction across processes. The
// callback may durably append canonical config operations before returning;
// a callback error leaves compatibility state unchanged and retryable.
func (s *DurableRelayCompatibility) Update(apply func(*RelayCompatibility) error) (RelayCompatibilityState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var state RelayCompatibilityState
	err := s.withFileLock(func() error {
		compatibility, err := s.load()
		if err != nil {
			return err
		}
		if apply == nil {
			return fmt.Errorf("%w: missing durable update callback", ErrInvalidRelayState)
		}
		if err := apply(compatibility); err != nil {
			return err
		}
		state = compatibility.State()
		return s.write(state)
	})
	if err != nil {
		return RelayCompatibilityState{}, err
	}
	return state, nil
}

func (s *DurableRelayCompatibility) load() (*RelayCompatibility, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return NewRelayCompatibility(s.realmID, RelayCompatibilityState{})
	}
	if err != nil {
		return nil, fmt.Errorf("read Relay compatibility state: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxRelayCompatibilitySize {
		return nil, fmt.Errorf("%w: file size", ErrInvalidRelayState)
	}
	state, err := ParseRelayCompatibilityState(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, fmt.Errorf("%w: non-canonical state", ErrInvalidRelayState)
	}
	return NewRelayCompatibility(s.realmID, state)
}

func (s *DurableRelayCompatibility) write(state RelayCompatibilityState) error {
	compatibility, err := NewRelayCompatibility(s.realmID, state)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(compatibility.State())
	if err != nil {
		return fmt.Errorf("marshal Relay compatibility state: %w", err)
	}
	if len(raw) > maxRelayCompatibilitySize {
		return fmt.Errorf("%w: file size", ErrInvalidRelayState)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Relay compatibility directory: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".relay-compat-*.tmp")
	if err != nil {
		return fmt.Errorf("create Relay compatibility temp: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("chmod Relay compatibility temp: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write Relay compatibility temp: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync Relay compatibility temp: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Relay compatibility temp: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace Relay compatibility state: %w", err)
	}
	if runtime.GOOS != "windows" {
		directory, err := os.Open(dir)
		if err != nil {
			return fmt.Errorf("open Relay compatibility directory: %w", err)
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return fmt.Errorf("sync Relay compatibility directory: %w", syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close Relay compatibility directory: %w", closeErr)
		}
	}
	return nil
}

func (s *DurableRelayCompatibility) withFileLock(run func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create Relay compatibility lock directory: %w", err)
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(relayCompatibilityTimeout)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			defer os.Remove(lockPath)
			return run()
		} else if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquire Relay compatibility lock: %w", err)
		}
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > relayCompatibilityStale {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("configsync: timed out waiting for Relay compatibility lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
