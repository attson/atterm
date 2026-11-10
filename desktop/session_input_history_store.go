package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
)

const sessionInputHistoryStoreVersion = 1

type persistedSessionInputHistory struct {
	Version     int                       `json:"version"`
	SavedAtUnix int64                     `json:"saved_at_unix"`
	Items       []SessionInputHistoryItem `json:"items"`
	Truncated   bool                      `json:"truncated"`
}

type sessionInputHistoryFlushRequest struct {
	close bool
	done  chan error
}

// sessionInputHistoryStore keeps one bounded document per local PTY
// generation. Queue is safe for the OSC callback path: it only replaces an
// in-memory pointer and wakes the single disk writer.
type sessionInputHistoryStore struct {
	dir string
	now func() time.Time

	mu      sync.Mutex
	pending map[uuid.UUID]*sessionInputHistory
	closed  bool
	wake    chan struct{}
	flush   chan sessionInputHistoryFlushRequest
	stopped chan struct{}
}

func NewSessionInputHistoryStore() (*sessionInputHistoryStore, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return nil, err
	}
	return newSessionInputHistoryStore(filepath.Join(dir, "session-input-history"), time.Now)
}

func newSessionInputHistoryStore(dir string, now func() time.Time) (*sessionInputHistoryStore, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create session input history dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure session input history dir: %w", err)
	}
	store := &sessionInputHistoryStore{
		dir:     dir,
		now:     now,
		pending: make(map[uuid.UUID]*sessionInputHistory),
		wake:    make(chan struct{}, 1),
		flush:   make(chan sessionInputHistoryFlushRequest),
		stopped: make(chan struct{}),
	}
	store.pruneExpired()
	go store.run()
	return store, nil
}

func (s *sessionInputHistoryStore) Queue(id uuid.UUID, history *sessionInputHistory) {
	if s == nil || id == uuid.Nil || history == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pending[id] = history
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *sessionInputHistoryStore) Load(id uuid.UUID) (SessionInputHistoryResult, bool, error) {
	if s == nil || id == uuid.Nil {
		return SessionInputHistoryResult{}, false, nil
	}
	blob, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return SessionInputHistoryResult{}, false, nil
		}
		return SessionInputHistoryResult{}, false, fmt.Errorf("read session input history: %w", err)
	}
	var persisted persistedSessionInputHistory
	if err := json.Unmarshal(blob, &persisted); err != nil {
		_ = os.Remove(s.path(id))
		return SessionInputHistoryResult{}, false, nil
	}
	if persisted.Version != sessionInputHistoryStoreVersion ||
		persisted.SavedAtUnix == 0 ||
		s.now().Unix()-persisted.SavedAtUnix > int64(recoveryTTLDuration/time.Second) {
		_ = os.Remove(s.path(id))
		return SessionInputHistoryResult{}, false, nil
	}

	// Reuse the live history's validation and caps before trusting disk data.
	history := newSessionInputHistory(s.now)
	history.Restore(SessionInputHistoryResult{Items: persisted.Items, Truncated: persisted.Truncated})
	return history.Snapshot(), true, nil
}

func (s *sessionInputHistoryStore) Flush() error {
	if s == nil {
		return nil
	}
	done := make(chan error, 1)
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil
	}
	select {
	case s.flush <- sessionInputHistoryFlushRequest{done: done}:
		return <-done
	case <-s.stopped:
		return nil
	}
}

func (s *sessionInputHistoryStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	done := make(chan error, 1)
	select {
	case s.flush <- sessionInputHistoryFlushRequest{close: true, done: done}:
		return <-done
	case <-s.stopped:
		return nil
	}
}

func (s *sessionInputHistoryStore) run() {
	defer close(s.stopped)
	for {
		select {
		case <-s.wake:
			if err := s.drain(); err != nil {
				logWarn("session-input-history", "persist failed: %v", err)
			}
		case req := <-s.flush:
			err := s.drain()
			req.done <- err
			if req.close {
				return
			}
		}
	}
}

func (s *sessionInputHistoryStore) drain() error {
	var errs []error
	for {
		s.mu.Lock()
		pending := s.pending
		s.pending = make(map[uuid.UUID]*sessionInputHistory)
		s.mu.Unlock()
		if len(pending) == 0 {
			return errors.Join(errs...)
		}
		for id, history := range pending {
			if err := s.write(id, history.Snapshot()); err != nil {
				errs = append(errs, err)
			}
		}
	}
}

func (s *sessionInputHistoryStore) write(id uuid.UUID, snapshot SessionInputHistoryResult) error {
	persisted := persistedSessionInputHistory{
		Version:     sessionInputHistoryStoreVersion,
		SavedAtUnix: s.now().Unix(),
		Items:       snapshot.Items,
		Truncated:   snapshot.Truncated,
	}
	blob, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal session input history %s: %w", id, err)
	}
	tmp := s.path(id) + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("write session input history %s: %w", id, err)
	}
	if file, err := os.Open(tmp); err == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	if err := os.Rename(tmp, s.path(id)); err != nil {
		return fmt.Errorf("replace session input history %s: %w", id, err)
	}
	return nil
}

func (s *sessionInputHistoryStore) path(id uuid.UUID) string {
	return filepath.Join(s.dir, id.String()+".json")
}

func (s *sessionInputHistoryStore) pruneExpired() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") {
			_ = os.Remove(filepath.Join(s.dir, name))
			continue
		}
		idText := strings.TrimSuffix(name, ".json")
		if idText == name {
			continue
		}
		if _, err := uuid.Parse(idText); err != nil {
			continue
		}
		path := filepath.Join(s.dir, name)
		blob, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var persisted persistedSessionInputHistory
		err = json.Unmarshal(blob, &persisted)
		if err != nil || persisted.Version != sessionInputHistoryStoreVersion ||
			persisted.SavedAtUnix == 0 ||
			s.now().Unix()-persisted.SavedAtUnix > int64(recoveryTTLDuration/time.Second) {
			_ = os.Remove(path)
		}
	}
}
