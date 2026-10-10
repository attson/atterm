// Package peertraffic persists privacy-preserving, device-local Peer route
// usage. Rows contain only UTC day, route class and aggregate encrypted record
// counts; peer identities, session IDs and network endpoints are never stored.
package peertraffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	storeVersion  = 1
	maxStoreSize  = 1 << 20
	retentionDays = 180
	lockTimeout   = 2 * time.Second
	staleLockAge  = 30 * time.Second
)

// Route identifies a privacy-preserving transport class.
type Route string

const (
	RouteDirect      Route = "direct"
	RouteQuickTunnel Route = "quick_tunnel"
	RouteLAN         Route = "lan"
	// RouteGateway is used by a shared host-side WSS gateway where the same
	// authenticated handler can be reached through Quick Tunnel or manual LAN.
	RouteGateway Route = "gateway"
)

// Direction is relative to the local desktop process.
type Direction uint8

const (
	DirectionSent Direction = iota + 1
	DirectionReceived
)

// Row is one local UTC-day and route aggregate suitable for renderer output.
type Row struct {
	Day             string `json:"day"`
	Route           Route  `json:"route"`
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	RecordsSent     uint64 `json:"records_sent"`
	RecordsReceived uint64 `json:"records_received"`
}

type state struct {
	Version int   `json:"version"`
	Rows    []Row `json:"rows"`
}

type key struct {
	day   string
	route Route
}

// Store keeps hot-path increments in memory and merges them into one bounded,
// cross-process-safe JSON file on Flush or Query.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	pending map[key]Row
}

// New constructs a local aggregate store. The file is created on first flush.
func New(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{path: path, now: now, pending: make(map[key]Row)}
}

// Record adds one successfully authenticated encrypted record. It performs no
// I/O and deliberately ignores invalid route/direction/size input.
func (s *Store) Record(route Route, direction Direction, wireBytes int) {
	if s == nil || !validRoute(route) || wireBytes <= 0 ||
		direction != DirectionSent && direction != DirectionReceived {
		return
	}
	day := s.now().UTC().Format(time.DateOnly)
	k := key{day: day, route: route}
	s.mu.Lock()
	row := s.pending[k]
	row.Day = day
	row.Route = route
	if direction == DirectionSent {
		row.BytesSent += uint64(wireBytes)
		row.RecordsSent++
	} else {
		row.BytesReceived += uint64(wireBytes)
		row.RecordsReceived++
	}
	s.pending[k] = row
	s.mu.Unlock()
}

// Flush durably merges this process's pending deltas with other desktop
// instances. Failed deltas are returned to memory for a later retry.
func (s *Store) Flush() error {
	if s == nil || s.path == "" {
		return errors.New("peertraffic: store path is missing")
	}
	pending := s.takePending()
	if len(pending) == 0 {
		return nil
	}
	err := s.withFileLock(func() error {
		persisted, err := s.load()
		if err != nil {
			return err
		}
		rows := make(map[key]Row, len(persisted.Rows)+len(pending))
		for _, row := range persisted.Rows {
			rows[key{day: row.Day, route: row.Route}] = row
		}
		for k, delta := range pending {
			row := rows[k]
			row.Day, row.Route = k.day, k.route
			row.BytesSent += delta.BytesSent
			row.BytesReceived += delta.BytesReceived
			row.RecordsSent += delta.RecordsSent
			row.RecordsReceived += delta.RecordsReceived
			rows[k] = row
		}
		cutoff := s.now().UTC().AddDate(0, 0, -retentionDays+1).Format(time.DateOnly)
		next := state{Version: storeVersion, Rows: make([]Row, 0, len(rows))}
		for _, row := range rows {
			if row.Day >= cutoff {
				next.Rows = append(next.Rows, row)
			}
		}
		sortRows(next.Rows)
		return s.write(next)
	})
	if err != nil {
		s.restorePending(pending)
	}
	return err
}

// Query flushes current deltas and returns inclusive UTC-day rows.
func (s *Store) Query(from, to time.Time) ([]Row, error) {
	if s == nil || from.IsZero() || to.IsZero() {
		return nil, errors.New("peertraffic: invalid range")
	}
	fromDay := from.UTC().Format(time.DateOnly)
	toDay := to.UTC().Format(time.DateOnly)
	if fromDay > toDay {
		return nil, errors.New("peertraffic: invalid range")
	}
	if err := s.Flush(); err != nil {
		return nil, err
	}
	var persisted state
	if err := s.withFileLock(func() error {
		var err error
		persisted, err = s.load()
		return err
	}); err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(persisted.Rows))
	for _, row := range persisted.Rows {
		if row.Day >= fromDay && row.Day <= toDay {
			rows = append(rows, row)
		}
	}
	sortRows(rows)
	return rows, nil
}

func (s *Store) takePending() map[key]Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = make(map[key]Row)
	return pending
}

func (s *Store) restorePending(failed map[key]Row) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, delta := range failed {
		row := s.pending[k]
		row.Day, row.Route = k.day, k.route
		row.BytesSent += delta.BytesSent
		row.BytesReceived += delta.BytesReceived
		row.RecordsSent += delta.RecordsSent
		row.RecordsReceived += delta.RecordsReceived
		s.pending[k] = row
	}
}

func (s *Store) load() (state, error) {
	blob, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state{Version: storeVersion, Rows: []Row{}}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("peertraffic: read store: %w", err)
	}
	if len(blob) == 0 || len(blob) > maxStoreSize {
		return state{}, errors.New("peertraffic: invalid store size")
	}
	var persisted state
	if err := strictJSON(blob, &persisted); err != nil {
		return state{}, fmt.Errorf("peertraffic: decode store: %w", err)
	}
	if persisted.Version != storeVersion {
		return state{}, errors.New("peertraffic: unsupported store version")
	}
	seen := make(map[key]struct{}, len(persisted.Rows))
	for _, row := range persisted.Rows {
		k := key{day: row.Day, route: row.Route}
		if !validDay(row.Day) || !validRoute(row.Route) {
			return state{}, errors.New("peertraffic: invalid row")
		}
		if _, duplicate := seen[k]; duplicate {
			return state{}, errors.New("peertraffic: duplicate row")
		}
		seen[k] = struct{}{}
	}
	return persisted, nil
}

func (s *Store) write(next state) error {
	blob, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("peertraffic: encode store: %w", err)
	}
	if len(blob) > maxStoreSize {
		return errors.New("peertraffic: store size limit exceeded")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("peertraffic: create directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".peer-traffic-*.tmp")
	if err != nil {
		return fmt.Errorf("peertraffic: create temp: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(blob); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("peertraffic: replace store: %w", err)
	}
	return nil
}

func (s *Store) withFileLock(run func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			defer os.Remove(lockPath)
			return run()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("peertraffic: acquire lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("peertraffic: timed out waiting for lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func validRoute(route Route) bool {
	return route == RouteDirect || route == RouteQuickTunnel || route == RouteLAN || route == RouteGateway
}

func validDay(day string) bool {
	parsed, err := time.Parse(time.DateOnly, day)
	return err == nil && parsed.Format(time.DateOnly) == day
}

func sortRows(rows []Row) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Route < rows[j].Route
	})
}

func strictJSON(blob []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(blob))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}
